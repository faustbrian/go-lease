package lease

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	// MaxClientWaiters is the largest configurable concurrent acquisition bound.
	MaxClientWaiters uint32 = 1_000_000
	// MaxClientManaged is the largest configurable managed-renewal bound.
	MaxClientManaged uint32 = 100_000
)

// OwnerSource creates opaque ownership-comparison identities. Shared sources
// must be concurrent-safe and prompt; this synchronous call has no context or
// panic containment. Default production identities use 24 cryptographic bytes;
// deterministic test sources do not provide authentication or unpredictability.
type OwnerSource interface {
	// NewOwner returns a nonempty identity of at most 128 bytes or an error.
	// Client hides source errors as ErrBackendUnavailable and reuses one owner
	// across the contention retries of one Acquire call.
	NewOwner() (string, error)
}

// Sleeper performs acquisition and managed-renewal delays. Shared implementations
// must be concurrent-safe, honor cancellation and return promptly; callbacks
// that block despite their context cannot be forcibly interrupted.
type Sleeper interface {
	// Sleep waits for duration or cancellation. Any nonnil error stops the delay;
	// managed renewal closes its stream without producing Loss on a sleep error.
	Sleep(context.Context, time.Duration) error
}

// RetrySource supplies acquisition jitter. Shared sources must be concurrent-
// safe and prompt; calls are synchronous without cancellation or panic handling.
type RetrySource interface {
	// Jitter returns an added delay, clamped by Client to [0, maximum]. The
	// default cryptographic source returns zero if its random read fails.
	Jitter(time.Duration) time.Duration
}

// ClientOptions borrows sources and selects shared client capacity bounds.
// Nil interfaces select defaults; nonnil typed-nil implementations are not tested.
type ClientOptions struct {
	// Clock selects local admission time; nil uses time.Now with a real-time backstop.
	Clock Clock
	// Owners supplies comparison identities; nil uses crypto/rand.
	Owners OwnerSource
	// Sleeper supplies cooperative waits; nil uses cancellable timers.
	Sleeper Sleeper
	// Retry supplies contention jitter; nil uses crypto/rand with failure-to-zero.
	Retry RetrySource
	// MaxWaiters bounds simultaneous Acquire calls, not TryAcquire; zero defaults to 1024.
	MaxWaiters uint32
	// MaxManaged bounds active renewers across acquired handles; zero defaults to 1024.
	MaxManaged uint32
}

// Client acquires handles from one borrowed Backend. NewClient creates no
// background work; handles and optional renewers have their own explicit release/
// stop lifetimes. Concurrent calls are supported when borrowed sources/backends
// obey their contracts. A zero Client is unusable, and the client never closes
// backend resources or automatically replays ambiguous mutations.
type Client struct {
	backend Backend
	clock   Clock
	owners  OwnerSource
	sleeper Sleeper
	retry   RetrySource
	waiters chan struct{}
	managed chan struct{}
}

// NewClient borrows backend and sources, snapshots options and supplies defaults.
// Nil backend or excessive capacities return ErrInvalidState. It does not connect,
// inspect backend durability, validate typed-nil implementations or add a closer.
// Cryptographic default owner/jitter generation is not caller authentication.
func NewClient(backend Backend, options ClientOptions) (*Client, error) {
	if backend == nil {
		return nil, fmt.Errorf("%w: nil backend", ErrInvalidState)
	}
	if options.Clock == nil {
		options.Clock = wallClock{}
	}
	if options.Owners == nil {
		options.Owners = randomOwners{reader: rand.Reader}
	}
	if options.Sleeper == nil {
		options.Sleeper = timerSleeper{}
	}
	if options.Retry == nil {
		options.Retry = randomRetry{reader: rand.Reader}
	}
	if options.MaxWaiters == 0 {
		options.MaxWaiters = 1_024
	}
	if options.MaxManaged == 0 {
		options.MaxManaged = 1_024
	}
	if options.MaxWaiters > MaxClientWaiters || options.MaxManaged > MaxClientManaged {
		return nil, Wrap(ErrInvalidState, "client capacity")
	}
	return &Client{
		backend: backend, clock: options.Clock,
		owners: options.Owners, sleeper: options.Sleeper, retry: options.Retry,
		waiters: make(chan struct{}, options.MaxWaiters),
		managed: make(chan struct{}, options.MaxManaged),
	}, nil
}

// TryAcquire makes one backend acquisition call, outside the MaxWaiters bound.
// Use a nonnil context and validated Key/Policy; a canceled context is rejected
// before sources/backend calls. Owner generation precedes the operation timeout.
// A successful response must match key/owner, nonzero token and ordered times,
// otherwise ErrAmbiguousOutcome is returned without assuming remote absence.
// Backend errors are propagated; bounded contexts require cooperative sources.
func (client *Client) TryAcquire(
	ctx context.Context,
	key Key,
	policy Policy,
) (*Handle, error) {
	if err := acquisitionContextError(ctx); err != nil {
		return nil, err
	}
	owner, err := client.owners.NewOwner()
	if err != nil || owner == "" || len(owner) > 128 {
		return nil, Wrap(ErrBackendUnavailable, "owner generation")
	}
	operationContext, cancel := context.WithTimeout(ctx, policy.OperationTimeout())
	defer cancel()
	started := client.clock.Now()
	wallStarted := time.Now()
	record, err := client.backend.TryAcquire(operationContext, key, owner, policy.TTL())
	if err != nil {
		return nil, err
	}
	if !validAcquisition(key, owner, record) {
		return nil, Wrap(ErrAmbiguousOutcome, "acquire response")
	}
	return newHandleAt(
		client.backend, client.clock, client.sleeper, client.managed, policy, record,
		started, wallStarted,
	), nil
}

