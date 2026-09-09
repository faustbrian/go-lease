package leaseservice

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/go-lease"
)

type serviceClock struct{ now time.Time }

func (clock serviceClock) Now() time.Time { return clock.now }

type serviceBackend struct {
	now               time.Time
	acquireErr        error
	releaseErr        error
	entered           chan struct{}
	proceed           chan struct{}
	expired           bool
	renewed           chan struct{}
	renewValues       chan any
	afterAcquire      func()
	releaseEntered    chan struct{}
	releaseProceed    chan struct{}
	stopped           <-chan struct{}
	releaseBeforeStop bool
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
	ctx context.Context,
	record lease.Record,
	ttl time.Duration,
) (lease.Record, error) {
	if backend.renewed != nil {
		backend.renewed <- struct{}{}
	}
	if backend.renewValues != nil {
		backend.renewValues <- ctx.Value(serviceRequestContextKey{})
	}
	record.ExpiresAt = backend.now.Add(ttl)
	return record, nil
}

type serviceRequestContextKey struct{}

func serviceTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestManagedRenewalDoesNotRetainAcquireRequestContext(t *testing.T) {
	t.Parallel()

	now := time.Now()
	trigger := make(chan struct{}, 1)
	values := make(chan any, 1)
	backend := &serviceBackend{now: now, renewValues: values}
	client, _ := lease.NewClient(backend, lease.ClientOptions{
		Clock: serviceClock{now: now}, Sleeper: serviceSleeper{trigger: trigger},
	})
	manager, _ := New(client, 1)
	key, _ := lease.NewKey("service", "request-context")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	requestContext := context.WithValue(context.Background(), serviceRequestContextKey{}, "request-value")
	if _, err := manager.Acquire(requestContext, key, policy); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	trigger <- struct{}{}
	select {
	case value := <-values:
		if value != nil {
			t.Fatalf("renewal retained request context value %v", value)
		}
	case <-time.After(time.Second):
		t.Fatal("managed renewal did not run")
	}
	if err := manager.Shutdown(serviceTestContext(t)); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
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

type observableServiceSleeper struct {
	trigger <-chan struct{}
	stopped chan<- struct{}
}

func (sleeper observableServiceSleeper) Sleep(ctx context.Context, _ time.Duration) error {
	select {
	case <-ctx.Done():
		close(sleeper.stopped)
		return ctx.Err()
	case <-sleeper.trigger:
		close(sleeper.stopped)
		return nil
	}
}
func (backend *serviceBackend) Validate(_ context.Context, record lease.Record) (lease.Record, error) {
	return record, nil
}

func (backend *serviceBackend) Release(context.Context, lease.Record) error {
	if backend.stopped != nil {
		select {
		case <-backend.stopped:
		default:
			backend.releaseBeforeStop = true
		}
	}
	if backend.releaseEntered != nil {
		close(backend.releaseEntered)
	}
	if backend.releaseProceed != nil {
		<-backend.releaseProceed
	}
	return backend.releaseErr
}

func TestShutdownStopsManagedRenewalBeforeRelease(t *testing.T) {
	t.Parallel()

	now := time.Now()
	trigger := make(chan struct{})
	stopped := make(chan struct{})
	backend := &serviceBackend{now: now, stopped: stopped}
	client, _ := lease.NewClient(backend, lease.ClientOptions{
		Clock:   serviceClock{now: now},
		Sleeper: observableServiceSleeper{trigger: trigger, stopped: stopped},
	})
	manager, _ := New(client, 1)
	key, _ := lease.NewKey("service", "ordered-shutdown")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	if _, err := manager.Acquire(context.Background(), key, policy); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := manager.Shutdown(serviceTestContext(t)); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if backend.releaseBeforeStop {
		t.Fatal("release ran before managed renewal stopped")
	}
}

func TestManagerRejectsInvalidContextsBeforeStateOrBackend(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := &serviceBackend{now: now}
	owners := &countingServiceOwners{}
	client, _ := lease.NewClient(backend, lease.ClientOptions{
		Clock: serviceClock{now: now}, Owners: owners,
	})
	manager, _ := New(client, 1)
	key, _ := lease.NewKey("service", "context")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{TTL: time.Second, MaxAttempts: 1})

	if _, err := manager.Acquire(nil, key, policy); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Acquire(nil) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Acquire(canceled, key, policy); !errors.Is(err, lease.ErrCanceled) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire(canceled) error = %v", err)
	}
	if manager.Active() != 0 || owners.calls != 0 {
		t.Fatalf("invalid context changed manager state: active=%d owners=%d", manager.Active(), owners.calls)
	}

	if err := manager.Shutdown(nil); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Shutdown(nil) error = %v", err)
	}
	if _, err := manager.Acquire(context.Background(), key, policy); err != nil {
		t.Fatalf("Acquire() after invalid shutdown error = %v", err)
	}
	if err := manager.Shutdown(serviceTestContext(t)); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

