package lease

import (
	"context"
	"sync"
	"time"
)

// Loss describes why managed renewal stopped admitting ownership-dependent work.
type Loss struct {
	// At is injected local clock time when renewal failure was observed.
	At time.Time
	// State is the handle's fail-closed local state at observation, not remote proof.
	State State
	// Err retains the renewal error and its classification/cause semantics.
	Err error
}

// Managed owns one renewal goroutine and a shared client capacity slot. Obtain
// it from StartManaged; its zero value is unusable. Cancellation/Stop stops only
// renewal, never implicitly releases ownership or forcibly interrupts callbacks.
type Managed struct {
	handle *Handle
	cancel context.CancelFunc
	loss   chan Loss
	done   chan struct{}
	once   sync.Once
}

// Loss returns the package-owned buffered channel for at most one renewal-call
// failure, then closes. Context cancellation or a Sleeper error closes it without
// a value; closure alone is not proof of successful renewal or remote release.
// Multiple readers share one result, not broadcasts. Callers must not close it.
func (managed *Managed) Loss() <-chan Loss { return managed.loss }

// Stop cancels renewal once and joins within the supplied nonnil context.
// Repeated/concurrent callers may wait independently; ErrCanceled ends only that
// caller's wait and cleanup continues. Slot release and Loss closure happen when
// the goroutine exits. Uncooperative sleeper/backend calls can delay completion;
// nil success never implies remote Release.
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
