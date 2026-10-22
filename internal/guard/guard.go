// Package guard joins lease acquisition, optional renewal and explicit release
// for queue/scheduler callbacks. Run borrows client and callback, owns its child
// context/renewal watcher, and cannot force callback termination or fence effects.
// It introduces no service beyond the chosen Backend; resource checks remain
// caller-owned, as described in docs/fencing.md.
package guard

import (
	"context"
	"errors"

	lease "github.com/faustbrian/go-lease/v3"
)

// Run acquires key, invokes callback synchronously with the token, and joins
// callback/renewal/release errors. Enabled renewal cancels the child context on
// a reported renewal failure; without renewal there is no automatic TTL watcher.
// Stop and release use cancellation-detached contexts, retaining handle operation
// bounds. Cooperative callback/sleeper/backend termination is required. Panics
// are not contained and can skip explicit release; resource-side fencing and
// effect idempotency remain mandatory for work that outlives ownership.
func Run(
	ctx context.Context,
	client *lease.Client,
	policy lease.Policy,
	key lease.Key,
	callback func(context.Context, lease.Token) error,
) error {
	handle, err := client.Acquire(ctx, key, policy)
	if err != nil {
		return err
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	var managed *lease.Managed
	losses := make(chan error, 1)
	if policy.RenewEvery() > 0 {
		managed, err = handle.StartManaged(runContext)
		if err != nil {
			return errors.Join(err, release(ctx, handle, policy))
		}
		go func() {
			loss, ok := <-managed.Loss()
			if ok {
				losses <- errors.Join(lease.ErrLost, loss.Err)
				cancel()
			}
			close(losses)
		}()
	}

	// Cancellation signals loss but cannot stop an arbitrary callback or undo an
	// effect already in flight. Join the renewal watcher before returning errors.
	callbackErr := callback(runContext, handle.Token())
	cancel()
	if managed != nil {
		callbackErr = errors.Join(callbackErr, managed.Stop(context.WithoutCancel(ctx)))
		if lossErr, ok := <-losses; ok {
			callbackErr = errors.Join(callbackErr, lossErr)
		}
	}
	return errors.Join(callbackErr, release(ctx, handle, policy))
}

func release(ctx context.Context, handle *lease.Handle, policy lease.Policy) error {
	releaseContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), policy.TTL())
	defer cancel()
	return handle.Release(releaseContext)
}
