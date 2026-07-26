package lease

import (
	"context"
	"time"
)

// Clock supplies time without production global state in deterministic tests.
type Clock interface {
	Now() time.Time
}

// Backend atomically persists one fenced lease record per key.
type Backend interface {
	TryAcquire(context.Context, Key, string, time.Duration) (Record, error)
	Renew(context.Context, Record, time.Duration) (Record, error)
	Validate(context.Context, Record) (Record, error)
	Release(context.Context, Record) error
}
