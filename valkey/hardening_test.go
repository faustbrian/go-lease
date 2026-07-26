package valkey

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
)

func TestStoreRejectsInvalidInputsAndPrefixes(t *testing.T) {
	t.Parallel()

	for _, prefix := range []string{"", strings.Repeat("x", 65), "bad prefix", "bad{tag}"} {
		if _, err := newStore(&fakeExecutor{}, prefix); !errors.Is(err, lease.ErrInvalidState) {
			t.Fatalf("newStore(%q) error = %v", prefix, err)
		}
	}
	store, _ := newStore(&fakeExecutor{}, "lease")
	key, _ := lease.NewKey("valkey", "input")
	record := lease.Record{Key: key, Owner: "owner"}
	if _, err := store.TryAcquire(context.Background(), key, "owner", 0); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("TryAcquire(ttl) error = %v", err)
	}
	if _, err := store.Renew(context.Background(), record, time.Second); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Renew(token) error = %v", err)
	}
	if _, err := store.Validate(context.Background(), record); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Validate(token) error = %v", err)
	}
	if err := store.Release(context.Background(), record); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Release(token) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.TryAcquire(ctx, key, "owner", time.Second); !errors.Is(err, lease.ErrCanceled) {
		t.Fatalf("TryAcquire(canceled) error = %v", err)
	}
}

func TestStoreClassifiesExecutorFailuresAndCorruptReplies(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("backend")
	key, _ := lease.NewKey("valkey", "failure")
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
		store, _ := newStore(&fakeExecutor{err: backendErr}, "lease")
		if err := mutation(store); !errors.Is(err, lease.ErrAmbiguousOutcome) {
			t.Fatalf("mutation error = %v", err)
		}
	}
	store, _ := newStore(&fakeExecutor{err: backendErr}, "lease")
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
	secret := errors.New("rediss://secret-owner:secret-password@backend")
	classified := classify(context.Background(), secret, true)
	if !errors.Is(classified, secret) || strings.Contains(classified.Error(), "secret") {
		t.Fatalf("classified error leaked backend detail: %v", classified)
	}

	badReplies := [][]string{{}, {"ok"}, {"ok", "0", "1", "2"}, {"ok", "x", "1", "2"}, {"ok", "1", "2", "1"}}
	for _, reply := range badReplies {
		store, _ := newStore(&fakeExecutor{reply: reply}, "lease")
		if _, err := store.TryAcquire(context.Background(), key, "owner", time.Second); !errors.Is(err, lease.ErrBackendUnavailable) {
			t.Fatalf("TryAcquire(reply=%v) error = %v", reply, err)
		}
	}
}

func TestReleaseResponsesAndHashTagFailures(t *testing.T) {
	t.Parallel()

	key, _ := lease.NewKey("valkey", "release")
	record := lease.Record{Key: key, Owner: "owner", Token: 1}
	for _, reply := range [][]string{{"ok"}, {"missing"}} {
		store, _ := newStore(&fakeExecutor{reply: reply}, "lease")
		if err := store.Release(context.Background(), record); err != nil {
			t.Fatalf("Release(%v) error = %v", reply, err)
		}
	}
	for _, reply := range [][]string{{}, {"future"}, {"ok", "extra"}} {
		store, _ := newStore(&fakeExecutor{reply: reply}, "lease")
		if err := store.Release(context.Background(), record); !errors.Is(err, lease.ErrBackendUnavailable) {
			t.Fatalf("Release(%v) error = %v", reply, err)
		}
	}
	if hashTag("missing") != "" || hashTag("}bad{") != "" {
		t.Fatal("hashTag accepted malformed keys")
	}
}

func TestContinuationRejectsMismatchedBackendToken(t *testing.T) {
	t.Parallel()

	key, _ := lease.NewKey("valkey", "mismatched-token")
	record := lease.Record{Key: key, Owner: "owner", Token: 1}
	reply := []string{"ok", "2", "1000", "2000"}
	store, _ := newStore(&fakeExecutor{reply: reply}, "lease")
	if _, err := store.Renew(context.Background(), record, time.Second); !errors.Is(err, lease.ErrAmbiguousOutcome) {
		t.Fatalf("Renew(mismatched token) error = %v", err)
	}
	store, _ = newStore(&fakeExecutor{reply: reply}, "lease")
	if _, err := store.Validate(context.Background(), record); !errors.Is(err, lease.ErrBackendUnavailable) {
		t.Fatalf("Validate(mismatched token) error = %v", err)
	}
	reply[1] = "1"
	store, _ = newStore(&fakeExecutor{reply: reply}, "lease")
	if _, err := store.Renew(context.Background(), record, time.Second); err != nil {
		t.Fatalf("Renew(matching token) error = %v", err)
	}
	store, _ = newStore(&fakeExecutor{reply: reply}, "lease")
	if _, err := store.Validate(context.Background(), record); err != nil {
		t.Fatalf("Validate(matching token) error = %v", err)
	}
}

func TestScriptsUseBackendTimeAndAtomicComparisons(t *testing.T) {
	t.Parallel()

	for name, script := range map[string]string{
		"acquire": acquireScript, "renew": renewScript,
	} {
		if !strings.Contains(script, "redis.call('TIME')") {
			t.Fatalf("%s does not use backend time", name)
		}
	}
	for name, script := range map[string]string{
		"renew": renewScript, "validate": validateScript, "release": releaseScript,
	} {
		ownerComparison := "redis.call('HGET', KEYS[1], 'owner') ~= ARGV[1]"
		// #nosec G101 -- token is a Lua hash field, not a credential.
		tokenComparison := "redis.call('HGET', KEYS[1], 'token') ~= ARGV[2]"
		if !strings.Contains(script, ownerComparison) ||
			!strings.Contains(script, tokenComparison) {
			t.Fatalf("%s does not compare owner and token", name)
		}
	}
}

func TestRollingScriptResponseVersionsFailClosed(t *testing.T) {
	t.Parallel()

	key, _ := lease.NewKey("valkey", "rolling-script")
	compatible, _ := newStore(&fakeExecutor{
		reply: []string{"ok", "1", "1000", "2000"},
	}, "lease")
	if _, err := compatible.TryAcquire(
		context.Background(), key, "compatible-client", time.Second,
	); err != nil {
		t.Fatalf("TryAcquire(compatible script) error = %v", err)
	}

	for name, reply := range map[string][]string{
		"record field added":   {"ok", "1", "1000", "2000", "future"},
		"record field removed": {"ok", "1", "2000"},
		"status changed":       {"future", "1", "1000", "2000"},
	} {
		store, _ := newStore(&fakeExecutor{reply: reply}, "lease")
		if _, err := store.TryAcquire(
			context.Background(), key, "rolling-client", time.Second,
		); !errors.Is(err, lease.ErrBackendUnavailable) {
			t.Fatalf("TryAcquire(%s) error = %v", name, err)
		}
	}
}
