package postgres

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewAndStoreRejectInvalidInputs(t *testing.T) {
	t.Parallel()

	if _, err := New(nil); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("New(nil) error = %v", err)
	}
	if _, err := newStore(nil); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("newStore(nil) error = %v", err)
	}
	pool, err := pgxpool.New(context.Background(), "postgres://localhost/unused?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if store, err := New(pool); err != nil || store == nil {
		t.Fatalf("New(pool) = %#v, %v", store, err)
	}

	store, _ := newStore(&fakeDatabase{})
	key, _ := lease.NewKey("postgres", "input")
	zero := lease.Record{Key: key, Owner: "owner"}
	if _, err := store.TryAcquire(context.Background(), key, "owner", 0); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("TryAcquire(ttl) error = %v", err)
	}
	if _, err := store.Renew(context.Background(), zero, time.Second); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Renew(token) error = %v", err)
	}
	valid := lease.Record{Key: key, Owner: "owner", Token: 1}
	if _, err := store.Renew(context.Background(), valid, 0); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Renew(ttl) error = %v", err)
	}
	if _, err := store.Validate(context.Background(), zero); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Validate(token) error = %v", err)
	}
	if err := store.Release(context.Background(), zero); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Release(token) error = %v", err)
	}
	overflow := lease.Record{Key: key, Owner: "owner", Token: lease.Token(math.MaxUint64)}
	if _, err := store.Validate(context.Background(), overflow); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Validate(overflow) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.TryAcquire(canceled, key, "owner", time.Second); !errors.Is(err, lease.ErrCanceled) {
		t.Fatalf("TryAcquire(canceled) error = %v", err)
	}
	if _, err := store.Renew(canceled, valid, time.Second); !errors.Is(err, lease.ErrCanceled) {
		t.Fatalf("Renew(canceled) error = %v", err)
	}
	if _, err := store.Validate(canceled, valid); !errors.Is(err, lease.ErrCanceled) {
		t.Fatalf("Validate(canceled) error = %v", err)
	}
}

func TestBackendFailuresAreFailClosed(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("backend")
	key, _ := lease.NewKey("postgres", "failure")
	record := lease.Record{Key: key, Owner: "owner", Token: 1}
	mutations := []func(*Store) error{
		func(store *Store) error {
			_, err := store.TryAcquire(context.Background(), key, "owner", time.Second)
			return err
		},
		func(store *Store) error { _, err := store.Renew(context.Background(), record, time.Second); return err },
		func(store *Store) error { return store.Release(context.Background(), record) },
	}
	for _, mutation := range mutations {
		store, _ := newStore(&fakeDatabase{rows: []pgx.Row{fakeRow{err: backendErr}}})
		if err := mutation(store); !errors.Is(err, lease.ErrAmbiguousOutcome) {
			t.Fatalf("mutation error = %v", err)
		}
	}
	store, _ := newStore(&fakeDatabase{rows: []pgx.Row{fakeRow{err: backendErr}}})
	if _, err := store.Validate(context.Background(), record); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("Validate(backend) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(classify(canceled, backendErr, true), lease.ErrAmbiguousOutcome) {
		t.Fatal("classify(canceled mutation) did not return ErrAmbiguousOutcome")
	}
	if !errors.Is(classify(canceled, backendErr, false), lease.ErrCanceled) {
		t.Fatal("classify(canceled read) did not return ErrCanceled")
	}
	secret := errors.New("postgres://secret-owner:secret-password@backend/lease")
	classified := classify(context.Background(), secret, true)
	if !errors.Is(classified, secret) || strings.Contains(classified.Error(), "secret") {
		t.Fatalf("classified error leaked backend detail: %v", classified)
	}
}

