# Failover, restart, and restore

Valkey script cache loss is safe: the client retries the script body after
`NOSCRIPT`. Reconnect and leader failover are safe only if the lease hash and
counter reflect one authoritative committed history. Flush, restore without
counters, or acknowledged replication loss resets continuity.

PostgreSQL reconnect and primary failover use ordinary transaction semantics.
A committed fence is continuous only to the durability point guaranteed by the
cluster. Point-in-time restore or promotion that loses committed rows creates a
new epoch.

After any continuity reset, stop lease consumers, reconcile the protected
resource's maximum accepted fence, choose a new namespace/epoch, and restart
consumers. Never silently reuse low tokens against a resource that remembers
higher ones.

`make backend-hardening` seeds a fixed key, snapshots each disposable backend,
restarts it, and requires the next fence to increase. It then restores the
older PostgreSQL dump and Valkey RDB and proves the next token does not exceed
the protected-resource maximum, detecting rollback. Separate destructive
table-reset and `FLUSHDB` phases must return token 1. Finally, physical
PostgreSQL and Valkey replicas are observed streaming, promoted after their
primaries stop, and required to issue strictly higher tokens.

The same fault gate restarts a persistent TLS-only Valkey instance with a new
CA, server certificate, and password. Connections using either the old trust
root or old credential must fail, while the fully rotated client must acquire a
higher token from the preserved counter history.

A lower or reused token at the protected resource, a missing expected
counter/fence row, or an infrastructure restore/failover event whose durability
point cannot be proven is the operator detection signal. Consumers remain
stopped until reconciliation and a new namespace are complete.
