package lease_test

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
	"github.com/faustbrian/golib/pkg/lease/memory"
)

type failingRenewBackend struct{ lease.Backend }

func (backend failingRenewBackend) Renew(
	context.Context,
	lease.Record,
	time.Duration,
) (lease.Record, error) {
	return lease.Record{}, lease.ErrAmbiguousOutcome
}

type triggeredSleeper struct{ trigger <-chan struct{} }

func (sleeper triggeredSleeper) Sleep(ctx context.Context, _ time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-sleeper.trigger:
		return nil
	}
}

type blockedSleeper struct{ release <-chan struct{} }

func (sleeper blockedSleeper) Sleep(context.Context, time.Duration) error {
	<-sleeper.release
	return context.Canceled
}

func TestManagedRenewalReportsUncertaintyAndStopsAdmission(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	trigger := make(chan struct{}, 1)
	client, _ := lease.NewClient(failingRenewBackend{Backend: store}, lease.ClientOptions{
		Clock: clock, Owners: ownerSource{next: "owner"},
		Sleeper: triggeredSleeper{trigger: trigger},
	})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, Retry: time.Millisecond, RenewEvery: 200 * time.Millisecond,
		SafetyMargin: 100 * time.Millisecond, MaxAttempts: 1,
	})
	key, _ := lease.NewKey("service", "leader")
	handle, _ := client.TryAcquire(context.Background(), key, policy)
	managed, err := handle.StartManaged(context.Background())
	if err != nil {
		t.Fatalf("StartManaged() error = %v", err)
	}
	trigger <- struct{}{}
	select {
	case loss := <-managed.Loss():
		if !errors.Is(loss.Err, lease.ErrAmbiguousOutcome) || loss.State != lease.StateUncertain {
			t.Fatalf("loss = %+v", loss)
		}
	case <-time.After(time.Second):
		t.Fatal("managed renewal did not report loss")
	}
	if handle.State() != lease.StateUncertain {
		t.Fatalf("State() = %s", handle.State())
	}
}

func TestManagedStopDoesNotPretendToRelease(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	trigger := make(chan struct{})
	client, _ := lease.NewClient(store, lease.ClientOptions{
		Clock: clock, Owners: ownerSource{next: "owner"},
		Sleeper: triggeredSleeper{trigger: trigger},
	})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, Retry: time.Millisecond, RenewEvery: 200 * time.Millisecond,
		SafetyMargin: 100 * time.Millisecond, MaxAttempts: 1,
	})
	key, _ := lease.NewKey("service", "worker")
	handle, _ := client.TryAcquire(context.Background(), key, policy)
	managed, _ := handle.StartManaged(context.Background())
	if err := managed.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if handle.State() != lease.StateActive {
		t.Fatalf("Stop changed ownership state to %s", handle.State())
	}
	if err := handle.Release(context.Background()); err != nil {
		t.Fatalf("explicit Release() error = %v", err)
	}
}

func TestManagedStopHonorsCallerDeadline(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	release := make(chan struct{})
	client, _ := lease.NewClient(store, lease.ClientOptions{
		Clock: clock, Owners: ownerSource{next: "owner"}, Sleeper: blockedSleeper{release: release},
	})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	key, _ := lease.NewKey("service", "blocked")
	handle, _ := client.TryAcquire(context.Background(), key, policy)
	managed, _ := handle.StartManaged(context.Background())
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := managed.Stop(canceled); !errors.Is(err, lease.ErrCanceled) {
		t.Fatalf("Stop(canceled) error = %v", err)
	}
	close(release)
	select {
	case <-managed.Loss():
	case <-time.After(time.Second):
		t.Fatal("managed goroutine did not stop")
	}
}

func TestClientBoundsManagedRenewalGoroutines(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 2})
	trigger := make(chan struct{})
	client, _ := lease.NewClient(store, lease.ClientOptions{
		Clock: clock, Owners: ownerSource{next: "owner"},
		Sleeper: triggeredSleeper{trigger: trigger}, MaxManaged: 1,
	})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	firstKey, _ := lease.NewKey("managed", "first")
	secondKey, _ := lease.NewKey("managed", "second")
	first, _ := client.TryAcquire(context.Background(), firstKey, policy)
	second, _ := client.TryAcquire(context.Background(), secondKey, policy)
	managed, err := first.StartManaged(context.Background())
	if err != nil {
		t.Fatalf("StartManaged(first) error = %v", err)
	}
	if _, err := second.StartManaged(context.Background()); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("StartManaged(over capacity) error = %v", err)
	}
	if err := managed.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}
