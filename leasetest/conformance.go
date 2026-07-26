package leasetest

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
)

// BackendFixture supplies one isolated backend and an expiry transition.
type BackendFixture struct {
	Backend lease.Backend
	Expire  func(time.Duration)
}

// RunBackendConformance proves the shared fencing and stale-owner contract.
func RunBackendConformance(
	t *testing.T,
	factory func(*testing.T) BackendFixture,
) {
	t.Helper()
	t.Run("ownership and successor protection", func(t *testing.T) {
		fixture := factory(t)
		key, err := lease.NewKey("conformance", t.Name())
		if err != nil {
			t.Fatalf("NewKey() error = %v", err)
		}
		first, err := fixture.Backend.TryAcquire(
			context.Background(), key, "owner-a", 100*time.Millisecond,
		)
		if err != nil {
			t.Fatalf("TryAcquire(first) error = %v", err)
		}
		if _, err := fixture.Backend.TryAcquire(
			context.Background(), key, "owner-b", time.Second,
		); !errors.Is(err, lease.ErrContended) {
			t.Fatalf("TryAcquire(contended) error = %v", err)
		}
		renewed, err := fixture.Backend.Renew(
			context.Background(), first, 100*time.Millisecond,
		)
		if err != nil {
			t.Fatalf("Renew() error = %v", err)
		}
		if _, err := fixture.Backend.Validate(context.Background(), renewed); err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
		fixture.Expire(110 * time.Millisecond)
		second, err := fixture.Backend.TryAcquire(
			context.Background(), key, "owner-b", time.Second,
		)
		if err != nil {
			t.Fatalf("TryAcquire(successor) error = %v", err)
		}
		if second.Token <= first.Token {
			t.Fatalf("successor token %d <= first token %d", second.Token, first.Token)
		}
		wrongOwner := second
		wrongOwner.Owner = "owner-forged"
		wrongToken := second
		wrongToken.Token = first.Token
		for name, forged := range map[string]lease.Record{
			"owner only": wrongOwner,
			"token only": wrongToken,
		} {
			if _, err := fixture.Backend.Renew(
				context.Background(), forged, time.Second,
			); !errors.Is(err, lease.ErrStaleOwner) {
				t.Fatalf("Renew(%s mismatch) error = %v", name, err)
			}
			if _, err := fixture.Backend.Validate(
				context.Background(), forged,
			); !errors.Is(err, lease.ErrStaleOwner) {
				t.Fatalf("Validate(%s mismatch) error = %v", name, err)
			}
			if err := fixture.Backend.Release(
				context.Background(), forged,
			); !errors.Is(err, lease.ErrStaleOwner) {
				t.Fatalf("Release(%s mismatch) error = %v", name, err)
			}
			if _, err := fixture.Backend.Validate(
				context.Background(), second,
			); err != nil {
				t.Fatalf("Validate(successor after %s mismatch) error = %v", name, err)
			}
		}
		if _, err := fixture.Backend.Renew(
			context.Background(), first, time.Second,
		); !errors.Is(err, lease.ErrStaleOwner) {
			t.Fatalf("Renew(stale) error = %v", err)
		}
		if err := fixture.Backend.Release(
			context.Background(), first,
		); !errors.Is(err, lease.ErrStaleOwner) {
			t.Fatalf("Release(stale) error = %v", err)
		}
		if _, err := fixture.Backend.Validate(context.Background(), second); err != nil {
			t.Fatalf("Validate(successor) error = %v", err)
		}
		if err := fixture.Backend.Release(context.Background(), second); err != nil {
			t.Fatalf("Release(successor) error = %v", err)
		}
		if err := fixture.Backend.Release(context.Background(), second); err != nil {
			t.Fatalf("Release(idempotent) error = %v", err)
		}
	})
}
