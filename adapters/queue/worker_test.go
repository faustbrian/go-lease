package leasequeue

import (
	"context"
	"testing"
	"time"

	lease "github.com/faustbrian/go-lease"
	"github.com/faustbrian/go-lease/leasetest"
	"github.com/faustbrian/go-lease/memory"
	"github.com/faustbrian/go-queue/core"
)

type testMessage struct{}

func (testMessage) Bytes() []byte   { return nil }
func (testMessage) Payload() []byte { return nil }

type testWorker struct{ token lease.Token }

func (worker *testWorker) Run(ctx context.Context, _ core.TaskMessage) error {
	worker.token, _ = TokenFromContext(ctx)
	return nil
}

func (*testWorker) Shutdown() error                    { return nil }
func (*testWorker) Queue(core.TaskMessage) error       { return nil }
func (*testWorker) Request() (core.TaskMessage, error) { return testMessage{}, nil }

func TestWorkerExposesFenceThroughCanonicalAdapter(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	client, _ := lease.NewClient(store, lease.ClientOptions{Clock: clock})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	inner := &testWorker{}
	worker, err := NewWorker(inner, client, policy, func(core.TaskMessage) (lease.Key, error) {
		return lease.NewKey("queue", "canonical")
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if err := worker.Run(context.Background(), testMessage{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if inner.token == 0 {
		t.Fatal("canonical adapter did not expose a fencing token")
	}
}
