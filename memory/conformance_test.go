package memory_test

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/lease/leasetest"
	"github.com/faustbrian/golib/pkg/lease/memory"
)

func TestBackendConformance(t *testing.T) {
	t.Parallel()

	leasetest.RunBackendConformance(t, func(t *testing.T) leasetest.BackendFixture {
		t.Helper()
		clock := leasetest.NewClock(time.Now())
		store, err := memory.New(memory.Options{Clock: clock, MaxKeys: 10})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		return leasetest.BackendFixture{Backend: store, Expire: clock.Advance}
	})
}
