# Shutdown

[Documentation map](index.md) · [Repository README](../README.md)
[Next: API reference](api.md)

`adapters/service.Manager` reserves a bounded handle count and starts renewal
only when the policy enables it. Its `Hooks` method plugs into `service`.
`leaseservice.Manager` is the deprecated compatibility facade.

Shutdown order:

1. stop admitting new ownership-dependent work;
2. run one caller-independent cleanup operation;
3. stop each managed renewer before releasing its handle;
4. retain active ownership accounting until each cleanup attempt finishes;
5. cache and report the aggregate terminal result.

Concurrent and repeated callers wait for that one result with independent
contexts. Canceling one caller stops only that caller's wait; cleanup continues
and later callers receive the cached terminal result. A canceled shutdown or
process exit does not prove remote release. The lease will remain until
successful release or backend expiry.

`Active` counts reservations and retained manager entries, not remotely usable
leases. Manual handle release/expiry does not return capacity before shutdown.
Renewal started by the manager uses a fresh background context rather than the
acquire request's cancellation or values. Cleanup waits for all acquisition/
rollback work, then stops and releases entries sequentially: handle operation
timeouts are not a single aggregate cleanup budget. Uncooperative backend or
sleeper code can delay completion despite a caller ending its wait.
