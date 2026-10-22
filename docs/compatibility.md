# Compatibility and rolling versions

[Documentation map](index.md) · [Repository README](../README.md)
[Next: API reference](api.md)

The minimum Go version is 1.27.0; CI uses the patched Go 1.27.2 runtime.
Applications should rebuild with the latest supported Go security patch.
The v2 API baseline is checked by
`golib api check --module .` through the pinned public verifier. PostgreSQL
versions 14 through 18 and Valkey 9 are exercised by the integration matrix.

Valkey script response shapes are versioned implicitly by the client release;
rolling clients must retain the four-field record and one-field release
outcomes. `TestRollingScriptResponseVersionsFailClosed` accepts the current
contract and rejects added, removed, or changed fields. PostgreSQL migration 1
is additive for the first release. The live operational fault suite proves an
old client continues through an additive column and fails ownership closed when
a required fence column is incompatible. Future schema changes must be backward
compatible for at least one rolling deployment window before old fields are
removed.

An incompatible key derivation, prefix, schema, or script response must use a
new coordinated namespace/version. Never silently split owners across formats.

The canonical integration paths are `adapters/queue`, `adapters/scheduler`, and
`adapters/service`. V3 removes the former `leasequeue`, `leasescheduler`, and
`leaseservice` facade paths; import the matching adapter path. The adapters
keep those package names, so only the import path changes. This removal
precedes the 180-day window the v1.1 deprecation announced because owned
consumers move with this release.

Adopting v3 requires adding `/v3` to lease imports. `postgres.GoMigration`
returns the `go-migrations/v4` type with the same version, name, SQL and
checksum; runners moving from go-migrations v2 must first complete the
[ledger name upgrade](https://github.com/faustbrian/go-migrations/blob/v4.0.0/docs/operations.md#ledger-name-upgrade). The service adapter's `Hooks` bind
`go-service/v3`, and the queue adapter accepts `go-queue/v2` workers. Key
derivation, schema and fencing tokens are unchanged, so v2 and v3 clients
share one continuity epoch during a rolling upgrade.
