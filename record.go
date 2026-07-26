package lease

import "time"

// Token is a monotonically increasing fencing value for one key.
type Token uint64

// Record is the backend-authenticated ownership snapshot for a lease.
type Record struct {
	Key        Key
	Owner      string
	Token      Token
	AcquiredAt time.Time
	ExpiresAt  time.Time
}

// SafeDeadline returns the backend-clock expiry after reserving margin.
// Handle.Deadline is the authoritative local admission bound.
func (record Record) SafeDeadline(margin time.Duration) time.Time {
	return record.ExpiresAt.Add(-margin)
}

// UsableAt compares a backend-clock time with the backend-clock safe deadline.
// Handle.State is the authoritative local admission check.
func (record Record) UsableAt(now time.Time, margin time.Duration) bool {
	return now.Before(record.SafeDeadline(margin))
}
