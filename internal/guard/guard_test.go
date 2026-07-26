package guard

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
)

type clock struct{ now time.Time }

func (clock clock) Now() time.Time { return clock.now }

type owners struct{}

func (owners) NewOwner() (string, error) { return "owner", nil }

type immediateSleeper struct{}

func (immediateSleeper) Sleep(context.Context, time.Duration) error { return nil }

type backend struct {
	now          time.Time
	acquireErr   error
	renewErr     error
	releaseErr   error
	expired      bool
	afterAcquire func()
}

func (backend backend) TryAcquire(
	_ context.Context, key lease.Key, owner string, ttl time.Duration,
) (lease.Record, error) {
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
func (backend backend) Renew(_ context.Context, record lease.Record, ttl time.Duration) (lease.Record, error) {
	if backend.renewErr != nil {
		return lease.Record{}, backend.renewErr
	}
	record.ExpiresAt = backend.now.Add(ttl)
	return record, nil
}
func (backend backend) Validate(_ context.Context, record lease.Record) (lease.Record, error) {
	return record, nil
}
func (backend backend) Release(context.Context, lease.Record) error { return backend.releaseErr }

func TestRunPropagatesAcquireCallbackAndReleaseFailures(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := lease.NewKey("guard", "errors")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	failing := backend{now: now, acquireErr: lease.ErrBackendUnavailable}
	client, _ := lease.NewClient(failing, lease.ClientOptions{Clock: clock{now}, Owners: owners{}})
	if err := Run(context.Background(), client, policy, key, func(context.Context, lease.Token) error {
		return nil
	}); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("Run(acquire) error = %v", err)
	}
	callbackErr := errors.New("callback")
	failing = backend{now: now, releaseErr: lease.ErrAmbiguousOutcome}
	client, _ = lease.NewClient(failing, lease.ClientOptions{Clock: clock{now}, Owners: owners{}})
	err := Run(context.Background(), client, policy, key, func(context.Context, lease.Token) error {
		return callbackErr
	})
	if !errors.Is(err, callbackErr) || !errors.Is(err, lease.ErrAmbiguousOutcome) {
		t.Fatalf("Run(callback/release) error = %v", err)
	}
}

func TestRunFailsClosedOnManagedStartAndRenewalLoss(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := lease.NewKey("guard", "managed")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond, MaxAttempts: 1,
	})
	leaseClock := &clock{now: now}
	expired := backend{now: now, afterAcquire: func() {
		leaseClock.now = now.Add(2 * time.Second)
	}}
	client, _ := lease.NewClient(expired, lease.ClientOptions{
		Clock: leaseClock, Owners: owners{},
	})
	if err := Run(context.Background(), client, policy, key, func(context.Context, lease.Token) error {
		return nil
	}); !errors.Is(err, lease.ErrLost) {
		t.Fatalf("Run(expired) error = %v", err)
	}

	stale := backend{now: now, renewErr: lease.ErrStaleOwner}
	client, _ = lease.NewClient(stale, lease.ClientOptions{
		Clock: clock{now}, Owners: owners{}, Sleeper: immediateSleeper{},
	})
	err := Run(context.Background(), client, policy, key, func(ctx context.Context, _ lease.Token) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, lease.ErrLost) || !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Run(renewal loss) error = %v", err)
	}
}
