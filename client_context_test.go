package lease_test

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/go-lease"
)

type countingOwnerSource struct{ calls int }

func (source *countingOwnerSource) NewOwner() (string, error) {
	source.calls++
	return "owner", nil
}

type countingBackend struct{ acquireCalls int }

func (backend *countingBackend) TryAcquire(
	context.Context,
	lease.Key,
	string,
	time.Duration,
) (lease.Record, error) {
	backend.acquireCalls++
	return lease.Record{}, errors.New("unexpected backend call")
}

func (*countingBackend) Renew(context.Context, lease.Record, time.Duration) (lease.Record, error) {
	return lease.Record{}, nil
}

func (*countingBackend) Validate(context.Context, lease.Record) (lease.Record, error) {
	return lease.Record{}, nil
}

func (*countingBackend) Release(context.Context, lease.Record) error { return nil }

func TestClientRejectsInvalidContextsBeforeSourcesAndBackend(t *testing.T) {
	t.Parallel()

	key, err := lease.NewKey("context", "boundary")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	policy, err := lease.NewPolicy(lease.PolicyOptions{
		TTL: time.Second, MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	for _, test := range []struct {
		name  string
		ctx   context.Context
		want  error
		cause error
	}{
		{name: "nil", ctx: nil, want: lease.ErrInvalidState},
		{name: "canceled", ctx: canceledContext(), want: lease.ErrCanceled, cause: context.Canceled},
		{name: "deadline", ctx: expiredContext(), want: lease.ErrCanceled, cause: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, call := range []struct {
				name string
				fn   func(*lease.Client) error
			}{
				{name: "try", fn: func(client *lease.Client) error {
					_, callErr := client.TryAcquire(test.ctx, key, policy)
					return callErr
				}},
				{name: "wait", fn: func(client *lease.Client) error {
					_, callErr := client.Acquire(test.ctx, key, policy)
					return callErr
				}},
			} {
				t.Run(call.name, func(t *testing.T) {
					backend := &countingBackend{}
					owners := &countingOwnerSource{}
					client, clientErr := lease.NewClient(backend, lease.ClientOptions{Owners: owners})
					if clientErr != nil {
						t.Fatalf("NewClient() error = %v", clientErr)
					}
					if callErr := call.fn(client); !errors.Is(callErr, test.want) {
						t.Fatalf("context error = %v, want %v", callErr, test.want)
					} else if test.cause != nil && !errors.Is(callErr, test.cause) {
						t.Fatalf("context error = %v, want cause %v", callErr, test.cause)
					}
					if owners.calls != 0 || backend.acquireCalls != 0 {
						t.Fatalf("invalid context touched owners/backend: owners=%d backend=%d", owners.calls, backend.acquireCalls)
					}
				})
			}
		})
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func expiredContext() context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	cancel()
	return ctx
}
