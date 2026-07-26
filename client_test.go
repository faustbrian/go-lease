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

type ownerSource struct{ next string }

func (source ownerSource) NewOwner() (string, error) { return source.next, nil }

type advancingSleeper struct{ clock *leasetest.Clock }

func (sleeper advancingSleeper) Sleep(_ context.Context, duration time.Duration) error {
	sleeper.clock.Advance(duration)
	return nil
}

type fixedRetry struct{ duration time.Duration }

func (source fixedRetry) Jitter(time.Duration) time.Duration { return source.duration }

type contextSleeper struct{}

func (contextSleeper) Sleep(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}

type contendedBackend struct{}

func (contendedBackend) TryAcquire(
	context.Context,
	lease.Key,
	string,
	time.Duration,
) (lease.Record, error) {
	return lease.Record{}, lease.ErrContended
}
func (contendedBackend) Renew(context.Context, lease.Record, time.Duration) (lease.Record, error) {
	return lease.Record{}, lease.ErrStaleOwner
}
func (contendedBackend) Validate(context.Context, lease.Record) (lease.Record, error) {
	return lease.Record{}, lease.ErrStaleOwner
}
func (contendedBackend) Release(context.Context, lease.Record) error { return nil }

type blockingBackend struct{ entered chan struct{} }

func (backend blockingBackend) TryAcquire(ctx context.Context, _ lease.Key, _ string, _ time.Duration) (lease.Record, error) {
	select {
	case backend.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return lease.Record{}, ctx.Err()
}
func (blockingBackend) Renew(context.Context, lease.Record, time.Duration) (lease.Record, error) {
	return lease.Record{}, lease.ErrStaleOwner
}
func (blockingBackend) Validate(context.Context, lease.Record) (lease.Record, error) {
	return lease.Record{}, lease.ErrStaleOwner
}
func (blockingBackend) Release(context.Context, lease.Record) error { return nil }

type recordingSleeper struct {
	clock *leasetest.Clock
	got   []time.Duration
}

func (sleeper *recordingSleeper) Sleep(_ context.Context, duration time.Duration) error {
	sleeper.got = append(sleeper.got, duration)
	sleeper.clock.Advance(duration)
	return nil
}

func newPolicy(t *testing.T, wait time.Duration) lease.Policy {
	t.Helper()
	policy, err := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, Wait: wait, Retry: 100 * time.Millisecond,
		SafetyMargin: 100 * time.Millisecond, MaxAttempts: 20,
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	return policy
}

func TestHandleLifecycleFailsClosed(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC))
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 10})
	client, err := lease.NewClient(store, lease.ClientOptions{
		Clock: clock, Owners: ownerSource{next: "owner-a"},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	key, _ := lease.NewKey("maintenance", "cleanup")
	handle, err := client.TryAcquire(context.Background(), key, newPolicy(t, 0))
	if err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	if handle.Owner() != "owner-a" || handle.Token() != 1 || handle.State() != lease.StateActive {
		t.Fatalf("unexpected handle: owner=%q token=%d state=%s", handle.Owner(), handle.Token(), handle.State())
	}
	clock.Advance(500 * time.Millisecond)
	if err := handle.Renew(context.Background()); err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
	if err := handle.Validate(context.Background()); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := handle.Release(context.Background()); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := handle.Release(context.Background()); err != nil {
		t.Fatalf("idempotent Release() error = %v", err)
	}
	if handle.State() != lease.StateReleased {
		t.Fatalf("State() = %s", handle.State())
	}
	if err := handle.Validate(context.Background()); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Validate(released) error = %v", err)
	}
}

func TestAcquireRetriesWithinWaitBound(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC))
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 10})
	key, _ := lease.NewKey("queue", "job")
	_, _ = store.TryAcquire(context.Background(), key, "first", 200*time.Millisecond)
	client, _ := lease.NewClient(store, lease.ClientOptions{
		Clock: clock, Owners: ownerSource{next: "second"},
		Sleeper: advancingSleeper{clock: clock},
	})
	handle, err := client.Acquire(context.Background(), key, newPolicy(t, time.Second))
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if handle.Token() != 2 {
		t.Fatalf("Token() = %d", handle.Token())
	}
}