// Acquire nonblockingly reserves a waiter slot and retries only ErrContended
// within Policy.Wait and MaxAttempts, reusing one owner. Full capacity reports
// ErrBackendUnavailable. No wait means one attempt; exhausted contention/wait
// reports ErrTimeout. Nonnil contexts are checked before sources/backend calls.
// Wait and per-call backend contexts use real deadlines as well as injected time,
// but synchronous owner/jitter callbacks are not forcibly timed or contained.
// Other backend errors, including ambiguous mutation, return without retry.
func (client *Client) Acquire(
	ctx context.Context,
	key Key,
	policy Policy,
) (*Handle, error) {
	if err := acquisitionContextError(ctx); err != nil {
		return nil, err
	}
	select {
	case client.waiters <- struct{}{}:
		defer func() { <-client.waiters }()
	default:
		return nil, Wrap(ErrBackendUnavailable, "waiter capacity")
	}
	owner, err := client.owners.NewOwner()
	if err != nil || owner == "" || len(owner) > 128 {
		return nil, Wrap(ErrBackendUnavailable, "owner generation")
	}
	waitContext := ctx
	cancelWait := func() {}
	if policy.Wait() > 0 {
		waitContext, cancelWait = context.WithTimeout(ctx, policy.Wait())
	}
	defer cancelWait()
	started := client.clock.Now()
	deadline := started.Add(policy.Wait())
	for attempt := uint32(1); ; attempt++ {
		operationContext, cancel := context.WithTimeout(waitContext, policy.OperationTimeout())
		attemptStarted := client.clock.Now()
		wallStarted := time.Now()
		record, acquireErr := client.backend.TryAcquire(operationContext, key, owner, policy.TTL())
		cancel()
		if acquireErr == nil {
			if !validAcquisition(key, owner, record) {
				return nil, Wrap(ErrAmbiguousOutcome, "acquire response")
			}
			return newHandleAt(
				client.backend, client.clock, client.sleeper, client.managed, policy, record,
				attemptStarted, wallStarted,
			), nil
		}
		if waitDeadlineReached(ctx, waitContext) {
			return nil, Wrap(ErrTimeout, "acquire")
		}
		if !isContention(acquireErr) {
			return nil, acquireErr
		}
		delay := policy.Retry() + boundedJitter(client.retry, policy.Jitter())
		if policy.Wait() == 0 || attempt == policy.MaxAttempts() ||
			!client.clock.Now().Add(delay).Before(deadline) {
			return nil, Wrap(ErrTimeout, "acquire")
		}
		if sleepErr := client.sleeper.Sleep(waitContext, delay); sleepErr != nil {
			if waitDeadlineReached(ctx, waitContext) {
				return nil, Wrap(ErrTimeout, "acquire")
			}
			return nil, Wrap(ErrCanceled, "acquire")
		}
	}
}

func acquisitionContextError(ctx context.Context) error {
	if ctx == nil {
		return Wrap(ErrInvalidState, "acquire context")
	}
	if ctx.Err() != nil {
		return errors.Join(Wrap(ErrCanceled, "acquire"), ctx.Err())
	}
	return nil
}

func isContention(err error) bool { return err != nil && errorIs(err, ErrContended) }

func waitDeadlineReached(parent, waitContext context.Context) bool {
	return parent.Err() == nil && waitContext.Err() == context.DeadlineExceeded
}

// Validate response identity without subtracting times from different clock
// domains. Local budget starts before the attempt so response latency consumes
// it; retained remote ownership can still require reconciliation on bad replies.
func validAcquisition(key Key, owner string, record Record) bool {
	return record.Key.String() == key.String() && record.Owner == owner &&
		record.Token != 0 && record.ExpiresAt.After(record.AcquiredAt)
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

type timerSleeper struct{}

func (timerSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type randomOwners struct{ reader io.Reader }

func (source randomOwners) NewOwner() (string, error) {
	var bytes [24]byte
	if _, err := io.ReadFull(source.reader, bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

type randomRetry struct{ reader io.Reader }

func (source randomRetry) Jitter(maximum time.Duration) time.Duration {
	if maximum < time.Nanosecond {
		return 0
	}
	var bytes [8]byte
	if _, err := io.ReadFull(source.reader, bytes[:]); err != nil {
		return 0
	}
	// #nosec G115 -- modulo bounds the value to a valid time.Duration.
	return time.Duration(binary.BigEndian.Uint64(bytes[:]) % (uint64(maximum) + 1))
}

func boundedJitter(source RetrySource, maximum time.Duration) time.Duration {
	return min(max(source.Jitter(maximum), 0), maximum)
}
