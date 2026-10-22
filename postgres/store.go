// Package postgres implements fenced leases through atomic PostgreSQL statements
// and server time. New borrows a pgx pool; the caller installs SchemaMigration,
// chooses trusted search_path/permissions and owns pool shutdown, authentication
// and durable replication. Fence continuity depends on retained lease_fences
// rows, including across restore/failover; the backend is not a consensus service.
// TTL uses whole milliseconds, and no cgo is introduced by this adapter.
// See docs/backend-guarantees.md and docs/quickstart-postgres.md.
package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	lease "github.com/faustbrian/go-lease/v3"
	"github.com/faustbrian/go-lease/v3/internal/failure"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Increment a retained fence row in the same statement transaction as attempted
// owner installation. Contention may commit a consumed fence, so successful
// tokens need not be consecutive; rollback reverts the increment (backend tests).
const acquireSQL = `
WITH next_fence AS (
    INSERT INTO lease_fences AS fences (key_digest, fencing_token)
    VALUES ($1, 1)
    ON CONFLICT (key_digest) DO UPDATE SET
        fencing_token = fences.fencing_token + 1
    WHERE fences.fencing_token < 9223372036854775807
    RETURNING fencing_token
), installed AS (
INSERT INTO lease_records AS leases (
    key_digest, owner, fencing_token, acquired_at, expires_at, active, updated_at
)
SELECT $1, $2, next_fence.fencing_token, clock_timestamp(),
       clock_timestamp() + ($3 * interval '1 millisecond'), true,
       clock_timestamp()
FROM next_fence
ON CONFLICT (key_digest) DO UPDATE SET
    owner = EXCLUDED.owner,
    fencing_token = EXCLUDED.fencing_token,
    acquired_at = clock_timestamp(),
    expires_at = clock_timestamp() + ($3 * interval '1 millisecond'),
    active = true,
    updated_at = clock_timestamp()
WHERE NOT leases.active OR leases.expires_at <= clock_timestamp()
RETURNING fencing_token, acquired_at, expires_at
)
SELECT fencing_token, acquired_at, expires_at, 'ok' FROM installed
UNION ALL
SELECT 0, clock_timestamp(), clock_timestamp(),
       CASE WHEN EXISTS (
           SELECT 1 FROM lease_fences
           WHERE key_digest = $1 AND fencing_token >= 9223372036854775807
       ) THEN 'exhausted' ELSE 'contended' END
WHERE NOT EXISTS (SELECT 1 FROM installed)`

const renewSQL = `
UPDATE lease_records SET
    expires_at = clock_timestamp() + ($4 * interval '1 millisecond'),
    updated_at = clock_timestamp()
WHERE key_digest = $1 AND owner = $2 AND fencing_token = $3
  AND active AND expires_at > clock_timestamp()
RETURNING fencing_token, acquired_at, expires_at`

const validateSQL = `
SELECT fencing_token, acquired_at, expires_at
FROM lease_records
WHERE key_digest = $1 AND owner = $2 AND fencing_token = $3
  AND active AND expires_at > clock_timestamp()`

const releaseSQL = `
WITH changed AS (
    UPDATE lease_records SET active = false, updated_at = clock_timestamp()
    WHERE key_digest = $1 AND owner = $2 AND fencing_token = $3 AND active
    RETURNING 1
), same_release AS (
    SELECT 1 FROM lease_records
    WHERE key_digest = $1 AND owner = $2 AND fencing_token = $3 AND NOT active
), existing AS (
    SELECT 1 FROM lease_records WHERE key_digest = $1
)
SELECT CASE
    WHEN EXISTS (SELECT 1 FROM changed) THEN 'ok'
    WHEN EXISTS (SELECT 1 FROM same_release) THEN 'idempotent'
    WHEN NOT EXISTS (SELECT 1 FROM existing) THEN 'idempotent'
    ELSE 'stale'
END`

const cleanupSQL = `
WITH doomed AS (
    SELECT key_digest FROM lease_records
    WHERE NOT active AND updated_at < clock_timestamp() - interval '1 hour'
    ORDER BY updated_at LIMIT $1 FOR UPDATE SKIP LOCKED
), deleted AS (
    DELETE FROM lease_records AS records USING doomed
    WHERE records.key_digest = doomed.key_digest RETURNING 1
)
SELECT count(*) FROM deleted`

