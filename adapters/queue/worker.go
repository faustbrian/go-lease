// Package leasequeue adds lease admission to a borrowed queue worker. NewWorker
// wraps Run, exposes TokenFromContext and explicitly releases after callback
// completion. Queue/Request/Shutdown remain delegated, not lease-owned lifetimes.
// Optional renewal signals loss through cancellation but cannot stop arbitrary
// work; durable resource fencing/idempotency stay application-owned. It requires
// only the selected Backend, not a new queue service or cgo integration.
package leasequeue

import (
	"context"
	"fmt"

	lease "github.com/faustbrian/go-lease/v2"
	"github.com/faustbrian/go-lease/v2/internal/guard"
	"github.com/faustbrian/go-queue/core"
)

type tokenContextKey struct{}

// KeyFunc derives a validated lease key from borrowed task data. Calls are
// synchronous before acquisition, without panic containment or a context;
// concurrent Run calls require a safe, prompt function. Errors are wrapped
// unchanged and can expose caller diagnostic text.
type KeyFunc func(core.TaskMessage) (lease.Key, error)

// Worker adds per-key lease admission around Run, not queue deduplication or
// durable exactly-once effects. Dependencies are borrowed and concurrency follows
// their contracts. Its zero value is unusable; construction adds no goroutine.
type Worker struct {
	inner  core.Worker
	client *lease.Client
	policy lease.Policy
	key    KeyFunc
}

// NewWorker borrows nonnil worker/client/key function and snapshots policy.
// It rejects nil interfaces/functions but not typed-nil implementations, and
// expects Policy from NewPolicy rather than revalidating it here.
func NewWorker(
	inner core.Worker,
	client *lease.Client,
	policy lease.Policy,
	key KeyFunc,
) (*Worker, error) {
	if inner == nil || client == nil || key == nil {
		return nil, lease.Wrap(lease.ErrInvalidState, "queue worker")
	}
	return &Worker{inner: inner, client: client, policy: policy, key: key}, nil
}

// Run calls KeyFunc, acquires ownership and invokes the inner worker with a
// child context carrying its fence. Optional renewal cancels on reported loss;
// disabled renewal does not create an expiry watcher. It joins callback/cleanup
// errors after return, but cannot undo effects or force callback termination.
// Task data is borrowed unchanged; panics can skip explicit release.
func (worker *Worker) Run(ctx context.Context, task core.TaskMessage) error {
	key, err := worker.key(task)
	if err != nil {
		return fmt.Errorf("queue lease key: %w", err)
	}
	return guard.Run(ctx, worker.client, worker.policy, key, func(
		callbackContext context.Context,
		token lease.Token,
	) error {
		return worker.inner.Run(
			context.WithValue(callbackContext, tokenContextKey{}, token), task,
		)
	})
}

// Shutdown delegates to the caller-owned worker.
func (worker *Worker) Shutdown() error { return worker.inner.Shutdown() }

// Queue delegates to the caller-owned worker.
func (worker *Worker) Queue(task core.TaskMessage) error { return worker.inner.Queue(task) }

// Request delegates to the caller-owned worker.
func (worker *Worker) Request() (core.TaskMessage, error) { return worker.inner.Request() }

// TokenFromContext reads the token installed during Run from a nonnil context.
// False means absent/wrong typed value. Presence is not current ownership or
// authorization: the receiving resource must atomically enforce fencing.
func TokenFromContext(ctx context.Context) (lease.Token, bool) {
	token, ok := ctx.Value(tokenContextKey{}).(lease.Token)
	return token, ok
}
