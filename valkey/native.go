package valkey

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	lease "github.com/faustbrian/golib/pkg/lease"
	"github.com/faustbrian/golib/pkg/lease/internal/failure"
	valkeygo "github.com/valkey-io/valkey-go"
)

type nativeExecutor struct {
	client  valkeygo.Client
	scripts map[operation]*valkeygo.Lua
}

// New constructs a native Valkey backend. The client owns reconnect, ACL,
// TLS, routing, and timeout policy and must be closed by its creator.
func New(client valkeygo.Client, prefix string) (*Store, error) {
	if client == nil {
		return nil, lease.Wrap(lease.ErrInvalidState, "nil valkey client")
	}
	return newStore(newNativeExecutor(client), prefix)
}

// Open constructs a backend and verifies Valkey 9 plus noeviction policy.
func Open(ctx context.Context, client valkeygo.Client, prefix string) (*Store, error) {
	if client == nil {
		return nil, lease.Wrap(lease.ErrInvalidState, "nil valkey client")
	}
	executor := newNativeExecutor(client)
	if err := executor.Check(ctx); err != nil {
		return nil, err
	}
	return newStore(executor, prefix)
}

func newNativeExecutor(client valkeygo.Client) *nativeExecutor {
	return &nativeExecutor{
		client: client,
		scripts: map[operation]*valkeygo.Lua{
			opAcquire:  valkeygo.NewLuaScript(acquireScript),
			opRenew:    valkeygo.NewLuaScript(renewScript),
			opValidate: valkeygo.NewLuaScript(validateScript),
			opRelease:  valkeygo.NewLuaScript(releaseScript),
		},
	}
}

func (executor *nativeExecutor) Check(ctx context.Context) error {
	info, err := executor.client.Do(
		ctx, executor.client.B().Info().Section("server").Build(),
	).ToString()
	if err != nil {
		return classify(ctx, err, false)
	}
	major, err := valkeyMajor(info)
	if err != nil || major < 9 {
		if err == nil {
			err = errors.New("valkey version is older than 9")
		}
		return failure.Wrap(lease.ErrBackendUnavailable, err, "Valkey inspection")
	}
	config, err := executor.client.Do(
		ctx, executor.client.B().ConfigGet().Parameter("maxmemory-policy").Build(),
	).AsStrMap()
	if err != nil {
		return classify(ctx, err, false)
	}
	if config["maxmemory-policy"] != "noeviction" {
		return lease.Wrap(lease.ErrBackendUnavailable, "Valkey eviction policy")
	}
	return nil
}

func valkeyMajor(info string) (int, error) {
	for _, line := range strings.Split(info, "\n") {
		version, found := strings.CutPrefix(strings.TrimSpace(line), "valkey_version:")
		if !found {
			continue
		}
		major, _, _ := strings.Cut(version, ".")
		return strconv.Atoi(major)
	}
	return 0, errors.New("valkey version missing")
}

func (executor *nativeExecutor) Exec(
	ctx context.Context,
	operation operation,
	keys []string,
	args []string,
) ([]string, error) {
	script, exists := executor.scripts[operation]
	if !exists {
		return nil, fmt.Errorf("unknown valkey lease operation")
	}
	values, err := script.Exec(ctx, executor.client, keys, args).ToArray()
	if err != nil {
		return nil, err
	}
	reply := make([]string, len(values))
	for index := range values {
		reply[index], err = values[index].ToString()
		if err != nil {
			return nil, err
		}
	}
	return reply, nil
}
