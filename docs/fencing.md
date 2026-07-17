# Fencing model

For key `k`, each successful acquisition returns token `F(k,n)` where
`F(k,n+1) > F(k,n)` within the backend's continuity epoch. Owner identity and
token are compared together for renew, validate, and release.

The protected resource must store its last accepted token transactionally:

```sql
UPDATE protected_jobs
SET payload = $1, last_fence = $2
WHERE id = $3 AND last_fence < $2;
```

Zero updated rows means the writer is stale. A lease alone cannot stop work
that survived a pause or partition. See the runnable
[`protectedwrite` example](../examples/protectedwrite/main.go).

PostgreSQL continuity lasts while `lease_fences` retains committed history.
Valkey continuity lasts while the same-slot counter survives replication,
failover, and persistence. Restore, flush, or data loss starts a new epoch and
requires operator coordination with protected resources.
