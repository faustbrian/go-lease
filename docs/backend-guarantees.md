# Backend guarantees

| Property | Memory | Valkey | PostgreSQL |
|---|---|---|---|
| Distributed | no | yes | yes |
| Time authority | injected clock | server `TIME` | `clock_timestamp()` |
| Atomic owner+token renew | mutex | Lua script | statement transaction |
| Successor-safe release | mutex | Lua script | conditional update |
| Persistent fence | process life | counter key | `lease_fences` row |
| Cleanup preserves fence | yes | lease TTL only | separate counter table |

Valkey assumes one authoritative deployment and durable replication configured
by its operator. PostgreSQL assumes committed transactions are not rolled back
by an HA failover beyond the selected durability policy. Neither backend is a
consensus or membership service. Acquisition is opportunistic; fairness and
starvation bounds are not promised.

## Continuity epochs

| Backend | Continuity remains valid while | A new epoch begins when | Detection signal |
|---|---|---|---|
| Memory | one store instance retains its key map | the process/store is replaced or its state is discarded | a known key restarts at token 1 |
| Valkey | the same counter-key history survives persistence and authoritative failover | a counter is flushed, deleted, evicted, restored older or missing, lost during failover, or addressed through a new namespace/key derivation | a probe token is not greater than the protected resource's maximum, an expected counter is missing, or durability cannot be proven |
| PostgreSQL | committed `lease_fences` rows survive transactions, backup, restore, and authoritative failover | the fence table/row is dropped or truncated, an older snapshot is restored, failover loses committed rows, the down migration runs, or a new namespace/key derivation is used | a probe token is not greater than the protected resource's maximum, an expected fence row is missing, or the cluster durability point cannot be proven |

PostgreSQL deliberately uses a transactional `lease_fences` row rather than a
database sequence. A transaction abort rolls back its fence increment; the
live operational-fault test requires the next committed token to be exactly
the previous token plus one. Contended attempts may consume committed fence
values, so successful tokens are strictly increasing but need not be
consecutive.

After any detection signal, ownership admission stops until operators compare
the backend with the protected resource, choose a coordinated new namespace
when continuity cannot be proved, and resume only after a higher-token probe.
