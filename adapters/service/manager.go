// Package leaseservice integrates managed leases with service lifecycle hooks.
package leaseservice

import (
	"context"
	"errors"
	"sync"

	lease "github.com/faustbrian/go-lease"
	serviceintegration "github.com/faustbrian/go-service/integration"
)

type owned struct {
	handle  *lease.Handle
	managed *lease.Managed
}

// Manager bounds managed renewal goroutines and explicit shutdown release.
type Manager struct {
	mu               sync.Mutex
	client           *lease.Client
	max              uint32
	active           uint32
	acquiring        uint32
	closed           bool
	entries          []owned
	acquisitionsDone chan struct{}
	shutdownStarted  bool
	shutdownDone     chan struct{}
	shutdownErr      error
}

// New constructs a lifecycle manager with a hard handle bound.
func New(client *lease.Client, maxHandles uint32) (*Manager, error) {
	if client == nil || maxHandles == 0 {
		return nil, lease.Wrap(lease.ErrInvalidState, "service manager")
	}
	acquisitionsDone := make(chan struct{})
	close(acquisitionsDone)
	manager := &Manager{client: client, max: maxHandles, acquisitionsDone: acquisitionsDone}
	return manager, nil
}

// Acquire reserves capacity, acquires ownership, and starts renewal when enabled.
func (manager *Manager) Acquire(
	ctx context.Context,
	key lease.Key,
	policy lease.Policy,
) (*lease.Handle, error) {
	if err := operationContextError(ctx, "service acquire"); err != nil {
		return nil, err
	}
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
	if manager.acquiring == 0 {
		manager.acquisitionsDone = make(chan struct{})
	}
	manager.acquiring++
	manager.mu.Unlock()

	handle, err := manager.client.Acquire(ctx, key, policy)
	if err != nil {
		manager.releaseReservation()
		return nil, err
	}
	var managed *lease.Managed
	if policy.RenewEvery() > 0 {
		managed, err = handle.StartManaged(context.Background())
		if err != nil {
			releaseErr := handle.Release(context.WithoutCancel(ctx))
			manager.releaseReservation()
			return nil, errors.Join(err, releaseErr)
		}
	}
	manager.mu.Lock()
	if manager.closed {
		manager.entries = append(manager.entries, owned{handle: handle, managed: managed})
		manager.finishAcquisitionLocked()
		done := manager.shutdownDone
		manager.mu.Unlock()
		select {
		case <-done:
			manager.mu.Lock()
			shutdownErr := manager.shutdownErr
			manager.mu.Unlock()
			return nil, errors.Join(lease.Wrap(lease.ErrInvalidState, "service shutdown"), shutdownErr)
		case <-ctx.Done():
			return nil, errors.Join(
				lease.Wrap(lease.ErrInvalidState, "service shutdown"),
				lease.Wrap(lease.ErrCanceled, "service acquire"),
				ctx.Err(),
			)
		}
	}
	manager.entries = append(manager.entries, owned{handle: handle, managed: managed})
	manager.finishAcquisitionLocked()
	manager.mu.Unlock()
	return handle, nil
}

// Active returns reserved and owned handle count.
func (manager *Manager) Active() uint32 {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.active
}

// Shutdown starts one caller-independent ordered cleanup and waits for its
// cached terminal result within the caller's context.
func (manager *Manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return lease.Wrap(lease.ErrInvalidState, "service shutdown context")
	}

	manager.mu.Lock()
	if !manager.shutdownStarted {
		manager.closed = true
		manager.shutdownStarted = true
		manager.shutdownDone = make(chan struct{})
		go manager.cleanup()
	}
	done := manager.shutdownDone
	manager.mu.Unlock()

	select {
	case <-done:
		manager.mu.Lock()
		defer manager.mu.Unlock()
		return manager.shutdownErr
	case <-ctx.Done():
		return errors.Join(lease.Wrap(lease.ErrCanceled, "service shutdown"), ctx.Err())
	}
}

func (manager *Manager) cleanup() {
	manager.mu.Lock()
	acquisitionsDone := manager.acquisitionsDone
	manager.mu.Unlock()
	<-acquisitionsDone

	manager.mu.Lock()
	entries := append([]owned(nil), manager.entries...)
	manager.mu.Unlock()

	var result error
	for _, entry := range entries {
		if entry.managed != nil {
			result = errors.Join(result, entry.managed.Stop(context.Background()))
		}
		result = errors.Join(result, entry.handle.Release(context.Background()))
	}

	manager.mu.Lock()
	manager.shutdownErr = result
	manager.entries = nil
	// #nosec G115 -- entries cannot exceed the uint32 max handle budget.
	manager.active -= uint32(len(entries))
	close(manager.shutdownDone)
	manager.mu.Unlock()
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
	manager.finishAcquisitionLocked()
	manager.mu.Unlock()
}

func (manager *Manager) finishAcquisitionLocked() {
	manager.acquiring--
	if manager.acquiring == 0 {
		close(manager.acquisitionsDone)
	}
}

func operationContextError(ctx context.Context, operation string) error {
	if ctx == nil {
		return lease.Wrap(lease.ErrInvalidState, operation+" context")
	}
	if ctx.Err() != nil {
		return errors.Join(lease.Wrap(lease.ErrCanceled, operation), ctx.Err())
	}
	return nil
}
