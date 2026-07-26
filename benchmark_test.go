package lease_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
	"github.com/faustbrian/golib/pkg/lease/memory"
)

func BenchmarkAcquireRelease(b *testing.B) {
	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: ^uint32(0)})
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		key, _ := lease.NewKey("benchmark", strconv.Itoa(index))
		record, err := store.TryAcquire(context.Background(), key, "owner", time.Second)
		if err != nil {
			b.Fatal(err)
		}
		if err := store.Release(context.Background(), record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenew(b *testing.B) {
	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	key, _ := lease.NewKey("benchmark", "renew")
	record, _ := store.TryAcquire(context.Background(), key, "owner", time.Hour)
	b.ReportAllocs()
	for range b.N {
		var err error
		record, err = store.Renew(context.Background(), record, time.Hour)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkContention(b *testing.B) {
	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	key, _ := lease.NewKey("benchmark", "contended")
	if _, err := store.TryAcquire(context.Background(), key, "owner", time.Hour); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := store.TryAcquire(context.Background(), key, "successor", time.Hour); !errors.Is(err, lease.ErrContended) {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenewalLoad(b *testing.B) {
	clock := leasetest.NewClock(time.Now())
	store, _ := memory.New(memory.Options{Clock: clock, MaxKeys: 1})
	key, _ := lease.NewKey("benchmark", "renewal-load")
	record, _ := store.TryAcquire(context.Background(), key, "owner", time.Hour)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := store.Renew(context.Background(), record, time.Hour); err != nil {
				b.Fatal(err)
			}
		}
	})
}
