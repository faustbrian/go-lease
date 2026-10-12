package lease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Operation is a bounded observation operation name.
type Operation string

const (
	// OperationAcquire identifies one acquisition attempt.
	OperationAcquire Operation = "acquire"
	// OperationRenew identifies one renewal attempt.
	OperationRenew Operation = "renew"
	// OperationValidate identifies one validation attempt.
	OperationValidate Operation = "validate"
	// OperationRelease identifies one release attempt.
	OperationRelease Operation = "release"
)

// Outcome is a bounded, identifier-free observation result.
type Outcome string

const (
	// OutcomeSuccess reports a successful operation.
	OutcomeSuccess Outcome = "success"
	// OutcomeContended reports expected acquisition contention.
	OutcomeContended Outcome = "contended"
	// OutcomeStale reports a rejected stale owner.
	OutcomeStale Outcome = "stale"
	// OutcomeCanceled reports caller cancellation.
	OutcomeCanceled Outcome = "canceled"
	// OutcomeUnavailable is the fallback for unavailable or unclassified errors;
	// it does not prove remote mutation absence.
	OutcomeUnavailable Outcome = "unavailable"
	// OutcomeAmbiguous reports an uncertain remote mutation.
	OutcomeAmbiguous Outcome = "ambiguous"
	// OutcomeInvalid reports invalid input or state.
	OutcomeInvalid Outcome = "invalid"
)

// Event is the observation value emitted with fixed vocabulary and hashed keys
// by NewObservedBackend. Caller-constructed values are not validated or redacted;
// string fields can contain arbitrary sensitive/unbounded text.
type Event struct {
	// At is the decorator's injected local observation instant.
	At time.Time
	// Operation is the fixed backend-call label on package-emitted events.
	Operation Operation
	// Outcome is a classification, not proof of remote effect absence.
	Outcome Outcome
	// KeyHash is 32 hex characters from the first 128 SHA-256 bits on emitted events.
	// It supports stable correlation but is unkeyed and guessable for known keys.
	KeyHash string
}

// String formats fields literally without validating, bounding or redacting
// caller-supplied values. Package-emitted values use fixed labels and key hashes;
// never treat arbitrary constructed Event fields as trusted diagnostic text.
func (event Event) String() string {
	return fmt.Sprintf("lease operation=%s outcome=%s key_hash=%s", event.Operation, event.Outcome, event.KeyHash)
}

// Observer consumes best-effort events asynchronously after the backend call.
// Each registration permits one in-flight callback and drops events while busy;
// registering the same object more than once or in multiple decorators can call
// it concurrently. Callbacks must terminate: there is no context, join or closer,
// and a blocked callback retains its goroutine/slot. Decorator calls recover panics.
type Observer interface {
	// Observe consumes one value; no ordered, durable or complete event delivery
	// is guaranteed. Shared implementations own synchronization and diagnostics.
	Observe(Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Event)

// Observe directly invokes the function without nil/panic handling. The
// NewObservedBackend callback boundary, not this adapter method, recovers panics.
func (observer ObserverFunc) Observe(event Event) { observer(event) }

type observedBackend struct {
	backend Backend
	clock   Clock
	slots   []observerSlot
}

type observerSlot struct {
	observer Observer
	active   chan struct{}
}

// NewObservedBackend borrows backend, clock and one to sixteen observers,
// snapshotting their registrations. Nil interfaces are rejected as ErrInvalidState;
// typed-nil usability is not inspected. Backend results/errors remain unchanged.
// Clock/hash work is synchronous; observer callbacks are nonblocking best effort.
// No resources are closed or joined by this decoration; callbacks own termination.
func NewObservedBackend(backend Backend, clock Clock, observers ...Observer) (Backend, error) {
	if backend == nil || clock == nil || len(observers) == 0 || len(observers) > 16 {
		return nil, Wrap(ErrInvalidState, "observation options")
	}
	for _, observer := range observers {
		if observer == nil {
			return nil, Wrap(ErrInvalidState, "nil observer")
		}
	}
	slots := make([]observerSlot, len(observers))
	for index, observer := range observers {
		slots[index] = observerSlot{observer: observer, active: make(chan struct{}, 1)}
	}
	return &observedBackend{backend: backend, clock: clock, slots: slots}, nil
}

func (backend *observedBackend) TryAcquire(
	ctx context.Context, key Key, owner string, ttl time.Duration,
) (Record, error) {
	record, err := backend.backend.TryAcquire(ctx, key, owner, ttl)
	backend.emit(OperationAcquire, key, err)
	return record, err
}

func (backend *observedBackend) Renew(
	ctx context.Context, record Record, ttl time.Duration,
) (Record, error) {
	next, err := backend.backend.Renew(ctx, record, ttl)
	backend.emit(OperationRenew, record.Key, err)
	return next, err
}

func (backend *observedBackend) Validate(ctx context.Context, record Record) (Record, error) {
	next, err := backend.backend.Validate(ctx, record)
	backend.emit(OperationValidate, record.Key, err)
	return next, err
}

func (backend *observedBackend) Release(ctx context.Context, record Record) error {
	err := backend.backend.Release(ctx, record)
	backend.emit(OperationRelease, record.Key, err)
	return err
}

func (backend *observedBackend) emit(operation Operation, key Key, err error) {
	sum := sha256.Sum256([]byte(key.String()))
	event := Event{
		At: backend.clock.Now(), Operation: operation,
		Outcome: classifyOutcome(err), KeyHash: hex.EncodeToString(sum[:16]),
	}
	for _, slot := range backend.slots {
		select {
		case slot.active <- struct{}{}:
			go func() {
				defer func() { <-slot.active }()
				callObserver(slot.observer, event)
			}()
		default:
			// Observations are best effort and never delay lease transitions.
		}
	}
}

func callObserver(observer Observer, event Event) {
	defer func() { _ = recover() }()
	observer.Observe(event)
}

func classifyOutcome(err error) Outcome {
	switch {
	case err == nil:
		return OutcomeSuccess
	case errors.Is(err, ErrContended), errors.Is(err, ErrTimeout):
		return OutcomeContended
	case errors.Is(err, ErrStaleOwner), errors.Is(err, ErrLost):
		return OutcomeStale
	case errors.Is(err, ErrCanceled):
		return OutcomeCanceled
	case errors.Is(err, ErrAmbiguousOutcome):
		return OutcomeAmbiguous
	case errors.Is(err, ErrInvalidState):
		return OutcomeInvalid
	default:
		return OutcomeUnavailable
	}
}
