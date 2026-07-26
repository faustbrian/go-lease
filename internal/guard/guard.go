// Package guard runs callbacks under fail-closed managed lease ownership.
package guard

import (
	"context"
	"errors"

	lease "github.com/faustbrian/golib/pkg/lease"
)

// Run acquires, optionally renews, cancels on loss, and explicitly releases.
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
