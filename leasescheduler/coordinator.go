// Package leasescheduler is the legacy facade for the canonical scheduler adapter.
//
// Deprecated: use github.com/faustbrian/go-lease/adapters/scheduler.
package leasescheduler

import (
	"context"

	lease "github.com/faustbrian/go-lease"
	adapter "github.com/faustbrian/go-lease/adapters/scheduler"
)

// Task performs one fenced scheduled occurrence.
//
// Deprecated: use scheduler.Task.
type Task func(context.Context, lease.Token) error

// Coordinator preserves the released scheduler type while delegating behavior
// to the canonical adapter.
//
// Deprecated: use scheduler.Coordinator.
type Coordinator struct{ inner *adapter.Coordinator }

// New delegates to the canonical scheduler adapter.
//
// Deprecated: use scheduler.New.
func New(client *lease.Client, policy lease.Policy) (*Coordinator, error) {
	coordinator, err := adapter.New(client, policy)
	if err != nil {
		return nil, err
	}
	return &Coordinator{inner: coordinator}, nil
}

// OnOneServer delegates to the canonical scheduler adapter.
func (coordinator *Coordinator) OnOneServer(ctx context.Context, key lease.Key, task Task) error {
	var successorTask adapter.Task
	if task != nil {
		successorTask = func(ctx context.Context, token lease.Token) error { return task(ctx, token) }
	}
	return coordinator.inner.OnOneServer(ctx, key, successorTask)
}

// WithoutOverlapping delegates to OnOneServer.
func (coordinator *Coordinator) WithoutOverlapping(ctx context.Context, key lease.Key, task Task) error {
	return coordinator.OnOneServer(ctx, key, task)
}
