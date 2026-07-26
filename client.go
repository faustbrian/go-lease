package lease

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
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

// OwnerSource creates opaque owner identities.
type OwnerSource interface {
	NewOwner() (string, error)
}

// Sleeper performs a cancelable acquisition delay.
type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}

// RetrySource supplies deterministic bounded acquisition jitter.
type RetrySource interface {
	Jitter(time.Duration) time.Duration
}

// ClientOptions injects deterministic sources used by a lease client.
type ClientOptions struct {
	Clock      Clock
	Owners     OwnerSource
	Sleeper    Sleeper
	Retry      RetrySource
	MaxWaiters uint32
	MaxManaged uint32
}

// Client acquires handles from one lease backend.
type Client struct {
	backend Backend
	clock   Clock
	owners  OwnerSource
	sleeper Sleeper
	retry   RetrySource
	waiters chan struct{}
	managed chan struct{}
}

// NewClient constructs a lease client with cryptographic production defaults.
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

// TryAcquire performs exactly one atomic acquisition attempt.
func (client *Client) TryAcquire(
	ctx context.Context,
	key Key,
	policy Policy,
) (*Handle, error) {
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

// Acquire retries contention within both wait and attempt bounds.
func (client *Client) Acquire(
	ctx context.Context,
	key Key,
	policy Policy,
) (*Handle, error) {
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

func isContention(err error) bool { return err != nil && errorIs(err, ErrContended) }

func waitDeadlineReached(parent, waitContext context.Context) bool {
	return parent.Err() == nil && waitContext.Err() == context.DeadlineExceeded
}

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
	if maximum <= 0 {
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
	jitter := source.Jitter(maximum)
	if jitter < 0 {
		return 0
	}
	if jitter > maximum {
		return maximum
	}
	return jitter
}
