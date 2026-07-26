# PostgreSQL quickstart

Apply the owned migration before constructing the backend:

```go
migration, err := leasepostgres.GoMigration()
if err != nil { return err }
// Apply migration with migrations during deployment.

pool, err := pgxpool.New(ctx, os.Getenv("POSTGRES_URL"))
if err != nil { return err }
defer pool.Close()
backend, err := leasepostgres.New(pool)
if err != nil { return err }
leases, err := lease.NewClient(backend, lease.ClientOptions{})
```

PostgreSQL `clock_timestamp()` anchors acquisition, renewal, validation, and
cleanup. `lease_fences` is never cleaned; inactive `lease_records` may be
removed in bounded batches with `Store.Cleanup`. This separation prevents
cleanup from recycling a fence.

Use normal PostgreSQL HA and synchronous durability policy appropriate to the
resource. A restore to older data creates a new continuity epoch.