type database interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Store borrows a database/pool and performs statements in its configured schema
// context. New does not check connectivity or install tables; its zero value is
// unusable. Concurrent use follows the pool contract. Direct calls need nonnil
// bounded contexts; the lease Client adds operation timeouts but cannot prevent
// effects committed before cancellation or a lost response.
type Store struct{ database database }

// New rejects nil pool and borrows it without connecting, migrating, validating
// search_path or changing transaction/durability settings. The creator closes it.
func New(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, lease.Wrap(lease.ErrInvalidState, "nil postgres pool")
	}
	return newStore(pool)
}

func newStore(database database) (*Store, error) {
	if database == nil {
		return nil, lease.Wrap(lease.ErrInvalidState, "nil postgres database")
	}
	return &Store{database: database}, nil
}

// TryAcquire atomically attempts owner installation with a retained signed-bigint
// fence. Contention can consume a fence; successful tokens need not be consecutive.
// TTL must be at least one millisecond and is truncated to whole milliseconds.
// Query/response failure after a possible mutation is ErrAmbiguousOutcome; the
// formatted native error hides driver text but traversal retains its cause.
func (store *Store) TryAcquire(
	ctx context.Context,
	key lease.Key,
	owner string,
	ttl time.Duration,
) (lease.Record, error) {
	if err := validate(ctx, key, owner, 1, ttl); err != nil {
		return lease.Record{}, err
	}
	var token int64
	var acquiredAt, expiresAt time.Time
	var outcome string
	err := store.database.QueryRow(
		ctx, acquireSQL, digest(key), owner, ttl.Milliseconds(),
	).Scan(&token, &acquiredAt, &expiresAt, &outcome)
	if err != nil {
		return lease.Record{}, classify(ctx, err, true)
	}
	switch outcome {
	case "contended":
		return lease.Record{}, lease.Wrap(lease.ErrContended, "postgres acquire")
	case "exhausted":
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "postgres fence exhausted")
	case "ok":
		record, recordErr := recordFromValues(key, owner, token, acquiredAt, expiresAt)
		if recordErr != nil {
			return lease.Record{}, classify(ctx, recordErr, true)
		}
		return record, nil
	default:
		return lease.Record{}, classify(
			ctx, lease.Wrap(lease.ErrBackendUnavailable, "postgres acquire response"), true,
		)
	}
}

