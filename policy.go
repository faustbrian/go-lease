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

// PolicyOptions configures immutable acquisition and renewal behavior.
type PolicyOptions struct {
	TTL              time.Duration
	Wait             time.Duration
	Retry            time.Duration
	Jitter           time.Duration
	RenewEvery       time.Duration
	SafetyMargin     time.Duration
	MaxAttempts      uint32
	OperationTimeout time.Duration
	FailureBehavior  FailureBehavior
}

// Policy is an immutable, bounded acquisition policy.
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

// NewPolicy validates and copies acquisition options.
func NewPolicy(options PolicyOptions) (Policy, error) {
	if options.OperationTimeout == 0 {
		options.OperationTimeout = min(options.TTL, 30*time.Second)
	}
	if options.FailureBehavior == 0 {
		options.FailureBehavior = FailureFailClosed
	}
	if options.TTL <= 0 || options.TTL > MaxTTL ||
		options.Wait < 0 || options.Wait > MaxWait || options.Retry < 0 || options.Retry > MaxWait ||
		options.Jitter < 0 || options.SafetyMargin < 0 ||
		options.SafetyMargin >= options.TTL || options.Jitter > options.Retry ||
		options.RenewEvery < 0 ||
		(options.RenewEvery > 0 && options.RenewEvery >= options.TTL-options.SafetyMargin) ||
		options.MaxAttempts == 0 || options.MaxAttempts > MaxAttempts ||
		options.OperationTimeout <= 0 || options.OperationTimeout > MaxOperationTimeout ||
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
