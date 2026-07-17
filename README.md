# go-lease

`go-lease` is a fenced, time-bounded distributed lease primitive for Go 1.26.5
and newer. It provides explicit owners, backend-anchored expiry, renewal,
validation, compare-and-release, and monotonically increasing fencing tokens
for native Valkey and PostgreSQL backends.

A lease does not stop expired work. Pass `Handle.Token()` into every protected
write and reject tokens lower than the resource's last accepted fence.

## Five-minute start

Choose a backend guide:

- [Valkey quickstart](docs/quickstart-valkey.md)
- [PostgreSQL quickstart](docs/quickstart-postgres.md)

The core shape is:

```go
policy, err := lease.NewPolicy(lease.PolicyOptions{
    TTL: 30 * time.Second, RenewEvery: 10 * time.Second,
    SafetyMargin: 5 * time.Second, Retry: 100 * time.Millisecond,
    Wait: 2 * time.Second, MaxAttempts: 20,
    OperationTimeout: 2 * time.Second,
})
handle, err := client.Acquire(ctx, key, policy)
if err != nil { return err }
managed, err := handle.StartManaged(ctx)
if err != nil { return err }
defer managed.Stop(context.WithoutCancel(ctx))
defer handle.Release(context.WithoutCancel(ctx))

if err := protectedWrite(ctx, handle.Token()); err != nil { return err }
```

Never infer successful release from cancellation or shutdown. Always inspect
the returned error. No acquisition order or starvation guarantee is provided.

## Packages

- root: model, policies, client, handles, managed renewal, observations
- `memory`: deterministic process-local reference backend, never distributed
- `valkey`: native `valkey-go` backend using backend-time Lua scripts
- `postgres`: native `pgx` backend and `go-migrations` schema
- `leasequeue`, `leasescheduler`, `leaseservice`: lifecycle integrations
- `leasetest`: deterministic clock and cross-backend conformance suite

See the [documentation index](docs/README.md), [security policy](SECURITY.md),
and [changelog](CHANGELOG.md).
