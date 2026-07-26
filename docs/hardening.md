# Hardening evidence

| Requirement | Executable evidence |
|---|---|
| successor-safe stale rejection | `leasetest.RunBackendConformance` |
| independent owner/token rejection | shared conformance forged-identity matrix |
| late/corrupt successful response | `TestHandleRejectsMismatchedSuccessfulResponses` |
| state model and clock jumps | `FuzzLeaseStateModel` |
| backend skew, rollback, and frozen client clock | local dual-deadline tests |
| bounded retry and jitter | `TestAcquireUsesInjectedBoundedJitter` |
| contention race | `TestContentionElectsExactlyOneOwner`, `make race` |
| repeated lifecycle concurrency | `make stress` |
| renewal uncertainty | `TestManagedRenewalReportsUncertaintyAndStopsAdmission` |
| observer re-entrancy | `TestObserverCanInspectHandleDuringStateTransition` |
| blocking observer isolation | `TestBlockingObserverCannotDelayLeaseTransition` |
| queue and scheduler loss cancellation | direct integration loss tests |
| concurrent handle operations | `TestHandleRejectsConcurrentOperationsWithoutBlockingState` |
| deadline during remote operation | `TestHandleFailsClosedWhenDeadlinePassesDuringOperation` |
| shutdown semantics | `leaseservice` hardening tests |
| response corruption | Valkey/PostgreSQL hardening tests |
| script owner/token comparison | `TestScriptsUseBackendTimeAndAtomicComparisons` |
| meaningful production coverage | `make coverage` requires exactly 100.0% |
| fuzz smoke | `make fuzz` |
| ownership mutation resistance | `make mutation`, including Lua/SQL predicates |
| live backend parity | `make integration` with backend environment variables |
| restart, restore, reset, partition, backend promotion | `make backend-hardening` |
| PostgreSQL abort, deadlock, cleanup race, pool churn, isolation | `TestLiveOperationalFaults` |
| Valkey TLS and named ACL rotation | secure continuity phases in backend hardening |
| allocation and latency baseline | `make benchmark` |

Local release verification uses `make check lint staticcheck nilaway mutation
vuln workflows`, `make backend-hardening`, and live PostgreSQL 14-18 and Valkey
9 matrices. The hardening target owns disposable PostgreSQL 18 and Valkey 9
containers, repeats conformance after restart, exercises script-cache recovery,
and requires partitioned mutations to return `ErrAmbiguousOutcome`. Hosted CI
is the final external verification step and is not used to block local
progress.
