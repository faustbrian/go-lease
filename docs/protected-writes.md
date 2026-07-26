# Protected writes

The protected system, not the lease backend, rejects stale effects. Store a
last-fence column beside the resource and update it in the same transaction as
the effect.

```sql
BEGIN;
UPDATE accounts
SET balance = $2, last_fence = $3
WHERE id = $1 AND last_fence < $3
RETURNING last_fence;
-- Zero returned rows means the supplied fence is stale or replayed.
COMMIT;
```

The runnable protected-write example has race-tested concurrent writers and
proves the highest fence wins regardless of arrival order.

For an HTTP downstream, include the fence in an authenticated request and make
the receiver persist and compare it. For object storage or APIs without a
conditional fence, acquiring a lease cannot prevent stale overwrites; redesign
the resource protocol or accept that risk explicitly.

## Reconstructible Valkey cache publication

A cache entry is not a durable business effect. When both modules use the same
standalone Valkey deployment, derive an opaque guard from the acquired record
and pass it to `cache.Cache.SetIfOwned`:

```go
owned, err := leaseStore.TryAcquire(ctx, key, owner, ttl)
if err != nil {
	return err
}
guard, err := leaseStore.Guard(owned)
if err != nil {
	return err
}
if err := catalog.SetIfOwned(ctx, query, references, guard); err != nil {
	return err
}
```

The cache script checks the exact active lease hash owner and token and writes
the expiring record atomically. Cache expiration or deletion cannot erase the
guard because the lease hash is separate. Missing, expired, released, and
successor leases reject old publishers.

This narrow protocol is safe for reconstructible cache refreshes. Durable rows,
events, webhooks, files, and external API effects must still persist and compare
the highest accepted fence at their own authoritative boundary.
