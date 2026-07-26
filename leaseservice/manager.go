// Package leaseservice integrates managed leases with service lifecycle hooks.
package leaseservice

import (
	"context"
	"errors"
	"sync"

	lease "github.com/faustbrian/golib/pkg/lease"
	serviceintegration "github.com/faustbrian/golib/pkg/service/integration"
)

type owned struct {
	handle  *lease.Handle
	managed *lease.Managed
}

// Manager bounds managed renewal goroutines and explicit shutdown release.
type Manager struct {
	mu      sync.Mutex
	client  *lease.Client
	max     uint32
	active  uint32
	closed  bool
	entries []owned
}

// New constructs a lifecycle manager with a hard handle bound.
func New(client *lease.Client, maxHandles uint32) (*Manager, error) {
	if client == nil || maxHandles == 0 {
		return nil, lease.Wrap(lease.ErrInvalidState, "service manager")
	}
	return &Manager{client: client, max: maxHandles}, nil
}

// Acquire reserves capacity, acquires ownership, and starts renewal when enabled.
func (manager *Manager) Acquire(
	ctx context.Context,
	key lease.Key,
	policy lease.Policy,
) (*lease.Handle, error) {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return nil, lease.Wrap(lease.ErrInvalidState, "service shutdown")
	}
	if manager.active >= manager.max {
		manager.mu.Unlock()
		return nil, lease.Wrap(lease.ErrBackendUnavailable, "service capacity")
	}
	manager.active++
	manager.mu.Unlock()

	handle, err := manager.client.Acquire(ctx, key, policy)
	if err != nil {
		manager.releaseReservation()
		return nil, err
	}
	var managed *lease.Managed
	if policy.RenewEvery() > 0 {
		managed, err = handle.StartManaged(context.WithoutCancel(ctx))
		if err != nil {
			manager.releaseReservation()
			return nil, errors.Join(err, handle.Release(context.WithoutCancel(ctx)))
		}
	}
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		if managed != nil {
			_ = managed.Stop(context.WithoutCancel(ctx))
		}
		manager.releaseReservation()
		return nil, errors.Join(
			lease.Wrap(lease.ErrInvalidState, "service shutdown"),
			handle.Release(context.WithoutCancel(ctx)),
		)
	}
	manager.entries = append(manager.entries, owned{handle: handle, managed: managed})
	manager.mu.Unlock()
	return handle, nil
}

// Active returns reserved and owned handle count.
func (manager *Manager) Active() uint32 {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.active
}

// Shutdown stops renewal and explicitly attempts compare-and-release for every
// handle. A canceled shutdown is returned as failure, never implied success.
func (manager *Manager) Shutdown(ctx context.Context) error {
	manager.mu.Lock()
	manager.closed = true
	entries := append([]owned(nil), manager.entries...)
	manager.entries = nil
	// #nosec G115 -- entries cannot exceed the uint32 max handle budget.
	manager.active -= uint32(len(entries))
	manager.mu.Unlock()

	var result error
	for _, entry := range entries {
		if entry.managed != nil {
			result = errors.Join(result, entry.managed.Stop(ctx))
		}
		result = errors.Join(result, entry.handle.Release(ctx))
	}
	return result
}

// Hooks adapts the manager to service's caller-owned lifecycle contract.
func (manager *Manager) Hooks() serviceintegration.Hooks {
	return serviceintegration.Hooks{
		Start: func(context.Context) error { return nil },
		Stop:  manager.Shutdown,
	}
}

func (manager *Manager) releaseReservation() {
	manager.mu.Lock()
	manager.active--
	manager.mu.Unlock()
}
