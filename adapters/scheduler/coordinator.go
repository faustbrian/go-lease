// Package leasescheduler admits caller-owned task callbacks under a per-key
// lease. New creates a coordinator; OnOneServer/WithoutOverlapping use the same
// lease protocol, not a scheduler, fairness promise or occurrence-deduplication
// store. Optional renewal signals loss through context cancellation; resource
// fencing, callback cooperation and effect idempotency remain caller-owned.
// There is no infrastructure/cgo requirement beyond the chosen Backend.
package leasescheduler

import (
	"context"

	lease "github.com/faustbrian/go-lease/v2"
	"github.com/faustbrian/go-lease/v2/internal/guard"
)

// Task synchronously performs an occurrence with a fence and child context.
// It must cooperate with cancellation and apply the fence atomically at effects;
// callback errors are joined unchanged, panics are not contained. Shared tasks
// must support concurrent invocations when the caller schedules different keys.
type Task func(context.Context, lease.Token) error

// Coordinator borrows a Client and snapshots Policy. It owns no schedule or
// background loop; concurrent calls depend on Client/Task contracts. Its zero
// value is unusable, and each call owns only its acquired/optional renewal work.
type Coordinator struct {
	client *lease.Client
	policy lease.Policy
}

// New rejects nil client and borrows it without revalidating Policy. Construct
// policy with NewPolicy; this function does not connect, start work or own a closer.
func New(client *lease.Client, policy lease.Policy) (*Coordinator, error) {
	if client == nil {
		return nil, lease.Wrap(lease.ErrInvalidState, "scheduler coordinator")
	}
	return &Coordinator{client: client, policy: policy}, nil
}

// OnOneServer rejects nil task, acquires key and invokes task synchronously,
// then stops renewal/releases and joins errors. Enabled renewal cancels on loss;
// otherwise TTL does not automatically cancel the task. Callbacks can outlive
// ownership, so fences and cooperative termination are still required.
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
