# Troubleshooting

- Frequent `ErrTimeout`: inspect contention, key cardinality, retry bounds, and
  handler duration; do not add unbounded waiting.
- `ErrAmbiguousOutcome`: stop admission. Check backend logs and wait for expiry
  or reconcile; never assume a canceled release succeeded.
- Immediate expiry: inspect TTL, safety margin, network delay, scheduler delay,
  and runtime pauses. Clock skew cannot extend the local deadline.
- Valkey stale after deploy: verify same prefix/key derivation, cluster slot,
  persistence, failover history, and script compatibility.
- PostgreSQL acquisition failure: verify migration 1, permissions, pool health,
  transaction errors, and token overflow.
- Lower token after restore: continuity was reset. Stop clients and follow the
  epoch procedure in [failover](failover.md).
- High label cardinality: export only the provided hashed `Event` fields.
