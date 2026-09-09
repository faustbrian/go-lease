package leasescheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/go-lease"
	"github.com/faustbrian/go-lease/leasetest"
	"github.com/faustbrian/go-lease/memory"
)

func TestCoordinatorRunsThroughCanonicalAdapter(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	coordinator, err := New(client, policy)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	key, _ := lease.NewKey("scheduler", "canonical")
	var token lease.Token
	if err := coordinator.OnOneServer(context.Background(), key, func(_ context.Context, fence lease.Token) error {
		token = fence
		return nil
	}); err != nil {
		t.Fatalf("OnOneServer() error = %v", err)
	}
	if token == 0 {
		t.Fatal("canonical adapter did not provide a fencing token")
	}
}

func TestCoordinatorValidationAndWithoutOverlapping(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, lease.Policy{}); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("New(nil) error = %v", err)
	}
	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	coordinator, _ := New(client, policy)
	key, _ := lease.NewKey("scheduler", "validation")
	if err := coordinator.OnOneServer(context.Background(), key, nil); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("OnOneServer(nil) error = %v", err)
	}
	called := false
	if err := coordinator.WithoutOverlapping(context.Background(), key, func(context.Context, lease.Token) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("WithoutOverlapping() error = %v", err)
	}
	if !called {
		t.Fatal("WithoutOverlapping() did not run task")
	}
}
