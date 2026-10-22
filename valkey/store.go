// Package valkey implements fenced leases with native Valkey Lua operations and
// server time. New borrows a client; Open additionally inspects version >=9 and
// noeviction policy once. The creator owns client closure, TLS/ACLs/timeouts and
// durability. Same-slot lease/counter keys use a stable truncated SHA-256 digest;
// retained counters define continuity, not failover/restore guarantees or secrecy.
// Guard supplies coordinates for a same-deployment atomic protected writer,
// not an active-ownership proof. TTL uses milliseconds and no cgo is introduced.
// See docs/backend-guarantees.md and docs/protected-writes.md.
package valkey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	lease "github.com/faustbrian/go-lease/v3"
	"github.com/faustbrian/go-lease/v3/internal/failure"
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

// Store borrows a native executor/client and immutable validated prefix. Its
// zero value is unusable; concurrent use follows the native client contract.
// Direct operations require nonnil bounded contexts; mutation failures may be
// ambiguous after transmission. Counter deletion/eviction/restore loses fencing
// continuity even though lease hashes normally expire separately.
type Store struct {
	executor executor
	prefix   string
}

// Guard holds opaque storage coordinates for a compatible atomic protected
// writer. Construction does not inspect backend state, expiry or caller authority;
// a copied or retained Guard can already be stale. The writer must compare the
// active lease key/owner/token at the effect boundary in the same deployment.
type Guard struct {
	storageKey string
	owner      string
	token      string
}

// StorageKey returns the configured prefix and same-slot hashed lease key.
// Hashing avoids literal key exposure but is unkeyed, stable and guessable;
// it is not encryption or permission to disclose an arbitrary prefix.
func (guard Guard) StorageKey() string { return guard.storageKey }

// Owner returns the lease owner compared by a protected write.
func (guard Guard) Owner() string { return guard.owner }

// Token returns the exact decimal fencing token compared by a protected write.
func (guard Guard) Token() string { return guard.token }

func newStore(executor executor, prefix string) (*Store, error) {
	if executor == nil {
		return nil, lease.Wrap(lease.ErrInvalidState, "valkey options")
	}
	if prefix == "" {
		return nil, lease.Wrap(lease.ErrInvalidState, "valkey options")
	}
	if len(prefix) > 64 {
		return nil, lease.Wrap(lease.ErrInvalidState, "valkey options")
	}
	if strings.ContainsAny(prefix, "{}\x00\r\n ") {
		return nil, lease.Wrap(lease.ErrInvalidState, "valkey options")
	}
	return &Store{executor: executor, prefix: prefix}, nil
}

// Guard derives the backend coordinates required to validate owned atomically.
// The protected writer must share this Store's deployment, configured prefix,
// key derivation and continuity epoch. Only nonempty key/owner, owner length
// and nonzero token are checked; freely constructed Record values are not
// authenticated here. The writer checks active ownership at the effect boundary,
// not during Guard construction.
func (store *Store) Guard(owned lease.Record) (Guard, error) {
	if owned.Key.String() == "" {
		return Guard{}, lease.Wrap(lease.ErrInvalidState, "valkey guard")
	}
	if owned.Owner == "" {
		return Guard{}, lease.Wrap(lease.ErrInvalidState, "valkey guard")
	}
	if len(owned.Owner) > 128 {
		return Guard{}, lease.Wrap(lease.ErrInvalidState, "valkey guard")
	}
	if owned.Token == 0 {
		return Guard{}, lease.Wrap(lease.ErrInvalidState, "valkey guard")
	}
	return Guard{
		storageKey: store.keys(owned.Key)[0],
		owner:      owned.Owner,
		token:      strconv.FormatUint(uint64(owned.Token), 10),
	}, nil
}

// TryAcquire acquires only an absent lease hash, using server TIME and a retained
// signed-counter key; live ownership reports contention even for the same owner.
// TTL is at least 1ms and fractional milliseconds are truncated. Transport errors
// may follow mutation and are ambiguous; native cause text is hidden only from
// ordinary formatting, not programmatic error traversal.
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

// Renew compares the existing lease hash owner/token and refreshes expiry from
// server time without changing acquisition/token values. TTL is at least 1ms,
// with fractional milliseconds truncated. Missing or mismatching ownership is
// stale; transport errors can follow extension and require reconciliation.
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

// Validate compares owner/token on the server's nonexpired lease hash, without
// extending it or reserving a later effect. Missing/mismatched ownership is stale;
// read errors are canceled/unavailable. A retained record or Guard is not a
// substitute for this check or the protected writer's atomic effect check.
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

// Release deletes only the matching owner/token lease hash, leaving its counter
// intact. Missing ownership is idempotent success; a successor is ErrStaleOwner.
// A lost response can follow deletion and returns ErrAmbiguousOutcome, not proof
// of absence or a reason to replay an unprotected application effect.
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
	// Both keys share the same cluster tag so lease/counter Lua mutation is atomic.
	// Preserve this derivation/prefix within a continuity epoch; hashes hide literal
	// names, not known-key guesses or administration-level backend access.
	sum := sha256.Sum256([]byte(key.String()))
	digest := hex.EncodeToString(sum[:16])
	tag := "{" + digest + "}"
	return []string{store.prefix + ":" + tag + ":lease", store.prefix + ":" + tag + ":counter"}
}

func hashTag(key string) string {
	start := strings.IndexByte(key, '{')
	if start == -1 {
		return ""
	}
	end := strings.IndexByte(key[start+1:], '}')
	if end == -1 {
		return ""
	}
	return key[start+1 : start+1+end]
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
	if tokenErr != nil {
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "valkey response")
	}
	if acquiredErr != nil {
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "valkey response")
	}
	if expiresErr != nil {
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "valkey response")
	}
	if token == 0 {
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "valkey response")
	}
	if expires <= acquired {
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
	if key.String() == "" {
		return lease.Wrap(lease.ErrInvalidState, "valkey input")
	}
	if owner == "" {
		return lease.Wrap(lease.ErrInvalidState, "valkey input")
	}
	if len(owner) > 128 {
		return lease.Wrap(lease.ErrInvalidState, "valkey input")
	}
	if token == 0 {
		return lease.Wrap(lease.ErrInvalidState, "valkey input")
	}
	if ttl < time.Millisecond {
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
