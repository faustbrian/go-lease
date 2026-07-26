package leaseservice_test

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leaseservice"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
	"github.com/faustbrian/golib/pkg/lease/memory"
)

func TestManagerBoundsHandlesAndReleasesOnShutdown(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 2})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	manager, err := leaseservice.New(client, 1)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, Retry: time.Millisecond, MaxAttempts: 1,
	})
	first, _ := lease.NewKey("service", "first")
	second, _ := lease.NewKey("service", "second")
	if _, err := manager.Acquire(context.Background(), first, policy); err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	if _, err := manager.Acquire(context.Background(), second, policy); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("Acquire(over capacity) error = %v", err)
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if _, err := client.TryAcquire(context.Background(), first, policy); err != nil {
		t.Fatalf("lease remained held after shutdown: %v", err)
	}
	if manager.Active() != 0 {
		t.Fatalf("Active() = %d", manager.Active())
	}
}

func TestHooksExposeBoundedShutdown(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	manager, _ := leaseservice.New(client, 1)
	hooks := manager.Hooks()
	if hooks.Start == nil || hooks.Stop == nil {
		t.Fatal("Hooks() returned incomplete lifecycle hooks")
	}
	if err := hooks.Start(context.Background()); err != nil {
		t.Fatalf("Start hook error = %v", err)
	}
}
