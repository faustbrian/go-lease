// Command protectedwrite demonstrates a process-local resource that compares
// and stores its highest fence with an effect under one mutex. Equal fences are
// rejected too, so this example allows one accepted write per token, not arbitrary
// multiple effects under a lease. It needs no external service/cgo and is not a
// durable resource, authentication boundary or distributed continuity mechanism.
package main

import (
	"errors"
	"fmt"
	"sync"

	lease "github.com/faustbrian/go-lease/v3"
)

type resource struct {
	mu    sync.Mutex
	fence lease.Token
	value string
}

// Compare the fence and apply the effect in the same critical section. Splitting
// them would let a predecessor overwrite a successor between check and mutation.
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
