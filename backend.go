package lease

import (
	"context"
	"time"
)

// Clock supplies injected local or memory-backend time. Shared implementations
// must tolerate concurrent calls, return promptly and not re-enter a handle or
// memory store: Now is also called while their state locks are held. A clock
// supplies a time domain, not remote server synchronization or pause protection.
type Clock interface {
	// Now returns the current instant in this clock's domain. Production local
	// admission also checks real time so a frozen test clock cannot extend use.
	Now() time.Time
}

// Backend owns atomic per-key owner/token comparisons and backend-clock expiry.
// Implementations must support concurrent clients, cooperate with nonnil bounded
// contexts and preserve strictly increasing acquisition tokens within a retained
// continuity epoch. An uncertain mutation must not be reported as definite
// absence; ErrAmbiguousOutcome requires reconciliation, not automatic replay.
// Calls are synchronous and panics/uncooperative implementations are not contained.
// The caller owns backend clients, authorization, storage durability and shutdown;
// this interface cannot authenticate a caller or fence the protected resource.
type Backend interface {
	// TryAcquire installs owner only if no usable lease exists, otherwise reports
	// ErrContended. Return matching key/owner, a nonzero fence and ordered backend
	// timestamps. Repeating an acquisition is not an idempotent success contract.
	TryAcquire(context.Context, Key, string, time.Duration) (Record, error)
	// Renew compares key, owner and token while unexpired, then extends expiry
	// using backend time without changing token or acquisition timestamp.
	Renew(context.Context, Record, time.Duration) (Record, error)
	// Validate returns a current unexpired matching record without extending it.
	// Success is an instantaneous check, not a reservation for a later effect.
	Validate(context.Context, Record) (Record, error)
	// Release conditionally deactivates matching ownership without affecting a
	// successor. Repeating a successful release is idempotent; missing-record
	// handling differs by backend. Retain fencing history independently.
	Release(context.Context, Record) error
}
