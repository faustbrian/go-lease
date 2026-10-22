package leaseservice_test

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/go-lease/v3"
	leaseservice "github.com/faustbrian/go-lease/v3/adapters/service"
	"github.com/faustbrian/go-lease/v3/leasetest"
	"github.com/faustbrian/go-lease/v3/memory"
	serviceintegration "github.com/faustbrian/go-service/v3/integration"
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
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancelShutdown)
	if err := manager.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if _, err := client.TryAcquire(context.Background(), first, policy); err != nil {
		t.Fatalf("lease remained held after shutdown: %v", err)
	}
	if manager.Active() != 0 {
		t.Fatalf("Active() = %d", manager.Active())
	}
}

// The hooks are the go-service v3 boundary; Stop must release every held lease
// through the service component so a successor can acquire the key.
func TestHooksStopReleasesThroughServiceComponent(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	manager, err := leaseservice.New(client, 1)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	component, err := serviceintegration.New("leases", manager.Hooks())
	if err != nil {
		t.Fatalf("integration.New() error = %v", err)
	}
	if err := component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, Retry: time.Millisecond, MaxAttempts: 1,
	})
	key, _ := lease.NewKey("service", "held")
	held, err := manager.Acquire(context.Background(), key, policy)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	stopContext, cancelStop := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancelStop)
	if err := component.Stop(stopContext); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if manager.Active() != 0 {
		t.Fatalf("Active() = %d after Stop", manager.Active())
	}
	successor, err := client.TryAcquire(context.Background(), key, policy)
	if err != nil {
		t.Fatalf("lease remained held after Stop: %v", err)
	}
	if successor.Token() <= held.Token() {
		t.Fatalf("successor token = %d, want above %d", successor.Token(), held.Token())
	}
}
