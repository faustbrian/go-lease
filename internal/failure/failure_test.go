package failure

import (
	"errors"
	"strings"
	"testing"
)

func TestWrapPreservesIdentityAndRedactsCause(t *testing.T) {
	t.Parallel()

	classification := errors.New("unavailable")
	cause := errors.New("secret-password")
	err := Wrap(classification, cause, "backend")
	if !errors.Is(err, classification) || !errors.Is(err, cause) {
		t.Fatalf("Wrap() lost error identity: %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("Wrap() leaked cause: %v", err)
	}
}
