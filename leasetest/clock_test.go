package leasetest_test

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/lease/leasetest"
)

func TestClockSupportsForwardAndRollbackFaults(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	clock := leasetest.NewClock(start)
	clock.Advance(time.Hour)
	clock.Advance(-2 * time.Hour)
	if got := clock.Now(); !got.Equal(start.Add(-time.Hour)) {
		t.Fatalf("Now() = %v", got)
	}
}