func TestRenewAndValidateReturnBackendRecord(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := lease.NewKey("postgres", "success")
	record := lease.Record{Key: key, Owner: "owner", Token: 2}
	database := &fakeDatabase{rows: []pgx.Row{
		fakeRow{values: []any{int64(2), now, now.Add(time.Second)}},
		fakeRow{values: []any{int64(2), now, now.Add(time.Second)}},
	}}
	store, _ := newStore(database)
	renewed, err := store.Renew(context.Background(), record, time.Second)
	if err != nil || renewed.Token != 2 {
		t.Fatalf("Renew() = %+v, %v", renewed, err)
	}
	validated, err := store.Validate(context.Background(), renewed)
	if err != nil || validated.Token != 2 {
		t.Fatalf("Validate() = %+v, %v", validated, err)
	}
}

func TestContinuationRejectsMismatchedBackendToken(t *testing.T) {
	t.Parallel()

	now := time.Now()
	key, _ := lease.NewKey("postgres", "mismatched-token")
	record := lease.Record{
		Key: key, Owner: "owner", Token: 2,
		AcquiredAt: now, ExpiresAt: now.Add(time.Second),
	}
	store, _ := newStore(&fakeDatabase{rows: []pgx.Row{
		fakeRow{values: []any{int64(3), now, now.Add(time.Second)}},
		fakeRow{values: []any{int64(3), now, now.Add(time.Second)}},
	}})
	if _, err := store.Renew(context.Background(), record, time.Second); !errors.Is(err, lease.ErrAmbiguousOutcome) {
		t.Fatalf("Renew(mismatched token) error = %v", err)
	}
	if _, err := store.Validate(context.Background(), record); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("Validate(mismatched token) error = %v", err)
	}
}

func TestCleanupIsBoundedAndValidatesResponse(t *testing.T) {
	t.Parallel()

	store, _ := newStore(&fakeDatabase{})
	if _, err := store.Cleanup(context.Background(), 0); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Cleanup(zero) error = %v", err)
	}
	if _, err := store.Cleanup(context.Background(), 10_001); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Cleanup(large) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Cleanup(canceled, 1); !errors.Is(err, lease.ErrCanceled) {
		t.Fatalf("Cleanup(canceled) error = %v", err)
	}
	store, _ = newStore(&fakeDatabase{rows: []pgx.Row{fakeRow{values: []any{int64(3)}}}})
	if count, err := store.Cleanup(context.Background(), 10); err != nil || count != 3 {
		t.Fatalf("Cleanup() = %d, %v", count, err)
	}
	for _, row := range []pgx.Row{fakeRow{values: []any{int64(-1)}}, fakeRow{values: []any{int64(11)}}} {
		store, _ = newStore(&fakeDatabase{rows: []pgx.Row{row}})
		if _, err := store.Cleanup(context.Background(), 10); !errors.Is(err, lease.ErrBackendUnavailable) {
			t.Fatalf("Cleanup(corrupt) error = %v", err)
		}
	}
	store, _ = newStore(&fakeDatabase{rows: []pgx.Row{fakeRow{err: errors.New("cleanup")}}})
	if _, err := store.Cleanup(context.Background(), 10); !errors.Is(err, lease.ErrAmbiguousOutcome) {
		t.Fatalf("Cleanup(backend) error = %v", err)
	}
}

func TestCorruptRecordsAndReleaseOutcomeAreRejected(t *testing.T) {
	t.Parallel()

	key, _ := lease.NewKey("postgres", "corrupt")
	now := time.Now()
	for _, values := range [][]any{
		{int64(0), now, now.Add(time.Second), "ok"},
		{int64(1), now, now, "ok"},
		{int64(1), now, now.Add(time.Second), "future"},
	} {
		store, _ := newStore(&fakeDatabase{rows: []pgx.Row{fakeRow{values: values}}})
		if _, err := store.TryAcquire(context.Background(), key, "owner", time.Second); !errors.Is(err, lease.ErrAmbiguousOutcome) {
			t.Fatalf("TryAcquire(corrupt) error = %v", err)
		}
	}
	record := lease.Record{Key: key, Owner: "owner", Token: 1}
	store, _ := newStore(&fakeDatabase{rows: []pgx.Row{fakeRow{values: []any{"future"}}}})
	if err := store.Release(context.Background(), record); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("Release(future) error = %v", err)
	}
}