// Renew compares active owner/token and server-clock expiry before extending
// it; acquisition time/token remain unchanged. TTL must be at least 1ms and
// fractional milliseconds are truncated. No matching row is ErrStaleOwner;
// possible mutation/query failure is ambiguous, not automatically replayable.
func (store *Store) Renew(
	ctx context.Context,
	owned lease.Record,
	ttl time.Duration,
) (lease.Record, error) {
	if err := validate(ctx, owned.Key, owned.Owner, owned.Token, ttl); err != nil {
		return lease.Record{}, err
	}
	record, err := scanRecord(owned.Key, owned.Owner, store.database.QueryRow(
		// #nosec G115 -- validate rejects tokens above PostgreSQL bigint.
		ctx, renewSQL, digest(owned.Key), owned.Owner, int64(owned.Token), ttl.Milliseconds(),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return lease.Record{}, lease.Wrap(lease.ErrStaleOwner, "postgres renew")
	}
	if err != nil {
		return lease.Record{}, classify(ctx, err, true)
	}
	if record.Token != owned.Token {
		return lease.Record{}, lease.Wrap(lease.ErrAmbiguousOutcome, "postgres renew response")
	}
	return record, nil
}

// Validate reads matching active owner/token before backend expiry, without
// extending it or reserving a later effect. No row is ErrStaleOwner; read failures
// are canceled/unavailable classifications, not a successful ownership proof.
// Retained driver causes are programmatically accessible despite safe rendering.
func (store *Store) Validate(ctx context.Context, owned lease.Record) (lease.Record, error) {
	if err := validate(ctx, owned.Key, owned.Owner, owned.Token, time.Millisecond); err != nil {
		return lease.Record{}, err
	}
	record, err := scanRecord(owned.Key, owned.Owner, store.database.QueryRow(
		// #nosec G115 -- validate rejects tokens above PostgreSQL bigint.
		ctx, validateSQL, digest(owned.Key), owned.Owner, int64(owned.Token),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return lease.Record{}, lease.Wrap(lease.ErrStaleOwner, "postgres validate")
	}
	if err != nil {
		return lease.Record{}, classify(ctx, err, false)
	}
	if record.Token != owned.Token {
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "postgres validate response")
	}
	return record, nil
}

// Release deactivates matching active owner/token even after expiry. Same-owner
// repeated release and a missing row succeed; a successor is ErrStaleOwner.
// Fence rows are never removed. An ambiguous response can follow a committed
// release, so lack of acknowledgement does not prove the lease remains active.
func (store *Store) Release(ctx context.Context, owned lease.Record) error {
	if err := validate(ctx, owned.Key, owned.Owner, owned.Token, time.Millisecond); err != nil {
		return err
	}
	var outcome string
	err := store.database.QueryRow(
		// #nosec G115 -- validate rejects tokens above PostgreSQL bigint.
		ctx, releaseSQL, digest(owned.Key), owned.Owner, int64(owned.Token),
	).Scan(&outcome)
	if err != nil {
		return classify(ctx, err, true)
	}
	switch outcome {
	case "ok", "idempotent":
		return nil
	case "stale":
		return lease.Wrap(lease.ErrStaleOwner, "postgres release")
	default:
		return lease.Wrap(lease.ErrBackendUnavailable, "postgres response")
	}
}

// Cleanup removes a bounded batch of old inactive lease rows. Fence rows are
// deliberately retained so cleanup cannot reset monotonic continuity. Only
// inactive rows older than one hour are selected; expired-but-active rows remain.
// Batch is 1..10000, locks are skipped, and zero is an ordinary empty result.
// A query failure can follow deletion and returns ambiguity rather than a count.
func (store *Store) Cleanup(ctx context.Context, batch uint32) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, lease.Wrap(lease.ErrCanceled, "postgres cleanup")
	}
	if batch == 0 || batch > 10_000 {
		return 0, lease.Wrap(lease.ErrInvalidState, "postgres cleanup")
	}
	var count int64
	if err := store.database.QueryRow(ctx, cleanupSQL, int(batch)).Scan(&count); err != nil {
		return 0, classify(ctx, err, true)
	}
	if count < 0 || count > int64(batch) {
		return 0, lease.Wrap(lease.ErrBackendUnavailable, "postgres cleanup response")
	}
	return uint32(count), nil
}

func scanRecord(key lease.Key, owner string, row pgx.Row) (lease.Record, error) {
	var token int64
	var acquiredAt, expiresAt time.Time
	if err := row.Scan(&token, &acquiredAt, &expiresAt); err != nil {
		return lease.Record{}, err
	}
	return recordFromValues(key, owner, token, acquiredAt, expiresAt)
}

func recordFromValues(
	key lease.Key,
	owner string,
	token int64,
	acquiredAt time.Time,
	expiresAt time.Time,
) (lease.Record, error) {
	if token <= 0 || !expiresAt.After(acquiredAt) {
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "postgres response")
	}
	return lease.Record{
		Key: key, Owner: owner, Token: lease.Token(token),
		AcquiredAt: acquiredAt.UTC(), ExpiresAt: expiresAt.UTC(),
	}, nil
}

func digest(key lease.Key) []byte {
	sum := sha256.Sum256([]byte(key.String()))
	return sum[:]
}

func validate(
	ctx context.Context,
	key lease.Key,
	owner string,
	token lease.Token,
	ttl time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return lease.Wrap(lease.ErrCanceled, "postgres context")
	}
	if key.String() == "" {
		return lease.Wrap(lease.ErrInvalidState, "postgres input")
	}
	if owner == "" {
		return lease.Wrap(lease.ErrInvalidState, "postgres input")
	}
	if len(owner) > 128 {
		return lease.Wrap(lease.ErrInvalidState, "postgres input")
	}
	if token == 0 {
		return lease.Wrap(lease.ErrInvalidState, "postgres input")
	}
	if token > lease.Token(9223372036854775807) {
		return lease.Wrap(lease.ErrInvalidState, "postgres input")
	}
	if ttl < time.Millisecond {
		return lease.Wrap(lease.ErrInvalidState, "postgres input")
	}
	return nil
}

func classify(ctx context.Context, err error, ambiguous bool) error {
	if ambiguous {
		return failure.Wrap(lease.ErrAmbiguousOutcome, err, "postgres operation")
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return lease.Wrap(lease.ErrCanceled, "postgres operation")
	}
	return failure.Wrap(lease.ErrBackendUnavailable, err, "postgres operation")
}
