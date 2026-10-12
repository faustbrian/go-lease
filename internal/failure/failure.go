// Package failure formats stable lease classifications without rendering driver
// causes. Native backends use Wrap with safe operation/classification text;
// error traversal still exposes original causes, so diagnostics must be selective.
// It adds no resource lifetime, service dependency or general-purpose redaction.
package failure

// Classified hides the cause only in its ordinary Error rendering. The
// classification and operation must already be safe; Unwrap exposes both errors
// programmatically, including any sensitive text carried by the original cause.
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

// Unwrap returns classification and original cause for errors.Is/errors.As.
// Traversing or logging the returned cause can expose its raw text; formatting
// protection does not erase the cause or make arbitrary traversal safe.
func (err *Classified) Unwrap() []error {
	return []error{err.classification, err.cause}
}
