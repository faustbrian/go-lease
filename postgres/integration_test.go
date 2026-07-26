package postgres_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/leasetest"
	leasepostgres "github.com/faustbrian/golib/pkg/lease/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLiveBackendConformance(t *testing.T) {
	url := os.Getenv("POSTGRES_URL")
	if url == "" {
		t.Skip("POSTGRES_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	migration := leasepostgres.SchemaMigration()
	if _, err := pool.Exec(ctx, migration.Down); err != nil {
		// A clean database has no tables yet.
		t.Logf("initial schema cleanup: %v", err)
	}
	if _, err := pool.Exec(ctx, migration.Up); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupContext, migration.Down); err != nil {
			t.Errorf("drop migration: %v", err)
		}
	})
	store, err := leasepostgres.New(pool)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	leasetest.RunBackendConformance(t, func(*testing.T) leasetest.BackendFixture {
		return leasetest.BackendFixture{Backend: store, Expire: time.Sleep}
	})
}

func TestLivePartitionOutcome(t *testing.T) {
	url := os.Getenv("POSTGRES_PARTITION_URL")
	if url == "" {
		t.Skip("POSTGRES_PARTITION_URL is not set")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	config.ConnConfig.ConnectTimeout = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	store, err := leasepostgres.New(pool)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	key, _ := lease.NewKey("integration", "partition")
	if _, err := store.TryAcquire(ctx, key, "owner", time.Second); !errors.Is(err, lease.ErrAmbiguousOutcome) {
		t.Fatalf("TryAcquire(partition) error = %v", err)
	}
}

func TestLiveOperationalFaults(t *testing.T) {
	url := os.Getenv("POSTGRES_FAULT_URL")
	if url == "" {
		t.Skip("POSTGRES_FAULT_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	migration := leasepostgres.SchemaMigration()
	pool := openPool(ctx, t, url)
	_, _ = pool.Exec(ctx, migration.Down)
	if _, err := pool.Exec(ctx, migration.Up); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	defer func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupPool := openPool(cleanupContext, t, url)
		defer cleanupPool.Close()
		if _, err := cleanupPool.Exec(cleanupContext, migration.Down); err != nil {
			t.Errorf("drop migration: %v", err)
		}
	}()

	store, _ := leasepostgres.New(pool)
	key, _ := lease.NewKey("fault", "postgres-operations")
	first := acquireAndRelease(ctx, t, store, key, "owner-1")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin aborted transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE lease_fences SET fencing_token = fencing_token + 100"); err != nil {
		t.Fatalf("mutate aborted transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT 1 / 0"); err == nil {
		t.Fatal("transaction abort did not fail")
	}
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("rollback aborted transaction: %v", err)
	}
	second := acquireAndRelease(ctx, t, store, key, "owner-2")
	if second.Token != first.Token+1 {
		t.Fatalf("token after aborted transaction = %d, want %d", second.Token, first.Token+1)
	}

	deadlockA, _ := lease.NewKey("fault", "deadlock-a")
	deadlockB, _ := lease.NewKey("fault", "deadlock-b")
	acquireAndRelease(ctx, t, store, deadlockA, "deadlock-a")
	acquireAndRelease(ctx, t, store, deadlockB, "deadlock-b")
	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin deadlock transaction A: %v", err)
	}
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin deadlock transaction B: %v", err)
	}
	lockFence(ctx, t, txA, "deadlock-a")
	lockFence(ctx, t, txB, "deadlock-b")
	errorsA := make(chan error, 1)
	errorsB := make(chan error, 1)
	go func() { errorsA <- updateFence(ctx, txA, "deadlock-b") }()
	go func() { errorsB <- updateFence(ctx, txB, "deadlock-a") }()
	errA := <-errorsA
	errB := <-errorsB
	_ = txA.Rollback(ctx)
	_ = txB.Rollback(ctx)
	if !isPostgresCode(errA, "40P01") && !isPostgresCode(errB, "40P01") {
		t.Fatalf("deadlock errors = %v and %v, want SQLSTATE 40P01", errA, errB)
	}

	third := acquireAndRelease(ctx, t, store, key, "owner-3")
	if _, err := pool.Exec(ctx, `UPDATE lease_records
SET updated_at = clock_timestamp() - interval '2 hours'
WHERE owner = $1`, third.Owner); err != nil {
		t.Fatalf("age released record: %v", err)
	}
	cleanupResult := make(chan error, 1)
	acquireResult := make(chan struct {
		record lease.Record
		err    error
	}, 1)
	go func() {
		_, cleanupErr := store.Cleanup(ctx, 10)
		cleanupResult <- cleanupErr
	}()
	go func() {
		record, acquireErr := store.TryAcquire(ctx, key, "successor", time.Second)
		acquireResult <- struct {
			record lease.Record
			err    error
		}{record: record, err: acquireErr}
	}()
	if err := <-cleanupResult; err != nil {
		t.Fatalf("Cleanup(race) error = %v", err)
	}
	successor := <-acquireResult
	if successor.err != nil || successor.record.Token <= third.Token {
		t.Fatalf("TryAcquire(cleanup race) = %+v, %v", successor.record, successor.err)
	}
	if err := store.Release(ctx, third); !errors.Is(err, lease.ErrStaleOwner) {
		t.Fatalf("Release(predecessor after cleanup race) error = %v", err)
	}
	if err := store.Release(ctx, successor.record); err != nil {
		t.Fatalf("Release(successor) error = %v", err)
	}

	if _, err := pool.Exec(ctx, "ALTER TABLE lease_records ADD COLUMN client_version integer"); err != nil {
		t.Fatalf("apply compatible rolling schema: %v", err)
	}
	compatible := acquireAndRelease(ctx, t, store, key, "compatible-client")
	if _, err := pool.Exec(ctx, "ALTER TABLE lease_records DROP COLUMN client_version"); err != nil {
		t.Fatalf("remove compatible rolling schema: %v", err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE lease_records RENAME COLUMN fencing_token TO incompatible_token"); err != nil {
		t.Fatalf("apply incompatible rolling schema: %v", err)
	}
	if _, err := store.TryAcquire(ctx, key, "incompatible-client", time.Second); !errors.Is(err, lease.ErrAmbiguousOutcome) {
		t.Fatalf("TryAcquire(incompatible schema) error = %v", err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE lease_records RENAME COLUMN incompatible_token TO fencing_token"); err != nil {
		t.Fatalf("restore compatible schema: %v", err)
	}

	pool.Close()
	last := compatible
	for index := 0; index < 4; index++ {
		pool = openPool(ctx, t, url)
		store, _ = leasepostgres.New(pool)
		next := acquireAndRelease(ctx, t, store, key, "pool-"+strconv.Itoa(index))
		if next.Token <= last.Token {
			t.Fatalf("pool churn token = %d, previous = %d", next.Token, last.Token)
		}
		last = next
		pool.Close()
	}

	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse serializable pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "serializable"
	pool, err = pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open serializable pool: %v", err)
	}
	defer pool.Close()
	store, _ = leasepostgres.New(pool)
	serializable := acquireAndRelease(ctx, t, store, key, "serializable-client")
	if serializable.Token <= last.Token {
		t.Fatalf("serializable token = %d, previous = %d", serializable.Token, last.Token)
	}
}

