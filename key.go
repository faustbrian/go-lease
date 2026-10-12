package lease

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxKeyBytes bounds a complete encoded lease key.
const MaxKeyBytes = 256

// Key is an immutable bounded identity. Its zero value is unusable. The encoded
// string, not separate namespace/name components, determines backend identity.
type Key struct {
	value string
}

// NewKey joins nonempty valid-UTF-8 namespace and name with a literal slash,
// rejecting NUL/CR/LF and an encoded length above MaxKeyBytes as ErrInvalidState.
// It does not trim, normalize Unicode, escape slashes or enforce tenant authority;
// different component splits can encode the same identity. Choose namespaces
// and naming rules deliberately, as in docs/fencing.md.
func NewKey(namespace, name string) (Key, error) {
	value := namespace + "/" + name
	if !validSegment(namespace) || !validSegment(name) || len(value) > MaxKeyBytes {
		return Key{}, fmt.Errorf("%w: invalid key", ErrInvalidState)
	}
	return Key{value: value}, nil
}

// ParseKey splits at the first slash and applies NewKey validation. Additional
// slashes remain in the name; accepted input bytes are preserved unchanged.
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
