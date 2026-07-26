package valkey

import (
	"context"
	"errors"
	"testing"

	lease "github.com/faustbrian/golib/pkg/lease"
	valkeygo "github.com/valkey-io/valkey-go"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
)

func TestNewBuildsNativeExecutorAndRejectsNil(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, "lease"); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("New(nil) error = %v", err)
	}
	client := valkeymock.NewClient(gomock.NewController(t))
	store, err := New(client, "lease")
	if err != nil || store == nil {
		t.Fatalf("New() = %#v, %v", store, err)
	}
}

func TestNativeExecutorDecodesAndRejectsResponses(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		client := valkeymock.NewClient(gomock.NewController(t))
		client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.Result(
			valkeymock.ValkeyArray(valkeymock.ValkeyString("ok"), valkeymock.ValkeyBlobString("1")),
		))
		executor := &nativeExecutor{client: client, scripts: map[operation]*valkeygo.Lua{
			opAcquire: valkeygo.NewLuaScript("return {'ok','1'}"),
		}}
		reply, err := executor.Exec(context.Background(), opAcquire, []string{"key"}, nil)
		if err != nil || len(reply) != 2 || reply[1] != "1" {
			t.Fatalf("Exec() = %v, %v", reply, err)
		}
	})
	t.Run("backend", func(t *testing.T) {
		backendErr := errors.New("connection")
		client := valkeymock.NewClient(gomock.NewController(t))
		client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(backendErr))
		executor := &nativeExecutor{client: client, scripts: map[operation]*valkeygo.Lua{
			opAcquire: valkeygo.NewLuaScript("return {'ok'}"),
		}}
		if _, err := executor.Exec(context.Background(), opAcquire, []string{"key"}, nil); !errors.Is(err, backendErr) {
			t.Fatalf("Exec(backend) error = %v", err)
		}
	})
	t.Run("nested", func(t *testing.T) {
		client := valkeymock.NewClient(gomock.NewController(t))
		client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.Result(
			valkeymock.ValkeyArray(valkeymock.ValkeyArray(valkeymock.ValkeyString("nested"))),
		))
		executor := &nativeExecutor{client: client, scripts: map[operation]*valkeygo.Lua{
			opAcquire: valkeygo.NewLuaScript("return {{'nested'}}"),
		}}
		if _, err := executor.Exec(context.Background(), opAcquire, []string{"key"}, nil); err == nil {
			t.Fatal("Exec(nested) error = nil")
		}
	})
	client := valkeymock.NewClient(gomock.NewController(t))
	if _, err := (&nativeExecutor{client: client, scripts: map[operation]*valkeygo.Lua{}}).Exec(
		context.Background(), operation(255), nil, nil,
	); err == nil {
		t.Fatal("Exec(unknown) error = nil")
	}
}

func TestOpenChecksValkeyVersionAndEviction(t *testing.T) {
	t.Parallel()
	if _, err := Open(context.Background(), nil, "lease"); !errors.Is(err, lease.ErrInvalidState) {
		t.Fatalf("Open(nil) error = %v", err)
	}

	tests := []struct {
		name    string
		info    string
		policy  string
		wantErr bool
	}{
		{"safe", "valkey_version:9.0.1\r\n", "noeviction", false},
		{"old", "valkey_version:8.1.0\r\n", "", true},
		{"malformed", "valkey_version:future\r\n", "", true},
		{"missing", "redis_version:9.0.1\r\n", "", true},
		{"eviction", "valkey_version:9.0.1\r\n", "allkeys-lru", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := valkeymock.NewClient(gomock.NewController(t))
			client.EXPECT().Do(gomock.Any(), valkeymock.Match("INFO", "server")).Return(
				valkeymock.Result(valkeymock.ValkeyBlobString(test.info)),
			)
			if test.info == "valkey_version:9.0.1\r\n" {
				client.EXPECT().Do(gomock.Any(), valkeymock.Match("CONFIG", "GET", "maxmemory-policy")).Return(
					valkeymock.Result(valkeymock.ValkeyArray(
						valkeymock.ValkeyBlobString("maxmemory-policy"),
						valkeymock.ValkeyBlobString(test.policy),
					)),
				)
			}
			store, err := Open(context.Background(), client, "lease")
			if test.wantErr && err == nil {
				t.Fatal("Open() error = nil")
			}
			if !test.wantErr && (err != nil || store == nil) {
				t.Fatalf("Open() = %#v, %v", store, err)
			}
		})
	}
}

func TestCheckPropagatesInspectionFailures(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("inspection")
	client := valkeymock.NewClient(gomock.NewController(t))
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(backendErr))
	if err := (&nativeExecutor{client: client}).Check(context.Background()); !errors.Is(err, lease.ErrBackendUnavailable) || !errors.Is(err, backendErr) {
		t.Fatalf("Check(info) error = %v", err)
	}

	client = valkeymock.NewClient(gomock.NewController(t))
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(
		valkeymock.Result(valkeymock.ValkeyBlobString("valkey_version:9.0.1\r\n")),
	)
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(backendErr))
	if err := (&nativeExecutor{client: client}).Check(context.Background()); !errors.Is(err, lease.ErrBackendUnavailable) || !errors.Is(err, backendErr) {
		t.Fatalf("Check(config) error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	client = valkeymock.NewClient(gomock.NewController(t))
	client.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.ErrorResult(context.Canceled))
	if err := (&nativeExecutor{client: client}).Check(canceled); !errors.Is(err, lease.ErrCanceled) {
		t.Fatalf("Check(canceled) error = %v", err)
	}
}
