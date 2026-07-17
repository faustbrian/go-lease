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
