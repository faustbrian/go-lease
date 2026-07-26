package main

import (
	"errors"
	"strconv"
	"sync"
	"testing"

	lease "github.com/faustbrian/golib/pkg/lease"
)

func TestResourceRejectsStaleAndReplayWrites(t *testing.T) {
	t.Parallel()

	protected := &resource{}
	if err := protected.write(2, "successor"); err != nil {
		t.Fatalf("write(successor) error = %v", err)
	}
	for _, token := range []lease.Token{1, 2} {
		if err := protected.write(token, "stale"); !errors.Is(err, lease.ErrStaleOwner) {
			t.Fatalf("write(%d) error = %v", token, err)
		}
	}
	if protected.value != "successor" || protected.fence != 2 {
		t.Fatalf("protected resource = fence %d, value %q", protected.fence, protected.value)
	}
}

func TestConcurrentProtectedWritesRetainHighestFence(t *testing.T) {
	t.Parallel()

	protected := &resource{}
	var wait sync.WaitGroup
	for token := lease.Token(1); token <= 64; token++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = protected.write(token, strconv.FormatUint(uint64(token), 10))
		}()
	}
	wait.Wait()
	if protected.fence != 64 || protected.value != "64" {
		t.Fatalf("protected resource = fence %d, value %q", protected.fence, protected.value)
	}
}
