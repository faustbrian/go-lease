package lease

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxKeyBytes bounds a complete encoded lease key.
const MaxKeyBytes = 256

// Key is a validated, namespaced lease identity.
type Key struct {
	value string
}

// NewKey constructs a bounded key from non-empty namespace and name segments.
func NewKey(namespace, name string) (Key, error) {
	value := namespace + "/" + name
	if !validSegment(namespace) || !validSegment(name) || len(value) > MaxKeyBytes {
		return Key{}, fmt.Errorf("%w: invalid key", ErrInvalidState)
	}
	return Key{value: value}, nil
}

// ParseKey validates a canonical namespace/name representation.
func ParseKey(value string) (Key, error) {
	namespace, name, found := strings.Cut(value, "/")
	if !found {
		return Key{}, fmt.Errorf("%w: invalid key", ErrInvalidState)
	}
	return NewKey(namespace, name)
}

// String returns the canonical namespaced representation.
func (key Key) String() string {
	return key.value
}

func validSegment(segment string) bool {
	return segment != "" && utf8.ValidString(segment) &&
		!strings.ContainsAny(segment, "\x00\r\n")
}
