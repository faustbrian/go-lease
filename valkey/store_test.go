package valkey

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	lease "github.com/faustbrian/go-lease"
)

type fakeExecutor struct {
	reply []string
	err   error
	keys  []string
	op    operation
}

func (executor *fakeExecutor) Exec(
	_ context.Context,
	op operation,
	keys []string,
	_ []string,
) ([]string, error) {
	executor.op = op
	executor.keys = append([]string(nil), keys...)
	return executor.reply, executor.err
}

func TestTryAcquireUsesRedactedSameSlotKeys(t *testing.T) {
	t.Parallel()

	executor := &fakeExecutor{reply: []string{"ok", "1", "1000", "2000"}}
	store, err := newStore(executor, "lease")
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}
	key, _ := lease.NewKey("queue", "sensitive-customer")
	record, err := store.TryAcquire(context.Background(), key, "owner", time.Second)
	if err != nil || record.Token != 1 {
		t.Fatalf("TryAcquire() = %+v, %v", record, err)
	}
	if len(executor.keys) != 2 || hashTag(executor.keys[0]) != hashTag(executor.keys[1]) {
		t.Fatalf("keys are not in one cluster slot: %v", executor.keys)
	}
	if strings.Contains(strings.Join(executor.keys, " "), "sensitive-customer") {
		t.Fatalf("backend key leaked raw lease identity: %v", executor.keys)
	}
}

func TestAtomicOutcomesMapToStableErrors(t *testing.T) {
	t.Parallel()

	key, _ := lease.NewKey("scheduler", "job")
	record := lease.Record{Key: key, Owner: "old", Token: 1}
	tests := []struct {
		name  string
		reply []string
		call  func(*Store) error
		want  error
	}{
		{"contention", []string{"contended"}, func(store *Store) error {
			_, err := store.TryAcquire(context.Background(), key, "new", time.Second)
			return err
		}, lease.ErrContended},
		{"stale renew", []string{"stale"}, func(store *Store) error {
			_, err := store.Renew(context.Background(), record, time.Second)
			return err
		}, lease.ErrStaleOwner},
		{"stale validate", []string{"stale"}, func(store *Store) error {
			_, err := store.Validate(context.Background(), record)
			return err
		}, lease.ErrStaleOwner},
		{"stale release", []string{"stale"}, func(store *Store) error {
			return store.Release(context.Background(), record)
		}, lease.ErrStaleOwner},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store, _ := newStore(&fakeExecutor{reply: test.reply}, "lease")
			if err := test.call(store); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
