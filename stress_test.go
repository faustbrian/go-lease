package lease_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
	"github.com/faustbrian/golib/pkg/lease/memory"
)

func TestContentionElectsExactlyOneOwner(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	key, _ := lease.NewKey("stress", "leader")
	var winners atomic.Int64
	var wait sync.WaitGroup
	for index := 0; index < 64; index++ {
		wait.Add(1)
		go func(owner int) {
			defer wait.Done()
			if _, err := store.TryAcquire(
				context.Background(), key, strconv.Itoa(owner), time.Second,
			); err == nil {
				winners.Add(1)
			} else if !errors.Is(err, lease.ErrContended) {
				t.Errorf("TryAcquire() error = %v", err)
			}
		}(index)
	}
	wait.Wait()
	if winners.Load() != 1 {
		t.Fatalf("winners = %d", winners.Load())
	}
}
