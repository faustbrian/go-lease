package lease

import (
	"context"
	"sync"
	"time"
)

// Loss describes why managed renewal stopped admitting ownership-dependent work.
type Loss struct {
	At    time.Time
	State State
	Err   error
}

// Managed owns one bounded renewal goroutine for a handle.
type Managed struct {
	handle *Handle
	cancel context.CancelFunc
	loss   chan Loss
	done   chan struct{}
	once   sync.Once
}

// Loss returns a channel that yields at most one terminal renewal failure.
func (managed *Managed) Loss() <-chan Loss { return managed.loss }

// Stop stops renewal and waits for the goroutine. It never implies release.
func (managed *Managed) Stop(ctx context.Context) error {
	managed.once.Do(managed.cancel)
	select {
	case <-managed.done:
		return nil
	case <-ctx.Done():
		return Wrap(ErrCanceled, "managed stop")
	}
}

func (managed *Managed) run(ctx context.Context) {
	defer func() {
		managed.handle.mu.Lock()
		managed.handle.managed = false
		<-managed.handle.managedSlots
		managed.handle.mu.Unlock()
		close(managed.loss)
		close(managed.done)
	}()
	for {
		if err := managed.handle.sleeper.Sleep(
			ctx, managed.handle.policy.RenewEvery(),
		); err != nil {
			return
		}
		if err := managed.handle.Renew(ctx); err != nil {
			managed.loss <- Loss{
				At: managed.handle.clock.Now(), State: managed.handle.State(), Err: err,
			}
			return
		}
	}
}
