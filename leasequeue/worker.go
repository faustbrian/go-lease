// Package leasequeue integrates fenced leases with queue workers.
package leasequeue

import (
	"context"
	"fmt"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/internal/guard"
	"github.com/faustbrian/golib/pkg/queue/core"
)

type tokenContextKey struct{}

// KeyFunc derives a bounded lease key from a delivered queue message.
type KeyFunc func(core.TaskMessage) (lease.Key, error)

// Worker adds unique-job and non-overlap admission to a queue worker.
type Worker struct {
	inner  core.Worker
	client *lease.Client
	policy lease.Policy
	key    KeyFunc
}

// NewWorker wraps a caller-owned worker with fenced lease admission.
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

// Run acquires the message lease and exposes its fence through context.
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

// TokenFromContext returns the fencing token for protected-resource writes.
func TokenFromContext(ctx context.Context) (lease.Token, bool) {
	token, ok := ctx.Value(tokenContextKey{}).(lease.Token)
	return token, ok
}
