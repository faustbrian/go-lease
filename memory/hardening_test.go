package memory

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
)

func TestOptionsCapacityAndOverflowAreBounded(t *testing.T) {
	t.Parallel()

	if _, err := New(Options{}); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("New(invalid) error = %v", err)
	}
	clock := leasetest.NewClock(time.Now())
	if _, err := New(Options{Clock: clock}); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("New(zero capacity) error = %v", err)
	}
	if _, err := New(Options{MaxKeys: 1}); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("New(nil clock) error = %v", err)
	}
	store, _ := New(Options{Clock: clock, MaxKeys: 1})
	first, _ := lease.NewKey("memory", "first")
	second, _ := lease.NewKey("memory", "second")
	if _, err := store.TryAcquire(context.Background(), first, "owner", time.Second); err != nil {
		t.Fatalf("TryAcquire(first) error = %v", err)
	}
	if _, err := store.TryAcquire(context.Background(), second, "owner", time.Second); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("TryAcquire(capacity) error = %v", err)
	}
	store.entries[first.String()] = entry{record: lease.Record{Key: first, Token: lease.Token(math.MaxUint64)}}
	if _, err := store.TryAcquire(context.Background(), first, "owner", time.Second); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("TryAcquire(overflow) error = %v", err)
	}
}

func TestCurrentOwnerCanRenewValidateAndRelease(t *testing.T) {
	t.Parallel()

	now := time.Now()
	clock := leasetest.NewClock(now)
	store, _ := New(Options{Clock: clock, MaxKeys: 1})
	key, _ := lease.NewKey("memory", "current-owner")
	record, _ := store.TryAcquire(context.Background(), key, "owner", time.Second)
	clock.Advance(100 * time.Millisecond)
	renewed, err := store.Renew(context.Background(), record, 2*time.Second)
	if err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
	if expected := now.Add(2100 * time.Millisecond); !renewed.ExpiresAt.Equal(expected) {
		t.Fatalf("Renew() expiry = %v, want %v", renewed.ExpiresAt, expected)
	}
	if _, err := store.Validate(context.Background(), renewed); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := store.Release(context.Background(), renewed); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := store.Renew(context.Background(), renewed, time.Second); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Renew(released) error = %v", err)
	}
	wrong := renewed
	wrong.Owner = "successor"
	if err := store.Release(context.Background(), wrong); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Release(released wrong owner) error = %v", err)
	}
	missingKey, _ := lease.NewKey("memory", "missing")
	missing := renewed
	missing.Key = missingKey
	if _, err := store.Validate(context.Background(), missing); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Validate(missing) error = %v", err)
	}
}

func TestOperationsRejectInvalidCanceledAndStaleInputs(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := New(Options{Clock: clock, MaxKeys: 2})
	key, _ := lease.NewKey("memory", "edge")
	zero := lease.Record{Key: key, Owner: "owner"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.TryAcquire(ctx, key, "owner", time.Second); !errors.Is(err, lease.ErrCanceled) {
		t.Fatalf("TryAcquire(canceled) error = %v", err)
	}
	if _, err := store.TryAcquire(context.Background(), lease.Key{}, "owner", time.Second); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("TryAcquire(empty key) error = %v", err)
	}
	if _, err := store.TryAcquire(context.Background(), key, strings.Repeat("x", 129), time.Second); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("TryAcquire(owner) error = %v", err)
	}
	boundaryKey, _ := lease.NewKey("memory", "owner-boundary")
	if _, err := store.TryAcquire(context.Background(), boundaryKey, strings.Repeat("x", 128), time.Second); err != nil {
		t.Fatalf("TryAcquire(128-byte owner) error = %v", err)
	}
	if _, err := store.TryAcquire(context.Background(), key, "owner", 0); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("TryAcquire(ttl) error = %v", err)
	}
	if _, err := store.Renew(context.Background(), zero, time.Second); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Renew(zero token) error = %v", err)
	}
	if _, err := store.Renew(ctx, lease.Record{Key: key, Owner: "owner", Token: 1}, time.Second); !errors.Is(err, lease.ErrCanceled) {
		t.Fatalf("Renew(canceled) error = %v", err)
	}
	if _, err := store.Renew(context.Background(), lease.Record{Key: key, Owner: "owner", Token: 1}, 0); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Renew(ttl) error = %v", err)
	}
	if _, err := store.Renew(context.Background(), lease.Record{Key: key, Owner: "owner", Token: 1}, time.Second); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Renew(missing) error = %v", err)
	}
	if _, err := store.Validate(context.Background(), zero); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Validate(zero token) error = %v", err)
	}
	if err := store.Release(context.Background(), zero); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Release(zero token) error = %v", err)
	}
	if err := store.Release(context.Background(), lease.Record{Key: key, Owner: "owner", Token: 1}); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Release(missing) error = %v", err)
	}
}

func TestExpiredAndReleasedRecordsRejectRenewalAndValidation(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := New(Options{Clock: clock, MaxKeys: 1})
	key, _ := lease.NewKey("memory", "expiry")
	record, _ := store.TryAcquire(context.Background(), key, "owner", time.Second)
	wrong := record
	wrong.Owner = "other"
	if _, err := store.Renew(context.Background(), wrong, time.Second); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Renew(wrong owner) error = %v", err)
	}
	clock.Advance(time.Second)
	if _, err := store.Renew(context.Background(), record, time.Second); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Renew(expired) error = %v", err)
	}
	if _, err := store.Validate(context.Background(), record); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Validate(expired) error = %v", err)
	}
	if err := store.Release(context.Background(), record); err != nil {
		t.Fatalf("Release(expired) error = %v", err)
	}
	if _, err := store.Validate(context.Background(), record); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Validate(released) error = %v", err)
	}
}
