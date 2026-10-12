package lease

import (
	"context"
	"errors"
	"sync"
	"time"
)

// State is the local fail-closed lifecycle state of a lease handle.
type State uint8

const (
	// StateActive permits admission before the safety deadline.
	StateActive State = iota + 1
	// StateExpired means the local safety deadline has passed.
	StateExpired
	// StateLost means the backend proved ownership is stale.
	StateLost
	// StateUncertain means a remote operation had no reliable outcome.
	StateUncertain
	// StateReleased means compare-and-release completed successfully.
	StateReleased
)

// String returns a stable redaction-safe state name.
func (state State) String() string {
	switch state {
	case StateActive:
		return "active"
	case StateExpired:
		return "expired"
	case StateLost:
		return "lost"
	case StateUncertain:
		return "uncertain"
	case StateReleased:
		return "released"
	default:
		return "invalid"
	}
}

// Handle is a concurrency-safe local admission view from a successful acquire.
// State access synchronizes snapshots; overlapping Renew/Validate/Release calls
// fail with ErrInvalidState rather than waiting for the in-flight operation.
// Backend work runs outside the state lock. Expiry/loss/uncertainty closes local
// admission permanently; resource-side fencing remains necessary for prior work.
// Its zero value is unusable, and the caller owns release/managed-stop lifetimes.
type Handle struct {
	mu           sync.Mutex
	backend      Backend
	clock        Clock
	sleeper      Sleeper
	managedSlots chan struct{}
	policy       Policy
	record       Record
	deadline     time.Time
	wallDeadline time.Time
	state        State
	busy         bool
	managed      bool
}

func newHandle(
	backend Backend,
	clock Clock,
	sleeper Sleeper,
	managedSlots chan struct{},
	policy Policy,
	record Record,
) *Handle {
	return newHandleAt(
		backend, clock, sleeper, managedSlots, policy, record,
		clock.Now(), time.Now(),
	)
}

func newHandleAt(
	backend Backend,
	clock Clock,
	sleeper Sleeper,
	managedSlots chan struct{},
	policy Policy,
	record Record,
	started time.Time,
	wallStarted time.Time,
) *Handle {
	// Keep injected/local and real-time budgets separately. A frozen/rolled-back
	// test clock cannot extend the real-time bound; remote timestamps do not set
	// either bound. Native TTL truncation requires suitable policy granularity.
	budget := policy.TTL() - policy.SafetyMargin()
	return &Handle{
		backend: backend, clock: clock, sleeper: sleeper,
		managedSlots: managedSlots, policy: policy,
		record: record, deadline: started.Add(budget),
		wallDeadline: wallStarted.Add(budget), state: StateActive,
	}
}

// Owner returns the opaque identity used for ownership comparisons.
func (handle *Handle) Owner() string {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	return handle.record.Owner
}

// Token returns the fencing token for protected-resource writes.
func (handle *Handle) Token() Token {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	return handle.record.Token
}

// Snapshot returns the latest copied backend record, even after local loss or
// release. It is not an active-ownership/authentication proof. Use State/Validate
// for admission and atomically check ownership at the protected-write boundary;
// for example, Valkey Guard construction alone performs no remote check.
func (handle *Handle) Snapshot() Record {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	return handle.record
}

// AcquiredAt returns the backend-anchored acquisition instant.
func (handle *Handle) AcquiredAt() time.Time {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	return handle.record.AcquiredAt
}

// Deadline returns the injected-clock safety deadline, not raw backend expiry.
// State also enforces a separate real-time deadline, so this accessor alone does
// not establish remaining admission when an injected clock is frozen or skewed.
func (handle *Handle) Deadline() time.Time {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	return handle.deadline
}

// State returns the current fail-closed local state.
func (handle *Handle) State() State {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.expireLocked()
	return handle.state
}

// Renew requires active local admission and a nonnil context, then asks Backend
// to compare ownership and renew. Matching success extends the local budget from
// the operation start only if the old safety deadline has not already passed.
// Remote success after that deadline still returns ErrLost; no local revival is
// allowed. Stale errors mark loss, other errors uncertainty; neither proves that
// a remote mutation did not happen. Concurrent operations are rejected.
func (handle *Handle) Renew(ctx context.Context) error {
	record, err := handle.beginActiveOperation()
	if err != nil {
		return err
	}
	started := handle.clock.Now()
	wallStarted := time.Now()
	operationContext, cancel := context.WithTimeout(ctx, handle.policy.OperationTimeout())
	defer cancel()
	next, err := handle.backend.Renew(operationContext, record, handle.policy.TTL())
	if err == nil && !sameAcquisition(record, next) {
		err = Wrap(ErrAmbiguousOutcome, "renew response")
	}
	budget := handle.policy.TTL() - handle.policy.SafetyMargin()
	return handle.finishActiveOperation(
		next, err, started.Add(budget), wallStarted.Add(budget),
	)
}

