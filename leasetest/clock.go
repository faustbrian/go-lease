// Package leasetest provides a manual Clock and one shared backend assertion
// suite. NewClock supplies deterministic time; RunBackendConformance borrows a
// caller-created isolated fixture. Helpers introduce no services/cgo themselves
// and do not qualify durability, partitions or every backend behavior. The caller
// owns fixture resources/expiry transitions and any external test infrastructure.
package leasetest

import (
	"sync"
	"time"
)

// Clock is a concurrency-safe manual clock, frozen between Advance calls.
// It supports backward fault jumps and is not a production wall-time source.
// Its zero value starts at time.Time{}; NewClock selects a deliberate instant.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a clock fixed at the supplied instant.
func NewClock(now time.Time) *Clock { return &Clock{now: now} }

// Now returns the current test instant.
func (clock *Clock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

// Advance adds duration literally, including negative rollback fault jumps.
// It does not sleep, schedule timers or move the separate real-time handle guard.
func (clock *Clock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}
