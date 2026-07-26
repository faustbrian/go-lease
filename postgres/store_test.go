package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/jackc/pgx/v5"
)

type fakeDatabase struct {
	rows  []pgx.Row
	query string
	args  []any
}

func (database *fakeDatabase) QueryRow(
	_ context.Context,
	query string,
	args ...any,
) pgx.Row {
	database.query = query
	database.args = append([]any(nil), args...)
	row := database.rows[0]
	database.rows = database.rows[1:]
	return row
}

type fakeRow struct {
	values []any
	err    error
}

func (row fakeRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	for index, value := range row.values {
		switch destination := destinations[index].(type) {
		case *int64:
			*destination = value.(int64)
		case *string:
			*destination = value.(string)
		case *time.Time:
			*destination = value.(time.Time)
		}
	}
	return nil
}

func TestAcquireUsesPostgresClockAndDurableFence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	database := &fakeDatabase{rows: []pgx.Row{fakeRow{values: []any{
		int64(7), now, now.Add(time.Minute), "ok",
	}}}}
	store, err := newStore(database)
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}
	key, _ := lease.NewKey("scheduler", "daily")
	record, err := store.TryAcquire(context.Background(), key, "owner", time.Minute)
	if err != nil || record.Token != 7 || !record.AcquiredAt.Equal(now) {
		t.Fatalf("TryAcquire() = %+v, %v", record, err)
	}
	if !strings.Contains(database.query, "clock_timestamp()") {
		t.Fatalf("acquisition does not use backend clock: %s", database.query)
	}
	if strings.Contains(database.query, ")\n),") {
		t.Fatalf("acquisition contains an invalid CTE boundary: %s", database.query)
	}
}

func TestAcquireReportsFenceExhaustion(t *testing.T) {
	t.Parallel()

	now := time.Now()
	database := &fakeDatabase{rows: []pgx.Row{fakeRow{values: []any{
		int64(0), now, now, "exhausted",
	}}}}
	store, _ := newStore(database)
	key, _ := lease.NewKey("postgres", "exhausted")
	if _, err := store.TryAcquire(
		context.Background(), key, "owner", time.Second,
	); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("TryAcquire(exhausted) error = %v", err)
	}
}

func TestContinuationSQLComparesOwnerAndToken(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"renew": renewSQL, "validate": validateSQL, "release": releaseSQL,
	} {
		if !strings.Contains(query, "owner = $2") ||
			!strings.Contains(query, "fencing_token = $3") {
			t.Fatalf("%s does not compare owner and token", name)
		}
	}
}

func TestEmptyAndContendedOutcomesAreClassified(t *testing.T) {
	t.Parallel()

	key, _ := lease.NewKey("queue", "job")
	record := lease.Record{Key: key, Owner: "owner", Token: 1}
	tests := []struct {
		name string
		call func(*Store) error
		want error
		row  pgx.Row
	}{
		{"acquire", func(store *Store) error {
			_, err := store.TryAcquire(context.Background(), key, "owner", time.Second)
			return err
		}, lease.ErrContended, fakeRow{values: []any{
			int64(0), time.Now(), time.Now(), "contended",
		}}},
		{"renew", func(store *Store) error {
			_, err := store.Renew(context.Background(), record, time.Second)
			return err
		}, lease.ErrStaleOwner, fakeRow{err: pgx.ErrNoRows}},
		{"validate", func(store *Store) error {
			_, err := store.Validate(context.Background(), record)
			return err
		}, lease.ErrStaleOwner, fakeRow{err: pgx.ErrNoRows}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store, _ := newStore(&fakeDatabase{rows: []pgx.Row{test.row}})
			if err := test.call(store); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReleaseMapsAtomicOutcome(t *testing.T) {
	t.Parallel()

	key, _ := lease.NewKey("queue", "job")
	record := lease.Record{Key: key, Owner: "owner", Token: 1}
	for _, outcome := range []string{"ok", "idempotent", "stale"} {
		store, _ := newStore(&fakeDatabase{rows: []pgx.Row{fakeRow{values: []any{outcome}}}})
		err := store.Release(context.Background(), record)
		if outcome == "stale" && !errors.Is(err, lease.ErrStaleOwner) {
			t.Fatalf("Release(stale) error = %v", err)
		}
		if outcome != "stale" && err != nil {
			t.Fatalf("Release(%s) error = %v", outcome, err)
		}
	}
}

func TestMigrationOwnsFenceAndLeaseTables(t *testing.T) {
	t.Parallel()

	migration := SchemaMigration()
	if !strings.Contains(migration.Up, "lease_fences") ||
		!strings.Contains(migration.Up, "lease_records") ||
		!strings.Contains(migration.Up, "expires_at") {
		t.Fatalf("migration does not own required schema: %s", migration.Up)
	}
	if _, err := GoMigration(); err != nil {
		t.Fatalf("GoMigration() error = %v", err)
	}
}
