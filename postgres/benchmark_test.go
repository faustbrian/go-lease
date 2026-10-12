package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// The benchmark returns one fixed local count; it neither executes cleanup SQL
// nor models lock contention, I/O or live PostgreSQL persistence.
type cleanupBenchmarkDatabase struct{}

func (cleanupBenchmarkDatabase) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRow{values: []any{int64(100)}}
}

// BenchmarkCleanup measures local cleanup-call validation and count conversion
// using a fake 100-row result, with setup included; not database deletion speed.
func BenchmarkCleanup(b *testing.B) {
	store, _ := newStore(cleanupBenchmarkDatabase{})
	b.ReportAllocs()
	for range b.N {
		if _, err := store.Cleanup(context.Background(), 100); err != nil {
			b.Fatal(err)
		}
	}
}
