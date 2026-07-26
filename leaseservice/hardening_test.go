package leaseservice

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
)

type serviceClock struct{ now time.Time }

func (clock serviceClock) Now() time.Time { return clock.now }

type serviceBackend struct {
	now          time.Time
	acquireErr   error
	releaseErr   error
	entered      chan struct{}
	proceed      chan struct{}
	expired      bool
	renewed      chan struct{}
	afterAcquire func()
}

func (backend *serviceBackend) TryAcquire(
	_ context.Context, key lease.Key, owner string, ttl time.Duration,
) (lease.Record, error) {
	if backend.entered != nil {
		close(backend.entered)
		<-backend.proceed
	}
	if backend.acquireErr != nil {
		return lease.Record{}, backend.acquireErr
	}
	expires := backend.now.Add(ttl)
	if backend.expired {
		expires = backend.now.Add(-time.Second)
	}
	if backend.afterAcquire != nil {
		backend.afterAcquire()
	}
	return lease.Record{Key: key, Owner: owner, Token: 1, AcquiredAt: backend.now, ExpiresAt: expires}, nil
}
func (backend *serviceBackend) Renew(
	_ context.Context,
	record lease.Record,
	ttl time.Duration,
) (lease.Record, error) {
	if backend.renewed != nil {
		backend.renewed <- struct{}{}
	}
	record.ExpiresAt = backend.now.Add(ttl)
	return record, nil
}

type serviceSleeper struct{ trigger <-chan struct{} }

func (sleeper serviceSleeper) Sleep(ctx context.Context, _ time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-sleeper.trigger:
		return nil
	}
}
func (backend *serviceBackend) Validate(_ context.Context, record lease.Record) (lease.Record, error) {
	return record, nil
}
func (backend *serviceBackend) Release(context.Context, lease.Record) error {
	return backend.releaseErr
}

func TestManagerValidationFailureAndClosedState(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, 1); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("New(nil) error = %v", err)
	}
	backend := &serviceBackend{now: time.Now(), acquireErr: lease.ErrBackendUnavailable}
	client, _ := lease.NewClient(backend, lease.ClientOptions{Clock: serviceClock{backend.now}})
	if _, err := New(client, 0); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("New(zero) error = %v", err)
	}
	manager, _ := New(client, 1)
	key, _ := lease.NewKey("service", "failure")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	if _, err := manager.Acquire(context.Background(), key, policy); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("Acquire(backend) error = %v", err)
	}
	if manager.Active() != 0 {
		t.Fatalf("Active() after failure = %d", manager.Active())
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if _, err := manager.Acquire(context.Background(), key, policy); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Acquire(closed) error = %v", err)
	}
}

func TestAcquireRacingShutdownReleasesReservation(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := &serviceBackend{
		now: now, entered: make(chan struct{}), proceed: make(chan struct{}),
	}
	client, _ := lease.NewClient(backend, lease.ClientOptions{Clock: serviceClock{now}})
	manager, _ := New(client, 1)
	key, _ := lease.NewKey("service", "race")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	result := make(chan error, 1)
	go func() {
		_, err := manager.Acquire(context.Background(), key, policy)
		result <- err
	}()
	<-backend.entered
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	close(backend.proceed)
	if err := <-result; !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Acquire(racing shutdown) error = %v", err)
	}
	if manager.Active() != 0 {
		t.Fatalf("Active() = %d", manager.Active())
	}
}

func TestManagedStartAndShutdownFailuresAreReported(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := lease.NewKey("service", "managed")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	leaseClock := &serviceClock{now: now}
	expired := &serviceBackend{now: now, afterAcquire: func() {
		leaseClock.now = now.Add(2 * time.Second)
	}}
	client, _ := lease.NewClient(expired, lease.ClientOptions{
		Clock: leaseClock,
	})
	manager, _ := New(client, 1)
	if _, err := manager.Acquire(context.Background(), key, policy); !errors.Is(err, lease.ErrLost) {
		t.Fatalf("Acquire(expired managed) error = %v", err)
	}

	failing := &serviceBackend{now: now, releaseErr: lease.ErrAmbiguousOutcome}
	client, _ = lease.NewClient(failing, lease.ClientOptions{Clock: serviceClock{now}})
	manager, _ = New(client, 1)
	plain, _ := lease.NewPolicy(lease.PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	if _, err := manager.Acquire(context.Background(), key, plain); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := manager.Shutdown(context.Background()); !errors.Is(err, lease.ErrAmbiguousOutcome) {
		t.Fatalf("Shutdown(release failure) error = %v", err)
	}
}

func TestManagedAcquireAndShutdownStopRenewal(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := &serviceBackend{now: now}
	client, _ := lease.NewClient(backend, lease.ClientOptions{Clock: serviceClock{now}})
	manager, _ := New(client, 1)
	key, _ := lease.NewKey("service", "managed-success")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	if _, err := manager.Acquire(context.Background(), key, policy); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestManagedRenewalOutlivesAcquireContext(t *testing.T) {
	t.Parallel()

	now := time.Now()
	trigger := make(chan struct{})
	backend := &serviceBackend{now: now, renewed: make(chan struct{}, 1)}
	client, _ := lease.NewClient(backend, lease.ClientOptions{
		Clock: serviceClock{now}, Sleeper: serviceSleeper{trigger: trigger},
	})
	manager, _ := New(client, 1)
	key, _ := lease.NewKey("service", "lifecycle-context")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := manager.Acquire(ctx, key, policy); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	cancel()
	trigger <- struct{}{}
	select {
	case <-backend.renewed:
	case <-time.After(time.Second):
		t.Fatal("managed renewal stopped with acquisition context")
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}
