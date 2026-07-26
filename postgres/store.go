// Package postgres provides native durable fenced leases for PostgreSQL.
package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/internal/failure"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

// Store persists leases and fencing counters in PostgreSQL server time.
type Store struct{ database database }

// New constructs a PostgreSQL backend from a caller-owned pgx pool.
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

// TryAcquire atomically advances the durable fence and installs an owner.
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

// Renew atomically compares owner and token before extending backend expiry.
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

// Validate checks current owner, token, active state, and backend deadline.
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

// Release atomically deactivates only a matching owner and token.
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
// deliberately retained so cleanup cannot reset monotonic continuity.
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
	if key.String() == "" || owner == "" || len(owner) > 128 || token == 0 ||
		token > lease.Token(9223372036854775807) || ttl <= 0 || ttl.Milliseconds() <= 0 {
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
