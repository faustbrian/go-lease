package lease

import (
	"errors"
	"fmt"
)

var (
	// ErrContended reports that another owner holds the requested lease.
	ErrContended = errors.New("lease: contended")
	// ErrTimeout reports that a bounded acquisition wait elapsed.
	ErrTimeout = errors.New("lease: timeout")
	// ErrCanceled reports that the caller canceled an operation.
	ErrCanceled = errors.New("lease: canceled")
	// ErrLost reports that a formerly owned lease is no longer safely usable.
	ErrLost = errors.New("lease: lost")
	// ErrStaleOwner reports an owner or fencing token that is no longer current.
	ErrStaleOwner = errors.New("lease: stale owner")
	// ErrBackendUnavailable reports backend, response or local-capacity failure.
	// Its presence alone does not prove that a remote mutation was absent.
	ErrBackendUnavailable = errors.New("lease: backend unavailable")
	// ErrInvalidState reports invalid input or an invalid state transition.
	ErrInvalidState = errors.New("lease: invalid state")
	// ErrAmbiguousOutcome reports that a remote mutation may have committed.
	ErrAmbiguousOutcome = errors.New("lease: ambiguous outcome")
)

// Wrap prefixes operation and wraps err unchanged. Neither input is sanitized:
// use safe operation labels and errors, not keys, credentials or raw driver text.
// Native adapters separately hide cause formatting through internal/failure.
func Wrap(err error, operation string) error {
	return fmt.Errorf("lease %s: %w", operation, err)
}

func errorIs(err, target error) bool { return errors.Is(err, target) }