func TestExpiredHandleStopsAdmission(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	client, _ := lease.NewClient(store, lease.ClientOptions{
		Clock: clock, Owners: ownerSource{next: "owner"},
	})
	key, _ := lease.NewKey("scheduler", "tick")
	handle, _ := client.TryAcquire(context.Background(), key, newPolicy(t, 0))
	clock.Advance(900 * time.Millisecond)
	if handle.State() != lease.StateExpired {
		t.Fatalf("State() = %s", handle.State())
	}
	if err := handle.Validate(context.Background()); !errors.Is(err, lease.ErrLost) {
		t.Fatalf("Validate(expired) error = %v", err)
	}
}

func TestAcquireUsesInjectedBoundedJitter(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	sleeper := &recordingSleeper{clock: clock}
	client, _ := lease.NewClient(contendedBackend{}, lease.ClientOptions{
		Clock: clock, Owners: ownerSource{next: "owner"}, Sleeper: sleeper,
		Retry: fixedRetry{duration: 20 * time.Millisecond},
	})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, Wait: time.Second, Retry: 50 * time.Millisecond,
		Jitter: 20 * time.Millisecond, MaxAttempts: 2,
	})
	key, _ := lease.NewKey("queue", "contended")
	if _, err := client.Acquire(context.Background(), key, policy); !errors.Is(err, lease.ErrTimeout) {
		t.Fatalf("Acquire() error = %v", err)
	}
	if len(sleeper.got) != 1 || sleeper.got[0] != 70*time.Millisecond {
		t.Fatalf("retry delays = %v", sleeper.got)
	}
}

func TestAcquireHonorsWaitWhenInjectedClockIsFrozen(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	client, _ := lease.NewClient(contendedBackend{}, lease.ClientOptions{
		Clock: clock, Owners: ownerSource{next: "owner"}, Sleeper: contextSleeper{},
	})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, Wait: 20 * time.Millisecond, Retry: time.Millisecond,
		MaxAttempts: 10_000,
	})
	key, _ := lease.NewKey("clock", "frozen")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := client.Acquire(ctx, key, policy); !errors.Is(err, lease.ErrTimeout) {
		t.Fatalf("Acquire() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Acquire() exceeded wait bound: %s", elapsed)
	}
}

func TestAcquireWaitBoundsOneSlowBackendAttempt(t *testing.T) {
	t.Parallel()

	backend := blockingBackend{entered: make(chan struct{}, 1)}
	client, _ := lease.NewClient(backend, lease.ClientOptions{})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, Wait: 20 * time.Millisecond, Retry: time.Millisecond,
		OperationTimeout: time.Second, MaxAttempts: 10_000,
	})
	key, _ := lease.NewKey("clock", "slow-backend")
	started := time.Now()
	if _, err := client.Acquire(context.Background(), key, policy); !errors.Is(err, lease.ErrTimeout) {
		t.Fatalf("Acquire() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Acquire() exceeded wait bound: %s", elapsed)
	}
}

func TestClientBoundsWaitersAndBackendOperationTime(t *testing.T) {
	t.Parallel()

	backend := blockingBackend{entered: make(chan struct{}, 1)}
	client, err := lease.NewClient(backend, lease.ClientOptions{MaxWaiters: 1})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, Wait: time.Second, Retry: time.Millisecond,
		OperationTimeout: 20 * time.Millisecond, MaxAttempts: 2,
	})
	key, _ := lease.NewKey("waiters", "bounded")
	first := make(chan error, 1)
	go func() {
		_, err := client.Acquire(context.Background(), key, policy)
		first <- err
	}()
	<-backend.entered
	if _, err := client.Acquire(context.Background(), key, policy); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("Acquire(over waiter capacity) error = %v", err)
	}
	if err := <-first; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire(operation timeout) error = %v", err)
	}
}
