package leasescheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leasescheduler"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
	"github.com/faustbrian/golib/pkg/lease/memory"
)

func TestOnOneServerProvidesFenceAndReleases(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, Retry: time.Millisecond, MaxAttempts: 1,
	})
	coordinator, err := leasescheduler.New(client, policy)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	key, _ := lease.NewKey("scheduler", "report")
	var token lease.Token
	err = coordinator.OnOneServer(context.Background(), key, func(_ context.Context, fence lease.Token) error {
		token = fence
		return nil
	})
	if err != nil || token != 1 {
		t.Fatalf("OnOneServer() token = %d, error = %v", token, err)
	}
}

func TestCoordinatorValidationAndOverlapAlias(t *testing.T) {
	t.Parallel()

	policy, _ := lease.NewPolicy(lease.PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	if _, err := leasescheduler.New(nil, policy); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("New(nil) error = %v", err)
	}
	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	coordinator, _ := leasescheduler.New(client, policy)
	key, _ := lease.NewKey("scheduler", "alias")
	if err := coordinator.OnOneServer(context.Background(), key, nil); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("OnOneServer(nil) error = %v", err)
	}
	if err := coordinator.WithoutOverlapping(context.Background(), key, func(context.Context, lease.Token) error { return nil }); err != nil {
		t.Fatalf("WithoutOverlapping() error = %v", err)
	}
}

func TestCoordinatorCancelsOwnershipSensitiveTaskOnLoss(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: 100 * time.Millisecond, RenewEvery: time.Millisecond,
		SafetyMargin: 10 * time.Millisecond, MaxAttempts: 1,
	})
	coordinator, _ := leasescheduler.New(client, policy)
	key, _ := lease.NewKey("scheduler", "loss")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := coordinator.OnOneServer(ctx, key, func(ctx context.Context, token lease.Token) error {
		if token == 0 {
			t.Fatal("scheduler task received a zero fence")
		}
		clock.Advance(time.Second)
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, lease.ErrLost) || !errors.Is(err, context.Canceled) {
		t.Fatalf("OnOneServer(renewal loss) error = %v", err)
	}
}
