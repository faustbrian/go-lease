// Package failure preserves error identity without rendering sensitive causes.
package failure

// Classified is a redacted error with a stable classification and hidden
// programmatic cause.
type Classified struct {
	operation      string
	classification error
	cause          error
}

// Wrap returns a redacted classified error that remains compatible with
// errors.Is for both the classification and original cause.
func Wrap(classification, cause error, operation string) error {
	return &Classified{
		operation: operation, classification: classification, cause: cause,
	}
}

// Error deliberately excludes the underlying cause.
func (err *Classified) Error() string {
	return "lease " + err.operation + ": " + err.classification.Error()
}

// Unwrap exposes identities to errors.Is without exposing cause text.
func (err *Classified) Unwrap() []error {
	return []error{err.classification, err.cause}
}
