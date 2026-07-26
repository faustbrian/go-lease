# PostgreSQL migrations

`postgres.GoMigration` returns the immutable `migrations` migration.
Migration 1 creates `lease_fences`, `lease_records`, expiry and cleanup indexes,
checks, and the foreign key between lease rows and counters.

Apply migrations before rolling out clients. The current schema is compatible
with v1 clients. Do not drop or truncate `lease_fences` during routine cleanup.
`Store.Cleanup` removes at most 10,000 inactive lease rows older than one hour
and deliberately leaves counters.

For rollback, stop all clients first. The down migration deletes continuity
history and therefore starts a new fencing epoch.
