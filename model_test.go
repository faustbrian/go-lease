package lease_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
)

func TestKeyIsBoundedAndNamespaced(t *testing.T) {
	t.Parallel()

	key, err := lease.NewKey("queue", "invoice:42")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	if got := key.String(); got != "queue/invoice:42" {
		t.Fatalf("String() = %q", got)
	}
	parsed, err := lease.ParseKey(key.String())
	if err != nil || parsed != key {
		t.Fatalf("ParseKey() = %v, %v", parsed, err)
	}
	if _, err := lease.ParseKey("missing-namespace"); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("ParseKey(invalid) error = %v", err)
	}
	if _, err := lease.NewKey("", "job"); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("empty namespace error = %v", err)
	}
	if _, err := lease.NewKey("queue", strings.Repeat("x", lease.MaxKeyBytes)); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("oversized key error = %v", err)
	}
}

func TestPolicyIsValidatedAndImmutable(t *testing.T) {
	t.Parallel()

	policy, err := lease.NewPolicy(lease.PolicyOptions{
		TTL:              10 * time.Second,
		Wait:             time.Second,
		Retry:            50 * time.Millisecond,
		Jitter:           10 * time.Millisecond,
		RenewEvery:       3 * time.Second,
		SafetyMargin:     time.Second,
		MaxAttempts:      20,
		OperationTimeout: 2 * time.Second,
		FailureBehavior:  lease.FailureFailClosed,
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	if policy.TTL() != 10*time.Second || policy.MaxAttempts() != 20 ||
		policy.OperationTimeout() != 2*time.Second ||
		policy.FailureBehavior() != lease.FailureFailClosed {
		t.Fatalf("policy accessors returned unexpected values")
	}

	invalid := []lease.PolicyOptions{
		{},
		{TTL: time.Second, SafetyMargin: time.Second},
		{TTL: time.Second, Wait: time.Second, Retry: 0, MaxAttempts: 1},
		{TTL: time.Second, Retry: time.Second, Jitter: 2 * time.Second, MaxAttempts: 1},
		{TTL: time.Second, RenewEvery: time.Second, MaxAttempts: 1},
		{TTL: lease.MaxTTL + time.Second, MaxAttempts: 1},
		{TTL: time.Second, Wait: lease.MaxWait + time.Second, MaxAttempts: 1},
		{TTL: time.Second, Retry: lease.MaxWait + time.Second, MaxAttempts: 1},
		{TTL: time.Second, MaxAttempts: lease.MaxAttempts + 1},
		{TTL: time.Second, RenewEvery: 900 * time.Millisecond, SafetyMargin: 200 * time.Millisecond, MaxAttempts: 1},
		{TTL: time.Second, OperationTimeout: lease.MaxOperationTimeout + time.Second, MaxAttempts: 1},
		{TTL: time.Second, FailureBehavior: lease.FailureBehavior(255), MaxAttempts: 1},
	}
	for _, options := range invalid {
		if _, err := lease.NewPolicy(options); !errors.Is(err, lease.ErrInvalidState) {
			t.Fatalf("NewPolicy(%+v) error = %v", options, err)
		}
	}
}

func TestStableErrorsRemainClassifiable(t *testing.T) {
	t.Parallel()

	errorsToClassify := []error{
		lease.ErrContended,
		lease.ErrTimeout,
		lease.ErrCanceled,
		lease.ErrLost,
		lease.ErrStaleOwner,
		lease.ErrBackendUnavailable,
		lease.ErrInvalidState,
		lease.ErrAmbiguousOutcome,
	}
	for _, target := range errorsToClassify {
		wrapped := lease.Wrap(target, "acquire")
		if !errors.Is(wrapped, target) {
			t.Fatalf("errors.Is(%v, %v) = false", wrapped, target)
		}
	}
}

func TestRecordExpiryUsesSafetyDeadline(t *testing.T) {
	t.Parallel()

	acquired := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	record := lease.Record{
		AcquiredAt: acquired,
		ExpiresAt:  acquired.Add(10 * time.Second),
	}
	if got := record.SafeDeadline(time.Second); !got.Equal(acquired.Add(9 * time.Second)) {
		t.Fatalf("SafeDeadline() = %v", got)
	}
	if record.UsableAt(acquired.Add(9*time.Second), time.Second) {
		t.Fatal("record remained usable at its safety deadline")
	}
}
