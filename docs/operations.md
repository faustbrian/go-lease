# Operations runbook

Monitor success, contention, stale, unavailable, ambiguous, and renewal-loss
rates using `NewObservedBackend`. Labels contain only operation, outcome, and a
bounded key hash. Alert on any ambiguous renewal, token exhaustion, continuity
reset, sustained backend unavailability, or cleanup backlog.
Observers are best effort and must tolerate dropped events. Each observer has
one isolated in-flight callback, so a blocked exporter cannot delay lease state
transitions or create an unbounded goroutine queue.

For Valkey require TLS/ACL where traffic is untrusted, `noeviction`, persistence
and replication consistent with the continuity promise, and backups that
include counter keys. For PostgreSQL monitor pool saturation, deadlocks,
replication lag, transaction aborts, inactive rows, and fence table growth.
Rotate Valkey trust and credentials by deploying clients that trust the new CA
and can obtain the new secret, then restart or reload servers, retire the old
material, and require a continuity probe before resuming lease consumers. An
old trust root or password must fail closed as backend unavailability.

Incident response: freeze new consumers, preserve backend and protected
resource evidence, identify the last accepted resource fence, determine the
continuity epoch, reconcile, then resume with a new namespace if continuity
cannot be proven.

Adapter errors expose only stable lease classifications and redacted operation
names. `errors.Is` can still match the underlying driver cause for local
control flow, but its message is deliberately omitted. Log the classified
error and bounded observation event; do not separately log raw driver causes,
connection strings, owners, or lease keys.
