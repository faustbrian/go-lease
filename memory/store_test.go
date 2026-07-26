package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
	"github.com/faustbrian/golib/pkg/lease/memory"
)

func TestStaleOwnerCannotAffectSuccessor(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC))
	store, err := memory.New(memory.Options{Clock: clock, MaxKeys: 10})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	key, _ := lease.NewKey("scheduler", "report")
	first, err := store.TryAcquire(context.Background(), key, "owner-a", time.Second)
	if err != nil {
		t.Fatalf("first TryAcquire() error = %v", err)
	}
	if _, err := store.TryAcquire(context.Background(), key, "owner-b", time.Second); !errors.Is(err, lease.ErrContended) {
		t.Fatalf("contended TryAcquire() error = %v", err)
	}

	clock.Advance(time.Second)
	second, err := store.TryAcquire(context.Background(), key, "owner-b", time.Second)
	if err != nil {
		t.Fatalf("successor TryAcquire() error = %v", err)
	}
	if second.Token <= first.Token {
		t.Fatalf("successor token %d <= first token %d", second.Token, first.Token)
	}
	if _, err := store.Renew(context.Background(), first, time.Second); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("stale Renew() error = %v", err)
	}
	if err := store.Release(context.Background(), first); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("stale Release() error = %v", err)
	}
	current, err := store.Validate(context.Background(), second)
	if err != nil || current.Owner != "owner-b" {
		t.Fatalf("Validate(successor) = %+v, %v", current, err)
	}
}

func TestReleaseIsIdempotentForSameOwner(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	key, _ := lease.NewKey("queue", "unique")
	record, _ := store.TryAcquire(context.Background(), key, "owner", time.Second)
	if err := store.Release(context.Background(), record); err != nil {
		t.Fatalf("first Release() error = %v", err)
	}
	if err := store.Release(context.Background(), record); err != nil {
		t.Fatalf("second Release() error = %v", err)
	}
}
