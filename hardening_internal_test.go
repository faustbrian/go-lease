package lease

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type stubBackend struct {
	acquire func(context.Context, Key, string, time.Duration) (Record, error)
	renew   func(context.Context, Record, time.Duration) (Record, error)
	check   func(context.Context, Record) (Record, error)
	release func(context.Context, Record) error
}

func (stub stubBackend) TryAcquire(ctx context.Context, key Key, owner string, ttl time.Duration) (Record, error) {
	return stub.acquire(ctx, key, owner, ttl)
}
func (stub stubBackend) Renew(ctx context.Context, record Record, ttl time.Duration) (Record, error) {
	return stub.renew(ctx, record, ttl)
}
func (stub stubBackend) Validate(ctx context.Context, record Record) (Record, error) {
	return stub.check(ctx, record)
}
func (stub stubBackend) Release(ctx context.Context, record Record) error {
	return stub.release(ctx, record)
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type partialReader struct{}

func (partialReader) Read(buffer []byte) (int, error) {
	copy(buffer, []byte{0, 0, 0, 0, 0, 1, 0})
	return 7, io.ErrUnexpectedEOF
}

type brokenOwners struct{ owner string }

func (source brokenOwners) NewOwner() (string, error) {
	if source.owner == "error" {
		return "", io.ErrUnexpectedEOF
	}
	return source.owner, nil
}

type cancelSleeper struct{}

func (cancelSleeper) Sleep(context.Context, time.Duration) error { return context.Canceled }

type hostileRetry struct{ value time.Duration }

func (source hostileRetry) Jitter(time.Duration) time.Duration { return source.value }

func successfulStub(now time.Time) stubBackend {
	return stubBackend{
		acquire: func(_ context.Context, key Key, owner string, ttl time.Duration) (Record, error) {
			return Record{Key: key, Owner: owner, Token: 1, AcquiredAt: now, ExpiresAt: now.Add(ttl)}, nil
		},
		renew: func(_ context.Context, record Record, ttl time.Duration) (Record, error) {
			record.ExpiresAt = now.Add(ttl)
			return record, nil
		},
		check:   func(_ context.Context, record Record) (Record, error) { return record, nil },
		release: func(context.Context, Record) error { return nil },
	}
}

func TestProductionDefaultsAndOwnerFailures(t *testing.T) {
	t.Parallel()

	if _, err := NewClient(nil, ClientOptions{}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("NewClient(nil) error = %v", err)
	}
	if _, err := NewClient(successfulStub(time.Now()), ClientOptions{
		MaxWaiters: MaxClientWaiters + 1,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("NewClient(waiter capacity) error = %v", err)
	}
	if _, err := NewClient(successfulStub(time.Now()), ClientOptions{
		MaxManaged: MaxClientManaged + 1,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("NewClient(managed capacity) error = %v", err)
	}
	now := time.Now()
	client, err := NewClient(successfulStub(now), ClientOptions{})
	if err != nil {
		t.Fatalf("NewClient(defaults) error = %v", err)
	}
	key, _ := NewKey("test", "defaults")
	policy, _ := NewPolicy(PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	handle, err := client.TryAcquire(context.Background(), key, policy)
	if err != nil || len(handle.Owner()) != 32 {
		t.Fatalf("TryAcquire(defaults) owner length = %d, error = %v", len(handle.Owner()), err)
	}
	for _, owner := range []string{"", "error", strings.Repeat("x", 129)} {
		client, _ := NewClient(successfulStub(now), ClientOptions{Owners: brokenOwners{owner: owner}})
		if _, err := client.TryAcquire(context.Background(), key, policy); !errors.Is(err, ErrBackendUnavailable) {
			t.Fatalf("TryAcquire(owner=%q) error = %v", owner, err)
		}
		if _, err := client.Acquire(context.Background(), key, policy); !errors.Is(err, ErrBackendUnavailable) {
			t.Fatalf("Acquire(owner=%q) error = %v", owner, err)
		}
	}
}

func TestClientAcceptsCapacityAndOwnerBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := successfulStub(now)
	client, err := NewClient(backend, ClientOptions{
		Owners:     brokenOwners{owner: strings.Repeat("o", 128)},
		MaxWaiters: MaxClientWaiters,
		MaxManaged: MaxClientManaged,
	})
	if err != nil {
		t.Fatalf("NewClient(maximum capacities) error = %v", err)
	}
	key, _ := NewKey("test", "client-boundaries")
	policy, _ := NewPolicy(PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	if _, err := client.TryAcquire(context.Background(), key, policy); err != nil {
		t.Fatalf("TryAcquire(128-byte owner) error = %v", err)
	}
	if _, err := client.Acquire(context.Background(), key, policy); err != nil {
		t.Fatalf("Acquire(128-byte owner) error = %v", err)
	}
}

func TestTryAcquirePropagatesBackendAndRandomSourceFailures(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := NewKey("test", "try-failures")
	policy, _ := NewPolicy(PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	backend := successfulStub(now)
	backend.acquire = func(context.Context, Key, string, time.Duration) (Record, error) {
		return Record{}, ErrContended
	}
	client, _ := NewClient(backend, ClientOptions{Owners: brokenOwners{owner: "owner"}})
	if _, err := client.TryAcquire(context.Background(), key, policy); !errors.Is(err, ErrContended) {
		t.Fatalf("TryAcquire(backend) error = %v", err)
	}
	if _, err := (randomOwners{reader: brokenReader{}}).NewOwner(); err == nil {
		t.Fatal("random owner reader error = nil")
	}
	if got := (randomRetry{reader: strings.NewReader(strings.Repeat("a", 8))}).Jitter(time.Second); got < 0 || got > time.Second {
		t.Fatalf("random jitter = %v", got)
	}
}

func TestRetryAndTimerSourcesCoverFailureBounds(t *testing.T) {
	t.Parallel()

	if got := (randomRetry{reader: brokenReader{}}).Jitter(time.Second); got != 0 {
		t.Fatalf("Jitter(reader failure) = %v", got)
	}
	if got := (randomRetry{reader: partialReader{}}).Jitter(4); got != 0 {
		t.Fatalf("Jitter(partial reader failure) = %v", got)
	}
	if got := (randomRetry{reader: strings.NewReader(strings.Repeat("a", 8))}).Jitter(0); got != 0 {
		t.Fatalf("Jitter(zero) = %v", got)
	}
	if got := (randomRetry{reader: strings.NewReader(strings.Repeat("a", 8))}).Jitter(-1); got != 0 {
		t.Fatalf("Jitter(negative) = %v", got)
	}
	if got := (randomRetry{reader: strings.NewReader("\x00\x00\x00\x00\x00\x00\x00\x03")}).Jitter(3); got != 3 {
		t.Fatalf("Jitter(inclusive maximum) = %v", got)
	}
	if got := (randomRetry{reader: strings.NewReader("\x00\x00\x00\x00\x00\x00\x00\x01")}).Jitter(1); got != 1 {
		t.Fatalf("Jitter(one nanosecond maximum) = %v", got)
	}
	if got := boundedJitter(hostileRetry{value: -1}, time.Second); got != 0 {
		t.Fatalf("boundedJitter(negative) = %v", got)
	}
	if got := boundedJitter(hostileRetry{value: 0}, time.Second); got != 0 {
		t.Fatalf("boundedJitter(zero) = %v", got)
	}
	if got := boundedJitter(hostileRetry{value: time.Second}, time.Second); got != time.Second {
		t.Fatalf("boundedJitter(maximum) = %v", got)
	}
	if got := boundedJitter(hostileRetry{value: 2 * time.Second}, time.Second); got != time.Second {
		t.Fatalf("boundedJitter(oversized) = %v", got)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (timerSleeper{}).Sleep(canceled, time.Hour); err == nil {
		t.Fatal("timer Sleep(canceled) error = nil")
	}
	if err := (timerSleeper{}).Sleep(context.Background(), time.Nanosecond); err != nil {
		t.Fatalf("timer Sleep() error = %v", err)
	}
	if (wallClock{}).Now().IsZero() {
		t.Fatal("wallClock.Now() is zero")
	}
}

func TestAcquisitionResponseFieldsAreIndependentlyValidated(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := NewKey("test", "response-fields")
	otherKey, _ := NewKey("test", "other")
	valid := Record{
		Key: key, Owner: "owner", Token: 1,
		AcquiredAt: now, ExpiresAt: now.Add(time.Second),
	}
	cases := map[string]Record{
		"key":        {Key: otherKey, Owner: valid.Owner, Token: valid.Token, AcquiredAt: valid.AcquiredAt, ExpiresAt: valid.ExpiresAt},
		"owner":      {Key: valid.Key, Owner: "other", Token: valid.Token, AcquiredAt: valid.AcquiredAt, ExpiresAt: valid.ExpiresAt},
		"token":      {Key: valid.Key, Owner: valid.Owner, Token: 0, AcquiredAt: valid.AcquiredAt, ExpiresAt: valid.ExpiresAt},
		"expiration": {Key: valid.Key, Owner: valid.Owner, Token: valid.Token, AcquiredAt: valid.AcquiredAt, ExpiresAt: valid.AcquiredAt},
	}
	if !validAcquisition(key, valid.Owner, valid) {
		t.Fatal("validAcquisition(valid) = false")
	}
	for name, record := range cases {
		if validAcquisition(key, valid.Owner, record) {
			t.Fatalf("validAcquisition(%s) = true", name)
		}
	}
}

func TestAcquirePropagatesBackendAndCancellation(t *testing.T) {
	t.Parallel()

	key, _ := NewKey("test", "acquire-errors")
	policy, _ := NewPolicy(PolicyOptions{
		TTL: time.Second, Wait: time.Second, Retry: time.Millisecond, MaxAttempts: 2,
	})
	backend := successfulStub(time.Now())
	backend.acquire = func(context.Context, Key, string, time.Duration) (Record, error) {
		return Record{}, ErrBackendUnavailable
	}
	client, _ := NewClient(backend, ClientOptions{Owners: brokenOwners{owner: "owner"}})
	if _, err := client.Acquire(context.Background(), key, policy); !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("Acquire(backend) error = %v", err)
	}
	backend.acquire = func(context.Context, Key, string, time.Duration) (Record, error) {
		return Record{}, ErrContended
	}
	client, _ = NewClient(backend, ClientOptions{
		Owners: brokenOwners{owner: "owner"}, Sleeper: cancelSleeper{},
	})
	if _, err := client.Acquire(context.Background(), key, policy); !errors.Is(err, ErrCanceled) {
		t.Fatalf("Acquire(canceled sleep) error = %v", err)
	}
}

func TestAcquireRejectsMismatchedSuccessfulResponse(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := NewKey("test", "acquire-response")
	policy, _ := NewPolicy(PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	backend := successfulStub(now)
	backend.acquire = func(_ context.Context, current Key, _ string, ttl time.Duration) (Record, error) {
		return Record{
			Key: current, Owner: "spoofed", Token: 0,
			AcquiredAt: now, ExpiresAt: now.Add(ttl),
		}, nil
	}
	client, _ := NewClient(backend, ClientOptions{Owners: brokenOwners{owner: "owner"}})
	if handle, err := client.TryAcquire(context.Background(), key, policy); handle != nil ||
		!errors.Is(err, ErrAmbiguousOutcome) {
		t.Fatalf("TryAcquire() handle=%v error=%v", handle, err)
	}
	if handle, err := client.Acquire(context.Background(), key, policy); handle != nil ||
		!errors.Is(err, ErrAmbiguousOutcome) {
		t.Fatalf("Acquire() handle=%v error=%v", handle, err)
	}
}

func TestHandleFailureStatesAndAccessors(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := NewKey("test", "handle")
	policy, _ := NewPolicy(PolicyOptions{
		TTL: time.Second, Retry: time.Millisecond, SafetyMargin: time.Millisecond, MaxAttempts: 1,
	})
	record := Record{Key: key, Owner: "owner", Token: 3, AcquiredAt: now, ExpiresAt: now.Add(time.Second)}
	clock := fixedClock{now: now}
	backend := successfulStub(now)
	handle := newHandle(backend, clock, timerSleeper{}, make(chan struct{}, 1), policy, record)
	if !handle.AcquiredAt().Equal(now) || !handle.Deadline().Equal(now.Add(time.Second-time.Millisecond)) {
		t.Fatalf("handle accessors returned wrong times")
	}
	backend.renew = func(context.Context, Record, time.Duration) (Record, error) { return Record{}, ErrStaleOwner }
	handle.backend = backend
	if err := handle.Renew(context.Background()); !errors.Is(err, ErrStaleOwner) || handle.State() != StateLost {
		t.Fatalf("Renew(stale) state=%s error=%v", handle.State(), err)
	}
	if err := handle.Release(context.Background()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("Release(lost) error = %v", err)
	}
	if err := handle.Renew(context.Background()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("Renew(lost) error = %v", err)
	}
}

func TestHandleValidationReleaseAndManagedFailureBranches(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := NewKey("test", "handle-failures")
	policy, _ := NewPolicy(PolicyOptions{TTL: time.Second, Retry: time.Millisecond, MaxAttempts: 1})
	record := Record{Key: key, Owner: "owner", Token: 1, AcquiredAt: now, ExpiresAt: now.Add(time.Second)}
	backend := successfulStub(now)
	backend.check = func(context.Context, Record) (Record, error) { return Record{}, ErrBackendUnavailable }
	handle := newHandle(backend, fixedClock{now}, timerSleeper{}, make(chan struct{}, 1), policy, record)
	if err := handle.Validate(context.Background()); !errors.Is(err, ErrBackendUnavailable) || handle.State() != StateUncertain {
		t.Fatalf("Validate(unavailable) state=%s error=%v", handle.State(), err)
	}

	backend = successfulStub(now)
	backend.release = func(context.Context, Record) error { return ErrStaleOwner }
	handle = newHandle(backend, fixedClock{now}, timerSleeper{}, make(chan struct{}, 1), policy, record)
	if err := handle.Release(context.Background()); !errors.Is(err, ErrStaleOwner) || handle.State() != StateLost {
		t.Fatalf("Release(stale) state=%s error=%v", handle.State(), err)
	}
	if _, err := handle.StartManaged(context.Background()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("StartManaged(lost) error = %v", err)
	}

	handle = newHandle(successfulStub(now), fixedClock{now}, timerSleeper{}, make(chan struct{}, 1), policy, record)
	if _, err := handle.StartManaged(context.Background()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("StartManaged(disabled) error = %v", err)
	}
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type mutableClock struct{ now time.Time }

func (clock *mutableClock) Now() time.Time { return clock.now }

func TestHandleRejectsConcurrentOperationsWithoutBlockingState(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := NewKey("test", "concurrent-handle")
	policy, _ := NewPolicy(PolicyOptions{
		TTL: time.Second, RenewEvery: 100 * time.Millisecond,
		SafetyMargin: 100 * time.Millisecond, MaxAttempts: 1,
	})
	record := Record{
		Key: key, Owner: "owner", Token: 1,
		AcquiredAt: now, ExpiresAt: now.Add(time.Second),
	}
	entered := make(chan struct{})
	unblock := make(chan struct{})
	backend := successfulStub(now)
	backend.check = func(_ context.Context, current Record) (Record, error) {
		close(entered)
		<-unblock
		return current, nil
	}
	handle := newHandle(
		backend, fixedClock{now}, timerSleeper{}, make(chan struct{}, 1), policy, record,
	)
	done := make(chan error, 1)
	go func() { done <- handle.Validate(context.Background()) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("Validate() returned before backend entry: %v", err)
	}
	if handle.State() != StateActive {
		t.Fatalf("State() = %s", handle.State())
	}
	if err := handle.Renew(context.Background()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("Renew(concurrent) error = %v", err)
	}
	if err := handle.Release(context.Background()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("Release(concurrent) error = %v", err)
	}
	if _, err := handle.StartManaged(context.Background()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("StartManaged(concurrent) error = %v", err)
	}
	close(unblock)
	if err := <-done; err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestHandleFailsClosedWhenDeadlinePassesDuringOperation(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := NewKey("test", "operation-deadline")
	policy, _ := NewPolicy(PolicyOptions{
		TTL: time.Second, SafetyMargin: 100 * time.Millisecond, MaxAttempts: 1,
	})
	record := Record{
		Key: key, Owner: "owner", Token: 1,
		AcquiredAt: now, ExpiresAt: now.Add(time.Second),
	}
	clock := &mutableClock{now: now}
	backend := successfulStub(now)
	backend.check = func(_ context.Context, current Record) (Record, error) {
		clock.now = now.Add(time.Second)
		return current, nil
	}
	handle := newHandle(
		backend, clock, timerSleeper{}, make(chan struct{}, 1), policy, record,
	)
	if err := handle.Validate(context.Background()); !errors.Is(err, ErrLost) ||
		handle.State() != StateExpired {
		t.Fatalf("Validate(deadline) state=%s error=%v", handle.State(), err)
	}

	clock.now = now
	backend.check = func(context.Context, Record) (Record, error) {
		clock.now = now.Add(time.Second)
		return Record{}, ErrBackendUnavailable
	}
	handle = newHandle(
		backend, clock, timerSleeper{}, make(chan struct{}, 1), policy, record,
	)
	if err := handle.Validate(context.Background()); !errors.Is(err, ErrBackendUnavailable) ||
		handle.State() != StateExpired {
		t.Fatalf("Validate(failure) state=%s error=%v", handle.State(), err)
	}
}

func TestHandleDeadlineIgnoresBackendClockSkew(t *testing.T) {
	t.Parallel()

	now := time.Now()
	clock := &mutableClock{now: now}
	key, _ := NewKey("test", "backend-clock-skew")
	policy, _ := NewPolicy(PolicyOptions{
		TTL: time.Second, SafetyMargin: 100 * time.Millisecond, MaxAttempts: 1,
	})
	record := Record{
		Key: key, Owner: "owner", Token: 1,
		AcquiredAt: now.Add(24 * time.Hour),
		ExpiresAt:  now.Add(24*time.Hour + time.Second),
	}
	handle := newHandle(
		successfulStub(now), clock, timerSleeper{}, make(chan struct{}, 1), policy, record,
	)
	clock.now = now.Add(900 * time.Millisecond)
	if handle.State() != StateExpired {
		t.Fatalf("State() with forward-skewed backend clock = %s", handle.State())
	}
}

func TestHandleDeadlineSurvivesFrozenInjectedClock(t *testing.T) {
	t.Parallel()

	now := time.Now()
	clock := &mutableClock{now: now}
	key, _ := NewKey("test", "frozen-clock")
	policy, _ := NewPolicy(PolicyOptions{TTL: 20 * time.Millisecond, MaxAttempts: 1})
	record := Record{
		Key: key, Owner: "owner", Token: 1,
		AcquiredAt: now, ExpiresAt: now.Add(20 * time.Millisecond),
	}
	handle := newHandle(
		successfulStub(now), clock, timerSleeper{}, make(chan struct{}, 1), policy, record,
	)
	time.Sleep(30 * time.Millisecond)
	clock.now = now.Add(-time.Hour)
	if handle.State() != StateExpired {
		t.Fatalf("State() with frozen or rolled-back injected clock = %s", handle.State())
	}
}

func TestHandleRejectsMismatchedSuccessfulResponses(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := NewKey("test", "mismatched-response")
	policy, _ := NewPolicy(PolicyOptions{TTL: time.Second, MaxAttempts: 1})
	record := Record{
		Key: key, Owner: "owner", Token: 7,
		AcquiredAt: now, ExpiresAt: now.Add(time.Second),
	}
	backend := successfulStub(now)
	backend.renew = func(_ context.Context, current Record, _ time.Duration) (Record, error) {
		current.Token++
		return current, nil
	}
	handle := newHandle(
		backend, fixedClock{now}, timerSleeper{}, make(chan struct{}, 1), policy, record,
	)
	if err := handle.Renew(context.Background()); !errors.Is(err, ErrAmbiguousOutcome) ||
		handle.State() != StateUncertain || handle.Token() != record.Token {
		t.Fatalf("Renew(mismatch) token=%d state=%s error=%v", handle.Token(), handle.State(), err)
	}

	backend = successfulStub(now)
	backend.check = func(_ context.Context, current Record) (Record, error) {
		current.Owner = "successor"
		return current, nil
	}
	handle = newHandle(
		backend, fixedClock{now}, timerSleeper{}, make(chan struct{}, 1), policy, record,
	)
	if err := handle.Validate(context.Background()); !errors.Is(err, ErrBackendUnavailable) ||
		handle.State() != StateUncertain || handle.Owner() != record.Owner {
		t.Fatalf("Validate(mismatch) owner=%q state=%s error=%v", handle.Owner(), handle.State(), err)
	}
}

func TestRenewUsesSafetyBudgetAndReleasedValidateDoesNotCallBackend(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := NewKey("test", "renew-budget")
	policy, _ := NewPolicy(PolicyOptions{
		TTL: time.Second, SafetyMargin: 250 * time.Millisecond, MaxAttempts: 1,
	})
	record := Record{
		Key: key, Owner: "owner", Token: 1,
		AcquiredAt: now, ExpiresAt: now.Add(time.Second),
	}
	backend := successfulStub(now)
	handle := newHandle(backend, fixedClock{now}, timerSleeper{}, make(chan struct{}, 1), policy, record)
	if err := handle.Renew(context.Background()); err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
	if expected := now.Add(750 * time.Millisecond); !handle.Deadline().Equal(expected) {
		t.Fatalf("Deadline() = %v, want %v", handle.Deadline(), expected)
	}
	if err := handle.Release(context.Background()); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	validateCalls := 0
	backend.check = func(context.Context, Record) (Record, error) {
		validateCalls++
		return record, nil
	}
	handle.backend = backend
	if err := handle.Validate(context.Background()); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("Validate(released) error = %v", err)
	}
	if validateCalls != 0 {
		t.Fatalf("Validate(released) backend calls = %d", validateCalls)
	}
}

func TestStateNamesAndObservationOutcomes(t *testing.T) {
	t.Parallel()

	states := map[State]string{
		StateActive: "active", StateExpired: "expired", StateLost: "lost",
		StateUncertain: "uncertain", StateReleased: "released", State(255): "invalid",
	}
	for state, expected := range states {
		if state.String() != expected {
			t.Fatalf("State(%d).String() = %q", state, state.String())
		}
	}
	outcomes := map[error]Outcome{
		nil: OutcomeSuccess, ErrContended: OutcomeContended, ErrTimeout: OutcomeContended,
		ErrStaleOwner: OutcomeStale, ErrLost: OutcomeStale, ErrCanceled: OutcomeCanceled,
		ErrAmbiguousOutcome: OutcomeAmbiguous, ErrInvalidState: OutcomeInvalid,
		ErrBackendUnavailable: OutcomeUnavailable,
	}
	for err, expected := range outcomes {
		if got := classifyOutcome(err); got != expected {
			t.Fatalf("classifyOutcome(%v) = %s", err, got)
		}
	}
}

func TestObservedBackendValidationAndAllOperations(t *testing.T) {
	t.Parallel()

	now := time.Now()
	backend := successfulStub(now)
	observer := ObserverFunc(func(Event) {})
	if _, err := NewObservedBackend(nil, fixedClock{now}, observer); err == nil {
		t.Fatal("NewObservedBackend(nil) error = nil")
	}
	if _, err := NewObservedBackend(backend, nil, observer); err == nil {
		t.Fatal("NewObservedBackend(nil clock) error = nil")
	}
	if _, err := NewObservedBackend(backend, fixedClock{now}); err == nil {
		t.Fatal("NewObservedBackend(no observers) error = nil")
	}
	if _, err := NewObservedBackend(backend, fixedClock{now}, nil); err == nil {
		t.Fatal("NewObservedBackend(nil observer) error = nil")
	}
	many := make([]Observer, 17)
	for index := range many {
		many[index] = observer
	}
	if _, err := NewObservedBackend(backend, fixedClock{now}, many...); err == nil {
		t.Fatal("NewObservedBackend(too many) error = nil")
	}
	maximum := make([]Observer, 16)
	for index := range maximum {
		maximum[index] = observer
	}
	if _, err := NewObservedBackend(backend, fixedClock{now}, maximum...); err != nil {
		t.Fatalf("NewObservedBackend(maximum observers) error = %v", err)
	}
	observed, _ := NewObservedBackend(backend, fixedClock{now}, observer)
	key, _ := NewKey("test", "observed")
	record, _ := observed.TryAcquire(context.Background(), key, "owner", time.Second)
	record, _ = observed.Renew(context.Background(), record, time.Second)
	record, _ = observed.Validate(context.Background(), record)
	if err := observed.Release(context.Background(), record); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}
