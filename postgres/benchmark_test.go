package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

type cleanupBenchmarkDatabase struct{}

func (cleanupBenchmarkDatabase) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRow{values: []any{int64(100)}}
}

func BenchmarkCleanup(b *testing.B) {
	store, _ := newStore(cleanupBenchmarkDatabase{})
	b.ReportAllocs()
	for range b.N {
		if _, err := store.Cleanup(context.Background(), 100); err != nil {
			b.Fatal(err)
		}
	}
}
