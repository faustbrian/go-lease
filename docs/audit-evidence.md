# Acceptance audit evidence

This audit maps the fenced distributed lease goal to the current local tree.
Hosted CI remains the maintainer's final external verification step.

## Core model

| Requirement | Evidence |
|---|---|
| bounded namespaced keys | `key.go`, `TestKeyIsBoundedAndNamespaced` |
| immutable acquisition policy | `policy.go`, `TestPolicyIsValidatedAndImmutable` |
| owner, fence, times, state | `record.go`, `handle.go`, lifecycle tests |
| try, bounded wait, renew, validate, release | `client.go`, `handle.go` |
| managed renewal and loss | `managed.go`, uncertainty and shutdown tests |
| stable errors | `errors.go`, classification tests |
| deterministic clock/retry | `leasetest.Clock`, injected retry tests |
| waiter, operation, goroutine bounds | client capacity and timeout tests |
| backend skew, client rollback/freeze, response pause | dual local deadline tests |

## Correctness and backends

| Requirement | Evidence |
|---|---|
| monotonically increasing success fences | shared conformance and model fuzzing |
| atomic owner plus token renewal | Valkey Lua and PostgreSQL conditional SQL |
| successor-safe release | shared conformance and stale release tests |
| backend time | Valkey `TIME`; PostgreSQL `clock_timestamp()` |
| Valkey cluster-safe layout | hashed same-slot lease/counter key test |
| `NOSCRIPT` recovery | live `SCRIPT FLUSH` integration test |
| PostgreSQL durable continuity | separate `lease_fences` table and migration |
| cleanup without fence reset | bounded cleanup tests and retained counters |
| restore/flush continuity limits | backend guarantees and failover guides |

Disposable local runs passed under `-race` for PostgreSQL 14, 15, 16, 17, and
18 and Valkey 9. Both backends passed after container restart; Valkey also
passed after script-cache flush. Client-owned TLS, ACL, reconnect, pool, and
command-timeout failures surface through fail-closed adapter tests. The
reproducible `make backend-hardening` target seeds and snapshots fixed fences,
proves continuity across restart, detects reuse after older-snapshot restore,
proves destructive reset returns token 1, promotes streaming PostgreSQL and
Valkey replicas, and repeats script-cache and live partition classification
checks in CI. Its secure Valkey phase rotates the CA, server certificate, named
ACL user, and password; old trust and old ACL credentials fail closed before
the new client proves fence continuity.

The PostgreSQL operational fault phase forces a transaction abort and a real
`40P01` deadlock, races bounded cleanup against successor acquisition, churns
caller-owned pools, runs acquisition at serializable isolation, and exercises
both additive-compatible and fail-closed incompatible rolling schemas. Fence
history must remain monotonic after every phase. PostgreSQL uses transactional
fence rows instead of sequences; the abort phase proves a rolled-back increment
does not create a committed jump and the next acquisition advances exactly
once.

The physical-replica phase authorizes SCRAM replication only from the
disposable primary's directly connected network before `pg_basebackup`; the
rule is reloaded explicitly and disappears with the fault container.

Valkey rolling-script tests accept the documented v1 response and reject
added, removed, or changed response fields as unavailable rather than treating
an incompatible response as ownership.

## Integrations and security

Queue workers and scheduler callbacks receive the fence; their direct loss
tests advance authoritative time beyond expiry and prove each callback context
is canceled with `ErrLost`. Service lifecycle bounds handles, stops renewers,
and reports remote release failure. The protected-write example race-tests
concurrent writers and rejects stale and replayed tokens atomically.

Cryptographic 192-bit owners, hashed backend keys, redacted observations,
redacted classified driver errors, observer panic isolation, token overflow
checks, bounded nonblocking observer slots, bounded cleanup, and the threat
model cover spoofing, collision,
leakage, stale writers, split brain, rollback, restore, malicious contention,
and resource exhaustion.

## Quality and release

- `make check`: format, vet, unit, race, exact 100.0% production statement
  coverage, repeated lifecycle stress, fuzz smoke, benchmarks, docs, examples,
  and API baseline
- `make lint staticcheck`: strict analyzer gates
- `make mutation`: 23 Go mutants plus four adapter comparison classes killed,
  zero lived, 100% efficacy and mutant coverage
- `make vuln`: no known reachable Go vulnerabilities
- `make workflows`: pinned workflow syntax validation
- `make nilaway`: visible advisory analysis; findings do not block by policy
- CI: PostgreSQL 14-18 and Valkey 9 matrices, mutation, vulnerability, lint,
  advisory NilAway, release workflow, and the locally reproducible gates

No unsupported fairness, consensus, multi-key atomicity, stopped-expired-work,
or distributed-transaction claim is made. Every dangerous stale effect still
requires protected-resource fencing.
