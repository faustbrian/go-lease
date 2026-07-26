package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
	"github.com/faustbrian/golib/pkg/lease/memory"
)

func FuzzLeaseStateModel(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4})
	f.Add([]byte{0, 4, 0, 2, 3})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 128 {
			operations = operations[:128]
		}
		clock := leasetest.NewClock(time.Unix(1_700_000_000, 0).UTC())
		store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
		key, _ := lease.NewKey("model", "lease")
		var current lease.Record
		var lastToken lease.Token
		for _, operation := range operations {
			switch operation % 5 {
			case 0:
				record, err := store.TryAcquire(context.Background(), key, "owner", time.Second)
				if err == nil {
					if record.Token <= lastToken {
						t.Fatalf("token did not increase: %d <= %d", record.Token, lastToken)
					}
					current, lastToken = record, record.Token
				} else if !errors.Is(err, lease.ErrContended) {
					t.Fatalf("unexpected acquire error: %v", err)
				}
			case 1:
				if current.Token != 0 {
					_, _ = store.Renew(context.Background(), current, time.Second)
				}
			case 2:
				if current.Token != 0 {
					_ = store.Release(context.Background(), current)
				}
			case 3:
				clock.Advance(time.Second)
			case 4:
				clock.Advance(-time.Millisecond)
			}
		}
	})
}
