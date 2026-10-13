# lease

[![CI](https://github.com/faustbrian/go-lease/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/faustbrian/go-lease/actions/workflows/ci.yml)
[![Documentation](https://img.shields.io/badge/docs-adoption_and_API-blue)](docs/index.md)
[![Go Reference](https://pkg.go.dev/badge/github.com/faustbrian/go-lease/v2.svg)](https://pkg.go.dev/github.com/faustbrian/go-lease/v2)
[![Release](https://img.shields.io/github/v/release/faustbrian/go-lease?sort=semver)](https://github.com/faustbrian/go-lease/releases)
[![Go](https://img.shields.io/badge/go-1.27.0-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

`lease` is a fenced, time-bounded distributed lease primitive for Go 1.27.0
and newer. It provides explicit owners, backend-anchored expiry, renewal,
validation, compare-and-release, and monotonically increasing fencing tokens
for native Valkey and PostgreSQL backends.

A lease does not stop expired work. Pass `Handle.Token()` into every protected
write and reject tokens lower than the resource's last accepted fence.
For reconstructible Valkey cache refreshes, `valkey.Store.Guard` exposes opaque
coordinates from `Handle.Snapshot()` that `pkg/cache` can compare atomically
with publication.

## Adoption entry points

The root module is `github.com/faustbrian/go-lease/v2` (stable).
It requires Go 1.27.0. Pin the published `v2.0.0` root tag
with `go get github.com/faustbrian/go-lease/v2@v2.0.0`.
Root subpackages share this module; they need no separate installation.

Start with the [documentation map](docs/index.md) for package selection,
examples, operational constraints, and contributor references.

## Five-minute start

For v2, import `github.com/faustbrian/go-lease/v2` and add `/v2` before backend
or adapter package names. Published v1 releases retain the path without `/v2`.

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
- `postgres`: native `pgx` backend and `migrations` schema
- `adapters/queue`, `adapters/scheduler`, `adapters/service`: canonical
  queue, scheduler, and service lifecycle integrations
- `leasequeue`, `leasescheduler`, `leaseservice`: deprecated compatibility
  facades for the canonical adapter paths
- `leasetest`: deterministic clock and cross-backend conformance suite

See the [documentation index](docs/README.md), [security policy](SECURITY.md),
and [changelog](CHANGELOG.md).

## Documentation

Use the [documentation index](docs/README.md) for package-owned guides,
operational contracts, examples, and maintainer references.

For ecosystem-wide package selection and ownership guidance, see the versioned
[Golib ecosystem index](https://github.com/faustbrian/go-library-tools/blob/v2.1.1/docs/ecosystem/README.md)
and its [Persistence and durability family](https://github.com/faustbrian/go-library-tools/blob/v2.1.1/docs/ecosystem/design-language.md#package-families-and-selection).

## Development

`GOWORK=off go test ./...` runs standalone tests and deterministic fuzz seeds.
`make check` and `make ci` use the public verifier's bounded local route and
retain the manifest's PostgreSQL/Valkey prerequisites; unconfigured direct Go
tests skip live cases. `make check-full` retains the full scheduled/release
campaigns. See [contributing](CONTRIBUTING.md#verification) for focused active
fuzzing, optional local mutation and explicit backend qualification.
