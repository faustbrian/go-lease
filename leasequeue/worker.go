// Package leasequeue is the legacy facade for the canonical queue adapter.
//
// Deprecated: use github.com/faustbrian/go-lease/adapters/queue.
package leasequeue

import (
	"context"

	lease "github.com/faustbrian/go-lease"
	adapter "github.com/faustbrian/go-lease/adapters/queue"
	"github.com/faustbrian/go-queue/core"
)

// KeyFunc derives a bounded lease key from a delivered queue message.
//
// Deprecated: use queue.KeyFunc.
type KeyFunc func(core.TaskMessage) (lease.Key, error)

// Worker preserves the released queue worker type while delegating behavior to
// the canonical adapter.
//
// Deprecated: use queue.Worker.
type Worker struct{ inner *adapter.Worker }

// NewWorker delegates to the canonical queue adapter.
//
// Deprecated: use queue.NewWorker.
func NewWorker(
	inner core.Worker,
	client *lease.Client,
	policy lease.Policy,
	key KeyFunc,
) (*Worker, error) {
	var successorKey adapter.KeyFunc
	if key != nil {
		successorKey = func(task core.TaskMessage) (lease.Key, error) { return key(task) }
	}
	worker, err := adapter.NewWorker(inner, client, policy, successorKey)
	if err != nil {
		return nil, err
	}
	return &Worker{inner: worker}, nil
}

// Run delegates to the canonical queue adapter.
func (worker *Worker) Run(ctx context.Context, task core.TaskMessage) error {
	return worker.inner.Run(ctx, task)
}

// Shutdown delegates to the caller-owned worker.
func (worker *Worker) Shutdown() error { return worker.inner.Shutdown() }

// Queue delegates to the caller-owned worker.
func (worker *Worker) Queue(task core.TaskMessage) error { return worker.inner.Queue(task) }

// Request delegates to the caller-owned worker.
func (worker *Worker) Request() (core.TaskMessage, error) { return worker.inner.Request() }

// TokenFromContext returns the fencing token installed by the queue adapter.
//
// Deprecated: use queue.TokenFromContext.
func TokenFromContext(ctx context.Context) (lease.Token, bool) {
	return adapter.TokenFromContext(ctx)
}
