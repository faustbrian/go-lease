# Threat model

Protected assets are exclusive admission, monotonic fences, successor leases,
and identifiers. Adversaries include stale processes, partitions, clock jumps,
backend rollback, replay, malicious contention, and operators restoring data.

Controls:

- 192-bit cryptographic owner identities; injectable deterministic test source
- owner plus token comparison on renew, validate, and release
- backend time and local safety deadline
- persistent per-key counters and explicit continuity epochs
- SHA-256-derived backend keys and observation labels
- redacted classified adapter errors that preserve `errors.Is` without
  rendering driver causes
- bounded key bytes, TTL, wait, attempts, operation time, waiters, managed
  renewers, observers, handles, cleanup, and retry
- fail-closed ambiguity and context cancellation

Residual risks include compromised backend administrators, resource endpoints
that ignore fences, durability rollback beyond backend guarantees, denial of
service by exhausting bounded key cardinality or tokens, and process code that
continues effects after cancellation.