type countingServiceOwners struct{ calls int }

func (owners *countingServiceOwners) NewOwner() (string, error) {
	owners.calls++
	return "owner", nil
}

func TestShutdownUsesOneCallerIndependentCleanupAndCachesResult(t *testing.T) {
	t.Parallel()

	now := time.Now()
	releaseErr := errors.New("release failed")
	backend := &serviceBackend{now: now, releaseErr: releaseErr}
	client, _ := lease.NewClient(backend, lease.ClientOptions{Clock: serviceClock{now: now}})
	manager, _ := New(client, 1)
	key, _ := lease.NewKey("service", "shared-shutdown")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	if _, err := manager.Acquire(context.Background(), key, policy); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	backend.releaseEntered = make(chan struct{})
	backend.releaseProceed = make(chan struct{})

	short, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { first <- manager.Shutdown(short) }()
	select {
	case <-backend.releaseEntered:
	case <-time.After(time.Second):
		t.Fatal("shutdown cleanup did not reach release")
	}
	cancel()
	select {
	case err := <-first:
		if !errors.Is(err, lease.ErrCanceled) || !errors.Is(err, context.Canceled) {
			t.Fatalf("first Shutdown() error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		close(backend.releaseProceed)
		t.Fatal("canceled caller remained bound to shared cleanup")
	}
	if manager.Active() != 1 {
		t.Fatalf("Active() during cleanup = %d", manager.Active())
	}

	second := make(chan error, 1)
	go func() { second <- manager.Shutdown(serviceTestContext(t)) }()
	close(backend.releaseProceed)
	if err := <-second; !errors.Is(err, releaseErr) {
		t.Fatalf("second Shutdown() error = %v", err)
	}
	if err := manager.Shutdown(serviceTestContext(t)); !errors.Is(err, releaseErr) {
		t.Fatalf("repeated Shutdown() error = %v", err)
	}
	if manager.Active() != 0 {
		t.Fatalf("Active() after cleanup = %d", manager.Active())
	}
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
	if err := manager.Shutdown(serviceTestContext(t)); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if _, err := manager.Acquire(context.Background(), key, policy); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Acquire(closed) error = %v", err)
	}
}

func TestAcquireRacingShutdownReleasesReservation(t *testing.T) {
	t.Parallel()

	now := time.Now()
	trigger := make(chan struct{})
	stopped := make(chan struct{})
	backend := &serviceBackend{
		now: now, entered: make(chan struct{}), proceed: make(chan struct{}),
		releaseEntered: make(chan struct{}), releaseProceed: make(chan struct{}),
	}
	client, _ := lease.NewClient(backend, lease.ClientOptions{
		Clock: serviceClock{now},
		Sleeper: observableServiceSleeper{
			trigger: trigger,
			stopped: stopped,
		},
	})
	manager, _ := New(client, 1)
	key, _ := lease.NewKey("service", "race")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	acquireContext, cancelAcquire := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := manager.Acquire(acquireContext, key, policy)
		result <- err
	}()
	select {
	case <-backend.entered:
	case err := <-result:
		t.Fatalf("Acquire() returned before backend entry: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Shutdown(canceled); !errors.Is(err, lease.ErrCanceled) {
		t.Fatalf("first Shutdown() error = %v", err)
	}
	shutdown := make(chan error, 1)
	go func() { shutdown <- manager.Shutdown(serviceTestContext(t)) }()
	close(backend.proceed)
	select {
	case <-backend.releaseEntered:
	case <-time.After(time.Second):
		t.Fatal("shared shutdown cleanup did not release racing acquisition")
	}
	if manager.Active() != 1 {
		t.Fatalf("Active() during racing release = %d", manager.Active())
	}
	cancelAcquire()
	select {
	case err := <-result:
		if !errors.Is(err, lease.ErrInvalidState) || !errors.Is(err, lease.ErrCanceled) ||
			!errors.Is(err, context.Canceled) {
			t.Fatalf("Acquire(racing shutdown) error = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		close(backend.releaseProceed)
		t.Fatal("Acquire() remained bound to shared shutdown cleanup")
	}
	close(backend.releaseProceed)
	if err := <-shutdown; err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if manager.Active() != 0 {
		t.Fatalf("Active() = %d", manager.Active())
	}
	select {
	case <-stopped:
	default:
		close(trigger)
		t.Fatal("racing shutdown did not stop managed renewal")
	}
}

func TestAcquireRacingShutdownReceivesSharedTerminalResult(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := &serviceBackend{
		now: now, entered: make(chan struct{}), proceed: make(chan struct{}),
		releaseEntered: make(chan struct{}), releaseProceed: make(chan struct{}),
	}
	client, _ := lease.NewClient(backend, lease.ClientOptions{Clock: serviceClock{now: now}})
	manager, _ := New(client, 1)
	key, _ := lease.NewKey("service", "race-terminal")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	result := make(chan error, 1)
	go func() {
		_, err := manager.Acquire(context.Background(), key, policy)
		result <- err
	}()
	select {
	case <-backend.entered:
	case err := <-result:
		t.Fatalf("Acquire() returned before backend entry: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Shutdown(canceled); !errors.Is(err, lease.ErrCanceled) {
		t.Fatalf("first Shutdown() error = %v", err)
	}
	shutdown := make(chan error, 1)
	go func() { shutdown <- manager.Shutdown(serviceTestContext(t)) }()
	close(backend.proceed)
	select {
	case <-backend.releaseEntered:
	case <-time.After(time.Second):
		t.Fatal("shared shutdown cleanup did not release racing acquisition")
	}
	close(backend.releaseProceed)
	if err := <-shutdown; err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := <-result; !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Acquire(racing shutdown) error = %v", err)
	}
	if manager.Active() != 0 {
		t.Fatalf("Active() = %d", manager.Active())
	}
}

func TestManagedStartFailureRetainsAccountingUntilRollbackReleaseFinishes(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := &serviceBackend{
		now: now, releaseEntered: make(chan struct{}), releaseProceed: make(chan struct{}),
	}
	managedSleep := make(chan struct{})
	client, _ := lease.NewClient(backend, lease.ClientOptions{
		Clock: serviceClock{now: now}, Sleeper: serviceSleeper{trigger: managedSleep}, MaxManaged: 1,
	})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	prefillKey, _ := lease.NewKey("service", "managed-capacity")
	prefill, err := client.Acquire(context.Background(), prefillKey, policy)
	if err != nil {
		t.Fatalf("prefill Acquire() error = %v", err)
	}
	managed, err := prefill.StartManaged(context.Background())
	if err != nil {
		t.Fatalf("prefill StartManaged() error = %v", err)
	}
	t.Cleanup(func() { _ = managed.Stop(context.Background()) })

	manager, _ := New(client, 1)
	key, _ := lease.NewKey("service", "managed-failure")
	result := make(chan error, 1)
	go func() {
		_, acquireErr := manager.Acquire(context.Background(), key, policy)
		result <- acquireErr
	}()
	select {
	case <-backend.releaseEntered:
	case <-time.After(time.Second):
		t.Fatal("managed-start rollback did not reach release")
	}
	if manager.Active() != 1 {
		t.Fatalf("Active() during managed-start rollback = %d", manager.Active())
	}
	close(backend.releaseProceed)
	if err := <-result; !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("Acquire() error = %v", err)
	}
	if manager.Active() != 0 {
		t.Fatalf("Active() after managed-start rollback = %d", manager.Active())
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
	if manager.Active() != 1 {
		t.Fatalf("Active() after plain acquisition = %d", manager.Active())
	}
	if err := manager.Shutdown(serviceTestContext(t)); !errors.Is(err, lease.ErrAmbiguousOutcome) {
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
	if manager.Active() != 1 {
		t.Fatalf("Active() after managed acquisition = %d", manager.Active())
	}
	if err := manager.Shutdown(serviceTestContext(t)); err != nil {
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
	if manager.Active() != 1 {
		t.Fatalf("Active() after managed acquisition = %d", manager.Active())
	}
	cancel()
	trigger <- struct{}{}
	select {
	case <-backend.renewed:
	case <-time.After(time.Second):
		t.Fatal("managed renewal stopped with acquisition context")
	}
	if err := manager.Shutdown(serviceTestContext(t)); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}
