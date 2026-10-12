package lease

import "time"

// Token is a nonzero per-key fencing value within one backend continuity epoch.
// It need not be consecutive, is not caller authentication, and only protects
// effects when the resource atomically compares and retains accepted fences.
type Token uint64

// Record is a value snapshot returned by a backend, not an authenticated proof.
// Callers can construct or alter one; backends compare its coordinates with
// stored ownership. Timestamp fields alone do not establish current admission.
type Record struct {
	// Key is the namespaced identity supplied to acquisition.
	Key Key
	// Owner is the opaque comparison identity, not an authorization principal.
	Owner string
	// Token is the acquisition fence retained across renewal.
	Token Token
	// AcquiredAt is backend-clock acquisition time, retained across renewal.
	AcquiredAt time.Time
	// ExpiresAt is backend-clock expiry, not a local wall/monotonic deadline.
	ExpiresAt time.Time
}

// SafeDeadline returns the backend-clock expiry after reserving margin.
// It performs no validation; margin is applied literally. Handle.State checks
// both local clock bounds rather than comparing backend timestamps directly.
func (record Record) SafeDeadline(margin time.Duration) time.Time {
	return record.ExpiresAt.Add(-margin)
}

// UsableAt compares a backend-clock time with the backend-clock safe deadline.
// Handle.State is the authoritative local admission check.
func (record Record) UsableAt(now time.Time, margin time.Duration) bool {
	return now.Before(record.SafeDeadline(margin))
}
