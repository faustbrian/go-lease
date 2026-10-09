package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	lease "github.com/faustbrian/go-lease/v2"
	leasepostgres "github.com/faustbrian/go-lease/v2/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLiveTimestampDecoding(t *testing.T) {
	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		t.Skip("POSTGRES_URL is not set")
	}
	for _, test := range []struct {
		name string
		mode pgx.QueryExecMode
	}{
		{name: "binary", mode: pgx.QueryExecModeCacheStatement},
		{name: "text", mode: pgx.QueryExecModeSimpleProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			config, err := pgxpool.ParseConfig(url)
			if err != nil {
				t.Fatalf("parse pool config: %v", err)
			}
			config.ConnConfig.DefaultQueryExecMode = test.mode
			config.ConnConfig.RuntimeParams["timezone"] = "Asia/Kolkata"
			config.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
				conn.TypeMap().RegisterType(&pgtype.Type{
					Name: "timestamptz", OID: pgtype.TimestamptzOID,
					Codec: &pgtype.TimestamptzCodec{ScanLocation: time.FixedZone("scan", 19800)},
				})
				return nil
			}
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatalf("open pool: %v", err)
			}
			defer pool.Close()
			migration := leasepostgres.SchemaMigration()
			if _, err := pool.Exec(ctx, migration.Up); err != nil {
				t.Fatalf("apply migration: %v", err)
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
				defer stop()
				if _, err := pool.Exec(cleanup, migration.Down); err != nil {
					t.Errorf("drop migration: %v", err)
				}
			}()
			store, err := leasepostgres.New(pool)
			if err != nil {
				t.Fatalf("new store: %v", err)
			}
			key, err := lease.NewKey("timestamp", test.name)
			if err != nil {
				t.Fatal(err)
			}
			owned, err := store.TryAcquire(ctx, key, "owner", 5*time.Second)
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			assertPostgresEpochs(ctx, t, pool, owned)
			renewed, err := store.Renew(ctx, owned, 10*time.Second)
			if err != nil {
				t.Fatalf("renew: %v", err)
			}
			if renewed.Key != key || renewed.Owner != owned.Owner || renewed.Token != owned.Token ||
				!renewed.AcquiredAt.Equal(owned.AcquiredAt) || !renewed.ExpiresAt.After(owned.ExpiresAt) {
				t.Fatalf("renewed ownership or timestamps changed: %+v -> %+v", owned, renewed)
			}
			assertPostgresEpochs(ctx, t, pool, renewed)
			validated, err := store.Validate(ctx, renewed)
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if validated != renewed {
				t.Fatalf("validated record = %+v, want %+v", validated, renewed)
			}
			assertPostgresEpochs(ctx, t, pool, validated)
			if err := store.Release(ctx, validated); err != nil {
				t.Fatalf("release: %v", err)
			}
			successor, err := store.TryAcquire(ctx, key, "successor", 5*time.Second)
			if err != nil || successor.Token <= owned.Token {
				t.Fatalf("reacquire = %+v, %v", successor, err)
			}
			assertPostgresEpochs(ctx, t, pool, successor)
			if err := store.Release(ctx, successor); err != nil {
				t.Fatalf("release successor: %v", err)
			}
		})
	}
}

func assertPostgresEpochs(ctx context.Context, t *testing.T, pool *pgxpool.Pool, record lease.Record) {
	t.Helper()
	var acquired, expires int64
	// Numeric epochs provide an oracle independent of pgx's timestamptz decoder.
	err := pool.QueryRow(ctx, `SELECT
    (extract(epoch from acquired_at) * 1000000)::bigint,
    (extract(epoch from expires_at) * 1000000)::bigint
FROM lease_records WHERE owner = $1 AND fencing_token = $2`, record.Owner, record.Token).
		Scan(&acquired, &expires)
	if err != nil {
		t.Fatalf("read persisted epochs: %v", err)
	}
	if record.AcquiredAt.Location() != time.UTC || record.ExpiresAt.Location() != time.UTC ||
		record.AcquiredAt.UnixMicro() != acquired || record.ExpiresAt.UnixMicro() != expires ||
		!record.ExpiresAt.After(record.AcquiredAt) {
		t.Fatalf("record timestamps = %+v, persisted microseconds = %d, %d", record, acquired, expires)
	}
}
