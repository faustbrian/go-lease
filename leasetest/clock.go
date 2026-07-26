// Package leasetest provides deterministic lease conformance utilities.
package leasetest

import (
	"sync"
	"time"
)

// Clock is a concurrency-safe manually advanced clock.
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

// Advance moves the clock forward by duration.
func (clock *Clock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}
