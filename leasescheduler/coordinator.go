// Package leasescheduler provides on-one-server and non-overlap execution.
package leasescheduler

import (
	"context"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/internal/guard"
)

// Task performs one fenced scheduled occurrence.
type Task func(context.Context, lease.Token) error

// Coordinator applies one immutable policy to scheduled ownership.
type Coordinator struct {
	client *lease.Client
	policy lease.Policy
}

// New constructs a scheduler lease coordinator.
func New(client *lease.Client, policy lease.Policy) (*Coordinator, error) {
	if client == nil {
		return nil, lease.Wrap(lease.ErrInvalidState, "scheduler coordinator")
	}
	return &Coordinator{client: client, policy: policy}, nil
}

// OnOneServer runs one occurrence under distributed fenced ownership.
func (coordinator *Coordinator) OnOneServer(
	ctx context.Context,
	key lease.Key,
	task Task,
) error {
	if task == nil {
		return lease.Wrap(lease.ErrInvalidState, "scheduler task")
	}
	return guard.Run(ctx, coordinator.client, coordinator.policy, key, task)
}

// WithoutOverlapping is an explicit semantic alias for OnOneServer.
func (coordinator *Coordinator) WithoutOverlapping(
	ctx context.Context,
	key lease.Key,
	task Task,
) error {
	return coordinator.OnOneServer(ctx, key, task)
}
