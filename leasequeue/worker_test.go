package leasequeue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leasequeue"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
	"github.com/faustbrian/golib/pkg/lease/memory"
	"github.com/faustbrian/golib/pkg/queue/core"
)

type task struct{ body []byte }

func (task task) Bytes() []byte   { return task.body }
func (task task) Payload() []byte { return task.body }

type worker struct {
	token lease.Token
	run   func(context.Context) error
}

func (worker *worker) Run(ctx context.Context, _ core.TaskMessage) error {
	worker.token, _ = leasequeue.TokenFromContext(ctx)
	if worker.run != nil {
		return worker.run(ctx)
	}
	return nil
}
func (*worker) Shutdown() error                    { return nil }
func (*worker) Queue(core.TaskMessage) error       { return nil }
func (*worker) Request() (core.TaskMessage, error) { return task{}, nil }

func TestWorkerRunsJobWithFenceAndReleases(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 2})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, Retry: time.Millisecond, MaxAttempts: 1,
	})
	inner := &worker{}
	wrapped, err := leasequeue.NewWorker(inner, client, policy, func(core.TaskMessage) (lease.Key, error) {
		return lease.NewKey("queue", "job")
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if err := wrapped.Run(context.Background(), task{body: []byte("payload")}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if inner.token != 1 {
		t.Fatalf("worker fencing token = %d", inner.token)
	}
	if err := wrapped.Run(context.Background(), task{body: []byte("payload")}); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if inner.token != 2 {
		t.Fatalf("second fencing token = %d", inner.token)
	}
}

func TestWorkerValidationErrorsAndDelegation(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	inner := &worker{}
	keyFunc := func(core.TaskMessage) (lease.Key, error) { return lease.NewKey("queue", "job") }
	for _, values := range []struct {
		inner  core.Worker
		client *lease.Client
		key    leasequeue.KeyFunc
	}{{nil, client, keyFunc}, {inner, nil, keyFunc}, {inner, client, nil}} {
		if _, err := leasequeue.NewWorker(values.inner, values.client, policy, values.key); !errors.Is(err, lease.ErrInvalidState) {
			t.Fatalf("NewWorker(invalid) error = %v", err)
		}
	}
	keyErr := errors.New("key")
	wrapped, _ := leasequeue.NewWorker(inner, client, policy, func(core.TaskMessage) (lease.Key, error) {
		return lease.Key{}, keyErr
	})
	if err := wrapped.Run(context.Background(), task{}); !errors.Is(err, keyErr) {
		t.Fatalf("Run(key) error = %v", err)
	}
	if err := wrapped.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := wrapped.Queue(task{}); err != nil {
		t.Fatalf("Queue() error = %v", err)
	}
	if _, err := wrapped.Request(); err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if _, ok := leasequeue.TokenFromContext(context.Background()); ok {
		t.Fatal("TokenFromContext(empty) found token")
	}
}

func TestWorkerCancelsOwnershipSensitiveJobOnLoss(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: 100 * time.Millisecond, RenewEvery: time.Millisecond,
		SafetyMargin: 10 * time.Millisecond, MaxAttempts: 1,
	})
	inner := &worker{run: func(ctx context.Context) error {
		clock.Advance(time.Second)
		<-ctx.Done()
		return ctx.Err()
	}}
	wrapped, _ := leasequeue.NewWorker(inner, client, policy, func(core.TaskMessage) (lease.Key, error) {
		return lease.NewKey("queue", "loss")
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := wrapped.Run(ctx, task{}); !errors.Is(err, lease.ErrLost) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("Run(renewal loss) error = %v", err)
	}
	if inner.token == 0 {
		t.Fatal("worker did not receive a fencing token")
	}
}
