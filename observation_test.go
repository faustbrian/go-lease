package lease_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
	"github.com/faustbrian/golib/pkg/lease/memory"
)

func TestObservedBackendHashesIdentifiersAndContainsPanics(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	events := make(chan lease.Event, 1)
	observed, err := lease.NewObservedBackend(store, clock,
		lease.ObserverFunc(func(current lease.Event) { events <- current }),
		lease.ObserverFunc(func(lease.Event) { panic("observer") }),
	)
	if err != nil {
		t.Fatalf("NewObservedBackend() error = %v", err)
	}
	key, _ := lease.NewKey("queue", "secret-customer")
	if _, err := observed.TryAcquire(context.Background(), key, "secret-owner", time.Second); err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	event := <-events
	if event.Operation != lease.OperationAcquire || event.Outcome != lease.OutcomeSuccess {
		t.Fatalf("event = %+v", event)
	}
	if len(event.KeyHash) != 32 || strings.Contains(event.KeyHash, "secret") {
		t.Fatalf("KeyHash = %q", event.KeyHash)
	}
	if strings.Contains(event.String(), "secret-customer") || strings.Contains(event.String(), "secret-owner") {
		t.Fatalf("Event.String() leaked identifier: %s", event.String())
	}
}

func TestBlockingObserverCannotDelayLeaseTransition(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	entered := make(chan struct{})
	unblock := make(chan struct{})
	observed, _ := lease.NewObservedBackend(store, clock,
		lease.ObserverFunc(func(lease.Event) {
			close(entered)
			<-unblock
		}),
	)
	key, _ := lease.NewKey("observer", "blocked")
	type result struct {
		record lease.Record
		err    error
	}
	done := make(chan result, 1)
	go func() {
		record, err := observed.TryAcquire(context.Background(), key, "owner", time.Second)
		done <- result{record: record, err: err}
	}()
	select {
	case acquired := <-done:
		if acquired.err != nil {
			close(unblock)
			t.Fatalf("TryAcquire() error = %v", acquired.err)
		}
		<-entered
		if _, err := observed.Validate(context.Background(), acquired.record); err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
		close(unblock)
	case <-time.After(100 * time.Millisecond):
		close(unblock)
		<-done
		t.Fatal("blocking observer delayed lease transition")
	}
}

func TestObserverCanInspectHandleDuringStateTransition(t *testing.T) {
	t.Parallel()

	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	var handle *lease.Handle
	inspected := make(chan struct{})
	var inspectedOnce sync.Once
	observed, _ := lease.NewObservedBackend(store, clock,
		lease.ObserverFunc(func(event lease.Event) {
			if event.Operation == lease.OperationValidate {
				_ = handle.State()
				inspectedOnce.Do(func() { close(inspected) })
			}
		}),
	)
	client, _ := lease.NewClient(observed, lease.ClientOptions{
		Clock: clock, Owners: ownerSource{next: "owner"},
	})
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, MaxAttempts: 1,
	})
	key, _ := lease.NewKey("observer", "reentrant")
	handle, _ = client.TryAcquire(context.Background(), key, policy)
	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		if err := handle.Validate(context.Background()); err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
		select {
		case <-inspected:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("observer did not inspect handle")
		}
	}
}
