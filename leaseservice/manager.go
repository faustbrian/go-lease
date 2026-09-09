// Package leaseservice is the legacy facade for the canonical service adapter.
//
// Deprecated: use github.com/faustbrian/go-lease/adapters/service.
package leaseservice

import (
	"context"

	lease "github.com/faustbrian/go-lease"
	adapter "github.com/faustbrian/go-lease/adapters/service"
	serviceintegration "github.com/faustbrian/go-service/integration"
)

// Manager preserves the released service lifecycle type while delegating
// behavior to the canonical adapter.
//
// Deprecated: use service.Manager.
type Manager struct{ inner *adapter.Manager }

// New delegates to the canonical service adapter.
//
// Deprecated: use service.New.
func New(client *lease.Client, maxHandles uint32) (*Manager, error) {
	manager, err := adapter.New(client, maxHandles)
	if err != nil {
		return nil, err
	}
	return &Manager{inner: manager}, nil
}

// Acquire delegates to the canonical service adapter.
func (manager *Manager) Acquire(ctx context.Context, key lease.Key, policy lease.Policy) (*lease.Handle, error) {
	return manager.inner.Acquire(ctx, key, policy)
}

// Active returns reserved and owned handle count.
func (manager *Manager) Active() uint32 { return manager.inner.Active() }

// Shutdown delegates to the canonical service adapter.
func (manager *Manager) Shutdown(ctx context.Context) error { return manager.inner.Shutdown(ctx) }

// Hooks delegates to the canonical service adapter.
func (manager *Manager) Hooks() serviceintegration.Hooks { return manager.inner.Hooks() }