func openPool(ctx context.Context, t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	return pool
}

func acquireAndRelease(
	ctx context.Context,
	t *testing.T,
	store *leasepostgres.Store,
	key lease.Key,
	owner string,
) lease.Record {
	t.Helper()
	record, err := store.TryAcquire(ctx, key, owner, time.Second)
	if err != nil {
		t.Fatalf("TryAcquire(%s) error = %v", owner, err)
	}
	if err := store.Release(ctx, record); err != nil {
		t.Fatalf("Release(%s) error = %v", owner, err)
	}
	return record
}

func lockFence(ctx context.Context, t *testing.T, tx pgx.Tx, owner string) {
	t.Helper()
	if err := updateFence(ctx, tx, owner); err != nil {
		t.Fatalf("lock fence for %s: %v", owner, err)
	}
}

func updateFence(ctx context.Context, tx pgx.Tx, owner string) error {
	_, err := tx.Exec(ctx, `UPDATE lease_fences SET fencing_token = fencing_token
WHERE key_digest = (SELECT key_digest FROM lease_records WHERE owner = $1)`, owner)
	return err
}

func isPostgresCode(err error, code string) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == code
}

func TestLiveFenceContinuity(t *testing.T) {
	url := os.Getenv("POSTGRES_CONTINUITY_URL")
	phase := os.Getenv("POSTGRES_CONTINUITY_PHASE")
	if url == "" || phase == "" {
		t.Skip("PostgreSQL continuity phase is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	migration := leasepostgres.SchemaMigration()
	if phase == "seed" {
		_, _ = pool.Exec(ctx, migration.Down)
		if _, err := pool.Exec(ctx, migration.Up); err != nil {
			t.Fatalf("apply migration: %v", err)
		}
	}
	if phase == "reset" {
		if _, err := pool.Exec(ctx, "TRUNCATE lease_records, lease_fences CASCADE"); err != nil {
			t.Fatalf("reset continuity tables: %v", err)
		}
	}
	store, err := leasepostgres.New(pool)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	key, _ := lease.NewKey("fault", "continuity")
	record, err := store.TryAcquire(ctx, key, "owner-"+phase, time.Second)
	if err != nil {
		t.Fatalf("TryAcquire(%s) error = %v", phase, err)
	}
	switch phase {
	case "seed", "reset":
		if record.Token != 1 {
			t.Fatalf("%s token = %d, want 1", phase, record.Token)
		}
	case "verify":
		if record.Token <= 1 {
			t.Fatalf("verify token = %d, continuity reset", record.Token)
		}
	case "rollback":
		maximum, err := strconv.ParseUint(os.Getenv("POSTGRES_CONTINUITY_MAX_TOKEN"), 10, 64)
		if err != nil || record.Token > lease.Token(maximum) {
			t.Fatalf("rollback token = %d, protected maximum = %d", record.Token, maximum)
		}
	default:
		t.Fatalf("unknown continuity phase %q", phase)
	}
	if err := store.Release(ctx, record); err != nil {
		t.Fatalf("Release(%s) error = %v", phase, err)
	}
}
