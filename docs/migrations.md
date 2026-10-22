# PostgreSQL migrations

[Documentation map](index.md) · [Repository README](../README.md)
[Next: API reference](api.md)

`postgres.GoMigration` returns an immutable `go-migrations/v4` migration.
Its checksum matches the v2-era value, but go-migrations v4 records history in
`public.migrations`; complete the [ledger name upgrade](https://github.com/faustbrian/go-migrations/blob/v4.0.0/docs/operations.md#ledger-name-upgrade) before the first
v4 run.
Migration 1 creates `lease_fences`, `lease_records`, expiry and cleanup indexes,
checks, and the foreign key between lease rows and counters.

Apply migrations before rolling out clients. The current schema is compatible
with v1 clients. Do not drop or truncate `lease_fences` during routine cleanup.
`Store.Cleanup` removes at most 10,000 inactive lease rows older than one hour
and deliberately leaves counters.

For rollback, stop all clients first. The down migration deletes continuity
history and therefore starts a new fencing epoch.
