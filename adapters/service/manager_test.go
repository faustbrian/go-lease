package leaseservice

import (
	"errors"
	"testing"

	lease "github.com/faustbrian/go-lease"
)

func TestCanonicalManagerValidatesConstruction(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, 1); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("New(nil) error = %v", err)
	}
}
