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

// Handle is a concurrency-safe local view of remotely fenced ownership.
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

// AcquiredAt returns the backend-anchored acquisition instant.
func (handle *Handle) AcquiredAt() time.Time {
	handle.mu.Lock()
	defer handle.mu.Unlock()
	return handle.record.AcquiredAt
}

// Deadline returns the local safety deadline, not the raw backend expiry.
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

// Renew atomically compares owner and token before extending the deadline.
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

// Validate checks remote ownership and fails local admission closed on error.
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

// Release idempotently compares owner and token before deactivation.
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

// StartManaged starts at most one caller-owned renewal goroutine.
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
