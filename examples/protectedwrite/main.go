// Command protectedwrite demonstrates rejecting a stale fencing token.
package main

import (
	"errors"
	"fmt"
	"sync"

	lease "github.com/faustbrian/golib/pkg/lease"
)

type resource struct {
	mu    sync.Mutex
	fence lease.Token
	value string
}

func (resource *resource) write(fence lease.Token, value string) error {
	resource.mu.Lock()
	defer resource.mu.Unlock()
	if fence <= resource.fence {
		return lease.ErrStaleOwner
	}
	resource.fence = fence
	resource.value = value
	return nil
}

func main() {
	protected := &resource{}
	if err := protected.write(2, "successor"); err != nil {
		panic(err)
	}
	if err := protected.write(1, "stale predecessor"); !errors.Is(err, lease.ErrStaleOwner) {
		panic("stale write was accepted")
	}
	fmt.Println(protected.value)
}
