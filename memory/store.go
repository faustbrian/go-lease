// Package memory provides a deterministic process-local reference backend.
// It is not a distributed coordination backend.
package memory

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
)

// Options bounds and configures the process-local reference backend.
type Options struct {
	Clock   lease.Clock
	MaxKeys uint32
}

type entry struct {
	record lease.Record
	active bool
}

// Store is a deterministic, concurrency-safe reference backend.
type Store struct {
	mu      sync.Mutex
	clock   lease.Clock
	maxKeys uint32
	entries map[string]entry
}

// New constructs a bounded process-local store.
func New(options Options) (*Store, error) {
	if options.Clock == nil || options.MaxKeys == 0 {
		return nil, fmt.Errorf("%w: invalid memory options", lease.ErrInvalidState)
	}
	return &Store{
		clock: options.Clock, maxKeys: options.MaxKeys,
		entries: make(map[string]entry),
	}, nil
}

// TryAcquire acquires an absent, released, or expired lease.
func (store *Store) TryAcquire(
	ctx context.Context,
	key lease.Key,
	owner string,
	ttl time.Duration,
) (lease.Record, error) {
	if err := validate(ctx, key, owner, ttl); err != nil {
		return lease.Record{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.clock.Now()
	current, exists := store.entries[key.String()]
	if exists && current.active && now.Before(current.record.ExpiresAt) {
		return lease.Record{}, lease.Wrap(lease.ErrContended, "try acquire")
	}
	// #nosec G115 -- map length cannot exceed the uint32 configured capacity.
	if !exists && uint32(len(store.entries)) >= store.maxKeys {
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "key capacity")
	}
	if current.record.Token == lease.Token(math.MaxUint64) {
		return lease.Record{}, lease.Wrap(lease.ErrBackendUnavailable, "token exhausted")
	}
	record := lease.Record{
		Key: key, Owner: owner, Token: current.record.Token + 1,
		AcquiredAt: now, ExpiresAt: now.Add(ttl),
	}
	store.entries[key.String()] = entry{record: record, active: true}
	return record, nil
}

// Renew extends a lease only while owner, token, and deadline remain current.
func (store *Store) Renew(
	ctx context.Context,
	owned lease.Record,
	ttl time.Duration,
) (lease.Record, error) {
	if err := validate(ctx, owned.Key, owned.Owner, ttl); err != nil || owned.Token == 0 {
		if err != nil {
			return lease.Record{}, err
		}
		return lease.Record{}, lease.Wrap(lease.ErrInvalidState, "renew")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.entries[owned.Key.String()]
	now := store.clock.Now()
	if !exists || !current.active || !sameOwner(current.record, owned) ||
		!now.Before(current.record.ExpiresAt) {
		return lease.Record{}, lease.Wrap(lease.ErrStaleOwner, "renew")
	}
	current.record.ExpiresAt = now.Add(ttl)
	store.entries[owned.Key.String()] = current
	return current.record, nil
}

// Validate proves that owner and token are current and unexpired.
func (store *Store) Validate(ctx context.Context, owned lease.Record) (lease.Record, error) {
	if err := validateIdentity(ctx, owned.Key, owned.Owner, owned.Token); err != nil {
		return lease.Record{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.entries[owned.Key.String()]
	if !exists || !current.active || !sameOwner(current.record, owned) ||
		!store.clock.Now().Before(current.record.ExpiresAt) {
		return lease.Record{}, lease.Wrap(lease.ErrStaleOwner, "validate")
	}
	return current.record, nil
}

// Release idempotently deactivates only the matching owner and token.
func (store *Store) Release(ctx context.Context, owned lease.Record) error {
	if err := validateIdentity(ctx, owned.Key, owned.Owner, owned.Token); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.entries[owned.Key.String()]
	if exists && !current.active && sameOwner(current.record, owned) {
		return nil
	}
	if !exists || !current.active || !sameOwner(current.record, owned) {
		return lease.Wrap(lease.ErrStaleOwner, "release")
	}
	current.active = false
	store.entries[owned.Key.String()] = current
	return nil
}

func validate(ctx context.Context, key lease.Key, owner string, ttl time.Duration) error {
	if err := validateIdentity(ctx, key, owner, 1); err != nil {
		return err
	}
	if ttl <= 0 {
		return lease.Wrap(lease.ErrInvalidState, "duration")
	}
	return nil
}

func validateIdentity(ctx context.Context, key lease.Key, owner string, token lease.Token) error {
	if err := ctx.Err(); err != nil {
		return lease.Wrap(lease.ErrCanceled, "context")
	}
	if key.String() == "" || owner == "" || len(owner) > 128 || token == 0 {
		return lease.Wrap(lease.ErrInvalidState, "identity")
	}
	return nil
}

func sameOwner(current, candidate lease.Record) bool {
	return current.Owner == candidate.Owner && current.Token == candidate.Token
}
