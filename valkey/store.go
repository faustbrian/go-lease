// Package valkey provides native atomic fenced leases for Valkey.
package valkey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	lease "github.com/faustbrian/go-lease"
	"github.com/faustbrian/go-lease/internal/failure"
)

type operation uint8

const (
	opAcquire operation = iota + 1
	opRenew
	opValidate
	opRelease
)

type executor interface {
	Exec(context.Context, operation, []string, []string) ([]string, error)
}

// Store persists atomic leases using backend-time Lua operations.
type Store struct {
	executor executor
	prefix   string
}

func newStore(executor executor, prefix string) (*Store, error) {
	if executor == nil || prefix == "" || len(prefix) > 64 ||
		strings.ContainsAny(prefix, "{}\x00\r\n ") {
		return nil, lease.Wrap(lease.ErrInvalidState, "valkey options")
	}
	return &Store{executor: executor, prefix: prefix}, nil
}

// TryAcquire atomically increments the persistent counter and creates a lease.
func (store *Store) TryAcquire(
	ctx context.Context,
	key lease.Key,
	owner string,
	ttl time.Duration,
) (lease.Record, error) {
	if err := validate(ctx, key, owner, 1, ttl); err != nil {
		return lease.Record{}, err
	}
	reply, err := store.executor.Exec(ctx, opAcquire, store.keys(key), []string{
		owner, strconv.FormatInt(ttl.Milliseconds(), 10),
	})
	if err != nil {
		return lease.Record{}, classify(ctx, err, true)
	}
	return parseRecord(key, owner, reply, lease.ErrContended)
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
	reply, err := store.executor.Exec(ctx, opRenew, store.keys(owned.Key), []string{
		owned.Owner, strconv.FormatUint(uint64(owned.Token), 10),
		strconv.FormatInt(ttl.Milliseconds(), 10),
	})
	if err != nil {
		return lease.Record{}, classify(ctx, err, true)
	}
	record, err := parseRecord(owned.Key, owned.Owner, reply, lease.ErrStaleOwner)
	if err != nil {
		return lease.Record{}, err
	}
	if record.Token != owned.Token {
		return lease.Record{}, lease.Wrap(lease.ErrAmbiguousOutcome, "valkey renew response")
	}
	return record, nil
}

// Validate atomically compares owner and token against an unexpired lease.
func (store *Store) Validate(ctx context.Context, owned lease.Record) (lease.Record, error) {
	if err := validate(ctx, owned.Key, owned.Owner, owned.Token, time.Millisecond); err != nil {
		return lease.Record{}, err
	}
	reply, err := store.executor.Exec(ctx, opValidate, store.keys(owned.Key), []string{
		owned.Owner, strconv.FormatUint(uint64(owned.Token), 10),
	})
	if err != nil {
		return lease.Record{}, classify(ctx, err, false)
	}
	record, err := parseRecord(owned.Key, owned.Owner, reply, lease.ErrStaleOwner)
	if err != nil {
		return lease.Record{}, err
	}
	if record.Token != owned.Token {
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "valkey validate response")
	}
	return record, nil
}

// Release atomically deletes only a matching lease; missing is idempotent.
func (store *Store) Release(ctx context.Context, owned lease.Record) error {
	if err := validate(ctx, owned.Key, owned.Owner, owned.Token, time.Millisecond); err != nil {
		return err
	}
	reply, err := store.executor.Exec(ctx, opRelease, store.keys(owned.Key), []string{
		owned.Owner, strconv.FormatUint(uint64(owned.Token), 10),
	})
	if err != nil {
		return classify(ctx, err, true)
	}
	if len(reply) != 1 {
		return lease.Wrap(lease.ErrBackendUnavailable, "valkey response")
	}
	switch reply[0] {
	case "ok", "missing":
		return nil
	case "stale":
		return lease.Wrap(lease.ErrStaleOwner, "valkey release")
	default:
		return lease.Wrap(lease.ErrBackendUnavailable, "valkey response")
	}
}

func (store *Store) keys(key lease.Key) []string {
	sum := sha256.Sum256([]byte(key.String()))
	digest := hex.EncodeToString(sum[:16])
	tag := "{" + digest + "}"
	return []string{store.prefix + ":" + tag + ":lease", store.prefix + ":" + tag + ":counter"}
}

func hashTag(key string) string {
	start := strings.IndexByte(key, '{')
	end := strings.IndexByte(key, '}')
	if start < 0 || end <= start {
		return ""
	}
	return key[start+1 : end]
}

func parseRecord(key lease.Key, owner string, reply []string, expected error) (lease.Record, error) {
	if len(reply) == 1 && reply[0] != "ok" {
		return lease.Record{}, lease.Wrap(expected, "valkey operation")
	}
	if len(reply) != 4 || reply[0] != "ok" {
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "valkey response")
	}
	token, tokenErr := strconv.ParseUint(reply[1], 10, 64)
	acquired, acquiredErr := strconv.ParseInt(reply[2], 10, 64)
	expires, expiresErr := strconv.ParseInt(reply[3], 10, 64)
	if tokenErr != nil || acquiredErr != nil || expiresErr != nil || token == 0 || expires <= acquired {
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "valkey response")
	}
	return lease.Record{
		Key: key, Owner: owner, Token: lease.Token(token),
		AcquiredAt: time.UnixMilli(acquired).UTC(),
		ExpiresAt:  time.UnixMilli(expires).UTC(),
	}, nil
}

func validate(ctx context.Context, key lease.Key, owner string, token lease.Token, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return lease.Wrap(lease.ErrCanceled, "valkey context")
	}
	if key.String() == "" || owner == "" || len(owner) > 128 || token == 0 ||
		ttl <= 0 || ttl.Milliseconds() <= 0 {
		return lease.Wrap(lease.ErrInvalidState, "valkey input")
	}
	return nil
}

func classify(ctx context.Context, err error, ambiguous bool) error {
	if ambiguous {
		return failure.Wrap(lease.ErrAmbiguousOutcome, err, "valkey operation")
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return lease.Wrap(lease.ErrCanceled, "valkey operation")
	}
	return failure.Wrap(lease.ErrBackendUnavailable, err, "valkey operation")
}