// Validate requires active local admission and a nonnil context, then checks
// remote ownership without extending the local deadline. Response identity is
// checked, backend errors fail admission closed, and expiry during the call
// returns ErrLost even after remote success. It cannot reserve a later effect.
func (handle *Handle) Validate(ctx context.Context) error {
	record, err := handle.beginActiveOperation()
	if err != nil {
		return err
	}
	operationContext, cancel := context.WithTimeout(ctx, handle.policy.OperationTimeout())
	defer cancel()
	next, err := handle.backend.Validate(operationContext, record)
	if err == nil && !sameAcquisition(record, next) {
		err = Wrap(ErrBackendUnavailable, "validate response")
	}
	return handle.finishActiveOperation(next, err, time.Time{}, time.Time{})
}

// Release conditionally deactivates the recorded owner/token under a nonnil
// operation context. An expired handle can still attempt release; lost/uncertain
// handles and overlapping operations return ErrInvalidState without remote work.
// After acknowledged release, repeats return nil without another backend call.
// A failure cannot be treated as remote absence, and release does not join or
// stop a managed renewer; the caller must stop renewal separately.
func (handle *Handle) Release(ctx context.Context) error {
	handle.mu.Lock()
	if handle.state == StateReleased {
		handle.mu.Unlock()
		return nil
	}
	if handle.state == StateLost || handle.state == StateUncertain {
		handle.mu.Unlock()
		return Wrap(ErrInvalidState, "release")
	}
	if handle.busy {
		handle.mu.Unlock()
		return Wrap(ErrInvalidState, "handle operation")
	}
	handle.busy = true
	record := handle.record
	handle.mu.Unlock()
	operationContext, cancel := context.WithTimeout(ctx, handle.policy.OperationTimeout())
	defer cancel()
	err := handle.backend.Release(operationContext, record)
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.busy = false
	if err != nil {
		handle.failLocked(err)
		return err
	}
	handle.state = StateReleased
	return nil
}

// StartManaged starts one renewer bound to the supplied nonnil context. It
// requires active admission, enabled RenewEvery, no overlapping operation/renewer
// and an available shared managed slot. The caller owns Managed.Stop and release;
// canceling this context stops renewal, not remote ownership. Sleeper/backend
// cooperation is required, and there is no separate automatic expiry watcher.
func (handle *Handle) StartManaged(ctx context.Context) (*Managed, error) {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	if err := handle.requireActiveLocked(); err != nil {
		return nil, err
	}
	if handle.policy.RenewEvery() <= 0 || handle.managed || handle.busy {
		return nil, Wrap(ErrInvalidState, "managed renewal")
	}
	select {
	case handle.managedSlots <- struct{}{}:
	default:
		return nil, Wrap(ErrBackendUnavailable, "managed renewal capacity")
	}
	// #nosec G118 -- Managed owns cancel and Stop invokes it exactly once.
	runContext, cancel := context.WithCancel(ctx)
	managed := &Managed{
		handle: handle, cancel: cancel,
		loss: make(chan Loss, 1), done: make(chan struct{}),
	}
	handle.managed = true
	go managed.run(runContext)
	return managed, nil
}

func (handle *Handle) beginActiveOperation() (Record, error) {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	if err := handle.requireActiveLocked(); err != nil {
		return Record{}, err
	}
	if handle.busy {
		return Record{}, Wrap(ErrInvalidState, "handle operation")
	}
	// Reserve a single operation, then drop the lock before backend I/O. Returning
	// quickly on overlap avoids blocking accessors or creating a second mutation.
	handle.busy = true
	return handle.record, nil
}

func (handle *Handle) finishActiveOperation(
	record Record,
	err error,
	deadline time.Time,
	wallDeadline time.Time,
) error {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	handle.busy = false
	// Expire against the old deadline before accepting a renewal response: a late
	// remote success must not revive admission already lost during this call.
	handle.expireLocked()
	if err != nil {
		handle.failLocked(err)
		return err
	}
	if handle.state != StateActive {
		return Wrap(ErrLost, "deadline during operation")
	}
	handle.record = record
	if !deadline.IsZero() {
		handle.deadline = deadline
		handle.wallDeadline = wallDeadline
	}
	return nil
}

func (handle *Handle) requireActiveLocked() error {
	handle.expireLocked()
	if handle.state == StateExpired {
		return Wrap(ErrLost, "deadline")
	}
	if handle.state != StateActive {
		return Wrap(ErrInvalidState, "handle")
	}
	return nil
}

func (handle *Handle) expireLocked() {
	if handle.state == StateActive &&
		(!handle.clock.Now().Before(handle.deadline) ||
			!time.Now().Before(handle.wallDeadline)) {
		handle.state = StateExpired
	}
}

func (handle *Handle) failLocked(err error) {
	if handle.state != StateActive {
		return
	}
	if errors.Is(err, ErrStaleOwner) || errors.Is(err, ErrLost) {
		handle.state = StateLost
		return
	}
	handle.state = StateUncertain
}

func sameAcquisition(current, next Record) bool {
	return current.Key.String() == next.Key.String() &&
		current.Owner == next.Owner && current.Token == next.Token &&
		current.AcquiredAt.Equal(next.AcquiredAt) &&
		next.ExpiresAt.After(next.AcquiredAt)
}
