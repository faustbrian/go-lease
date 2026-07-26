# Failure matrix

| Failure | Local result | Ownership admission | Operator action |
|---|---|---|---|
| contention | `ErrContended`/`ErrTimeout` | no | retry within policy |
| stale owner | `ErrStaleOwner` | no | stop work |
| deadline reached | `ErrLost`/expired state | no | stop work |
| canceled before dispatch | `ErrCanceled` | no | no remote mutation was attempted |
| canceled or timed out after mutation dispatch | `ErrAmbiguousOutcome` | no | wait for expiry/reconcile |
| connection loss during mutation | `ErrAmbiguousOutcome` | no | wait for expiry/reconcile |
| definite validation outage | `ErrBackendUnavailable` | no | restore backend |
| mismatched successful acquire/renew | `ErrAmbiguousOutcome` | no | stop and reconcile |
| mismatched successful validation | `ErrBackendUnavailable` | no | stop and inspect compatibility |
| Valkey `NOSCRIPT` | transparent reload | unchanged if response succeeds | none |
| backend process restart with durable counters | fence increases | normal after verification | alert if token regresses |
| backend restore/flush | new continuity epoch | stop | reconcile and re-namespace |
| token exhaustion | unavailable | no | create coordinated new epoch |
| callback panic | application policy | no new admission | fence remaining effects |

Deadlocks and PostgreSQL serialization/transaction aborts surface as backend
errors; callers must not retry beyond policy or reinterpret them as ownership.
