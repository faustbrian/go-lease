# Migrating Laravel locks and unique jobs

Laravel cache locks and `ShouldBeUnique` prevent common overlap but do not
provide a protected-resource fencing protocol. Inventory every lock key, TTL,
blocking wait, owner assumption, and side effect before migration.

Map the Laravel key to `NewKey`, the lock seconds to policy TTL, and bounded
blocking to `Wait`, `Retry`, and `MaxAttempts`. Replace `block()` callbacks with
queue or scheduler adapters. Add a fence column to every stale-write-sensitive
resource and compare it transactionally.

During dual rollout, do not let legacy and fenced workers write the same
resource unless the legacy path is disabled or given a compatible fence. A
cache flush can reset Valkey counters, so plan namespace epochs before cutover.
