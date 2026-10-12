package lease

import (
	"fmt"
	"time"
)

const (
	// MaxTTL bounds one remote lease lifetime.
	MaxTTL = 24 * time.Hour
	// MaxWait bounds acquisition wall-clock waiting.
	MaxWait = time.Hour
	// MaxAttempts bounds backend operations in one acquisition.
	MaxAttempts uint32 = 10_000
	// MaxOperationTimeout bounds one backend call.
	MaxOperationTimeout = time.Minute
)

// FailureBehavior defines ownership admission after backend failure.
type FailureBehavior uint8

const (
	// FailureFailClosed denies admission after every uncertain operation.
	FailureFailClosed FailureBehavior = iota + 1
)

// PolicyOptions configures immutable acquisition and renewal behavior. Zero
// options are invalid; only OperationTimeout and FailureBehavior have defaults.
type PolicyOptions struct {
	// TTL is positive, at most MaxTTL. Native stores need at least one millisecond
	// and truncate fractional milliseconds; prefer whole milliseconds.
	TTL time.Duration
	// Wait is 0..MaxWait for Acquire, with zero allowing only one attempt.
	Wait time.Duration
	// Retry is a nonnegative delay, required positive when Wait is positive.
	Retry time.Duration
	// Jitter is nonnegative, at most Retry, and added to the retry delay.
	Jitter time.Duration
	// RenewEvery is zero to disable managed renewal or positive and below TTL-SafetyMargin.
	RenewEvery time.Duration
	// SafetyMargin reserves nonnegative local budget, strictly below TTL. Choose
	// it for latency, pauses and backend granularity; no remote clock check sets it.
	SafetyMargin time.Duration
	// MaxAttempts is 1..lease.MaxAttempts, counting all Acquire backend attempts.
	MaxAttempts uint32
	// OperationTimeout is positive and at most MaxOperationTimeout; zero defaults
	// to min(TTL, 30 seconds). It requires backend context cooperation.
	OperationTimeout time.Duration
	// FailureBehavior defaults to FailureFailClosed, the only accepted behavior.
	FailureBehavior FailureBehavior
}

// Policy is an immutable acquisition/renewal value safe to share. Construct it
// with NewPolicy; its zero value is invalid and backends may impose finer input
// restrictions, including native millisecond TTL granularity.
type Policy struct {
	ttl              time.Duration
	wait             time.Duration
	retry            time.Duration
	jitter           time.Duration
	renewEvery       time.Duration
	safetyMargin     time.Duration
	maxAttempts      uint32
	operationTimeout time.Duration
	failureBehavior  FailureBehavior
}

// NewPolicy validates and copies options, applying only the documented timeout
// and failure defaults. It rejects unsupported failure modes and invalid timing
// relationships as ErrInvalidState, without invoking or inspecting a backend.
func NewPolicy(options PolicyOptions) (Policy, error) {
	if options.OperationTimeout == 0 {
		options.OperationTimeout = min(options.TTL, 30*time.Second)
	}
	if options.FailureBehavior == 0 {
		options.FailureBehavior = FailureFailClosed
	}
	remainingTTL := options.TTL - options.SafetyMargin
	invalidRenewal := options.RenewEvery < 0
	if options.RenewEvery != 0 {
		invalidRenewal = invalidRenewal || options.RenewEvery >= remainingTTL
	}
	if options.TTL < time.Nanosecond || options.TTL > MaxTTL ||
		options.Wait < 0 || options.Wait > MaxWait || options.Retry < 0 || options.Retry > MaxWait ||
		options.Jitter < 0 || options.SafetyMargin < 0 ||
		remainingTTL < time.Nanosecond || options.Jitter > options.Retry ||
		invalidRenewal ||
		options.MaxAttempts == 0 || options.MaxAttempts > MaxAttempts ||
		options.OperationTimeout < time.Nanosecond || options.OperationTimeout > MaxOperationTimeout ||
		options.FailureBehavior != FailureFailClosed ||
		(options.Wait > 0 && options.Retry == 0) {
		return Policy{}, fmt.Errorf("%w: invalid policy", ErrInvalidState)
	}
	return Policy{
		ttl: options.TTL, wait: options.Wait, retry: options.Retry,
		jitter: options.Jitter, renewEvery: options.RenewEvery,
		safetyMargin: options.SafetyMargin, maxAttempts: options.MaxAttempts,
		operationTimeout: options.OperationTimeout, failureBehavior: options.FailureBehavior,
	}, nil
}

// TTL returns the remote lease lifetime.
func (policy Policy) TTL() time.Duration { return policy.ttl }

// Wait returns the maximum acquisition wait.
func (policy Policy) Wait() time.Duration { return policy.wait }

// Retry returns the retry interval before jitter.
func (policy Policy) Retry() time.Duration { return policy.retry }

// Jitter returns the maximum retry jitter.
func (policy Policy) Jitter() time.Duration { return policy.jitter }

// RenewEvery returns the managed-renewal interval, or zero when disabled.
func (policy Policy) RenewEvery() time.Duration { return policy.renewEvery }

// SafetyMargin returns time reserved for delay and uncertainty.
func (policy Policy) SafetyMargin() time.Duration { return policy.safetyMargin }

// MaxAttempts returns the total acquisition attempt bound.
func (policy Policy) MaxAttempts() uint32 { return policy.maxAttempts }

// OperationTimeout returns the maximum duration of one backend call.
func (policy Policy) OperationTimeout() time.Duration { return policy.operationTimeout }

// FailureBehavior returns the immutable fail-closed admission policy.
func (policy Policy) FailureBehavior() FailureBehavior { return policy.failureBehavior }
