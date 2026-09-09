# Shutdown

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
