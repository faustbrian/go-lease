# Source documentation audit

[Documentation map](index.md) · [Repository README](../README.md)
[Next: API reference](api.md)

This is a historical audit of the v2 source at the base below. Its
`leasequeue`, `leasescheduler` and `leaseservice` facade rows describe paths v3
removes.

## Scope and acceptance

Base: `ff28f9913a08bd1f024486c157f099b4d3a41949` (remote main).
Tier A, this repository only: clarify real leases, local admission deadlines,
managed renewal, fencing/resource enforcement, cancellation and adapter ownership
without changing executable behavior, dependencies, fixtures or durable contracts.
Direct consumers are root/backend clients, canonical queue/scheduler/service
adapters and intentionally retained deprecated named facades.
Risks: ambiguous remote effects, backend-versus-local clock domains, cooperative
callbacks, deadline/loss races, owner/token comparison versus authentication,
fence-history continuity, cancellation/cleanup and borrowed clients.
Smallest checks: all-file token/AST/directive equality, useful public/header
contracts, parsed/native rendered docs, native source/local documentation and
applicable links, changed-content secrets, one ordinary complete final diff and
Intent commit. No live backend/fault/race/fuzz/mutation/release campaigns.

## Reproducible complete inventory

Use `git ls-files '*.go' 'go.mod' 'go.sum'`, modules.json/packages.json and
`rg -n '^func (Test|TestMain|Fuzz|Benchmark|Example)|go:|Code generated|TODO|FIXME|HACK|NOTE|WARNING|Deprecated:|nolint|nosec' --glob '*.go'`.
One root module: github.com/faustbrian/go-lease/v2, minimum Go 1.27.0;
pinned producer Go 1.27.2. All 62 tracked Go files in 16 catalog package directories are owned;
external test namespaces are retained and included below.
Every exported field/interface method and important private/test helper is in
scope. No generated Go source, vendor files or build-tag/platform variant exists.

| Package directory | Go namespaces, including tests |
| --- | --- |
| `.` | `lease`, `lease_test` |
| `adapters/queue` | `leasequeue` |
| `adapters/scheduler` | `leasescheduler` |
| `adapters/service` | `leaseservice` |
| `examples/postgres` | `main` |
| `examples/protectedwrite` | `main` |
| `examples/valkey` | `main` |
| `internal/failure` | `failure` |
| `internal/guard` | `guard` |
| `leasequeue` | `leasequeue`, `leasequeue_test` |
| `leasescheduler` | `leasescheduler`, `leasescheduler_test` |
| `leaseservice` | `leaseservice`, `leaseservice_test` |
| `leasetest` | `leasetest`, `leasetest_test` |
| `memory` | `memory`, `memory_test` |
| `postgres` | `postgres`, `postgres_test` |
| `valkey` | `valkey`, `valkey_test` |

| Go file | Audit role |
| --- | --- |
| `adapters/queue/worker.go` | implementation and public/private ownership contracts |
| `adapters/queue/worker_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `adapters/scheduler/coordinator.go` | implementation and public/private ownership contracts |
| `adapters/scheduler/coordinator_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `adapters/service/manager.go` | implementation and public/private ownership contracts |
| `adapters/service/manager_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `backend.go` | implementation and public/private ownership contracts |
| `benchmark_test.go` | fixed workload/setup/comparison bounds, not new performance proof |
| `client.go` | implementation and public/private ownership contracts |
| `client_context_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `client_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `doc.go` | implementation and public/private ownership contracts |
| `errors.go` | implementation and public/private ownership contracts |
| `examples/postgres/main.go` | main-package entry and backend/protected-write lifetime; executable preserved |
| `examples/protectedwrite/main.go` | main-package entry and backend/protected-write lifetime; executable preserved |
| `examples/protectedwrite/main_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `examples/valkey/main.go` | main-package entry and backend/protected-write lifetime; executable preserved |
| `fuzz_test.go` | bounded fuzz input/model oracle; no new exploration |
| `handle.go` | implementation and public/private ownership contracts |
| `hardening_internal_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `internal/failure/failure.go` | implementation and public/private ownership contracts |
| `internal/failure/failure_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `internal/guard/guard.go` | implementation and public/private ownership contracts |
| `internal/guard/guard_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `key.go` | implementation and public/private ownership contracts |
| `leasequeue/worker.go` | implementation and public/private ownership contracts |
| `leasequeue/worker_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `leasescheduler/coordinator.go` | implementation and public/private ownership contracts |
| `leasescheduler/coordinator_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `leaseservice/hardening_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `leaseservice/manager.go` | implementation and public/private ownership contracts |
| `leaseservice/manager_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `leasetest/clock.go` | implementation and public/private ownership contracts |
| `leasetest/clock_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `leasetest/conformance.go` | implementation and public/private ownership contracts |
| `managed.go` | implementation and public/private ownership contracts |
| `managed_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `memory/conformance_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `memory/hardening_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `memory/model_test.go` | bounded fuzz input/model oracle; no new exploration |
| `memory/store.go` | implementation and public/private ownership contracts |
| `memory/store_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `model_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `observation.go` | implementation and public/private ownership contracts |
| `observation_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `policy.go` | implementation and public/private ownership contracts |
| `postgres/benchmark_test.go` | fixed workload/setup/comparison bounds, not new performance proof |
| `postgres/hardening_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `postgres/integration_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `postgres/migration.go` | implementation and public/private ownership contracts |
| `postgres/pgx_adoption_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `postgres/store.go` | implementation and public/private ownership contracts |
| `postgres/store_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `record.go` | implementation and public/private ownership contracts |
| `stress_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `valkey/hardening_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `valkey/integration_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `valkey/native.go` | implementation and public/private ownership contracts |
| `valkey/native_test.go` | assertions, collaborator/fault/model helpers and environment admission |
| `valkey/scripts.go` | implementation and public/private ownership contracts |
| `valkey/store.go` | implementation and public/private ownership contracts |
| `valkey/store_test.go` | assertions, collaborator/fault/model helpers and environment admission |

## Missing versus investigated or unresolved

Initially missing: meaningful root/package responsibility comments, Backend and
Clock method obligations, public options/record fields and nuanced owner/token,
clock, cancellation and provider callback contracts. Existing short comments
must be reconciled with implementation, tests and maintained fencing, renewal,
shutdown, backend-guarantee and threat-model evidence before strengthening them.

Investigated: acquisition responses must match key/owner and ordered timestamps;
contention alone retries with one owner. MaxWaiters covers Acquire, not TryAcquire.
Handle state/operation admission is mutex-owned, backend I/O is outside it, and
late success cannot revive an expired local budget. Injected and real-time bounds
start before each backend attempt, independently of backend timestamp skew.
Owner/jitter callbacks have no context; clocks may be called under state locks.
Managed sleeper/cancellation closure need not produce Loss, and Stop never releases.

Canonical queue/scheduler callbacks share internal/guard: enabled renewal signals
loss, but disabled renewal has no automatic TTL watcher; cooperative callback
return precedes joined cleanup, and panics can skip explicit release. Service
renewal is detached from acquire request cancellation/values. Accounting retains
in-flight/rollback/owned entries until shared shutdown cleanup, even after manual
handle release; sequential cleanup is not one total caller-timeout budget.

Memory retains every distinct key/fence, accepts clock fault injection and resets
continuity on replacement. PostgreSQL retains separate fence rows and can consume
committed values during contention; Cleanup removes old inactive records only.
Valkey retains counter keys outside lease TTL and preserves exact decimal tokens
via server GET rather than Lua numeric conversion. Guard construction only checks
coordinate shape. Both native stores borrow clients and need operator-owned
permissions, trusted deployment/search path, persistence and continuity recovery.

Evidence: root hardening/context/managed/observation cases, all canonical/retained
adapter suites, reusable leasetest owner/token mismatch assertions, memory model,
provider fake/native response tests and environment-sensitive operational suites.
Private delayed-backend helpers model reservation/response ordering, not actual
service atomicity. The epoch/timestamp live oracle and fault harness were inspected,
not executed. Keep the original historical test name mentioning authenticated
Guard coordinates with an explicit shape-only clarification.

Missing versus unresolved: documentation gaps above are repaired. Retained runtime
follow-up: native stores accept fractional TTLs >=1ms and transmit their truncated
Milliseconds value, while local Handle budgets use the original TTL. Zero/small
margin can leave a sub-millisecond mismatch; comments recommend whole-millisecond
policy/granularity and do not claim an unconditional remote-expiry bound. No runtime
repair or live reproduction was performed. NewKey also joins strings before its
encoded-length check, so its returned-key bound is not hostile allocation admission.
Uncooperative callbacks, arbitrary Event/Wrap text, traversable raw error causes,
unkeyed key-hash guesses and deployment fence-history continuity remain explicit
caller/operator boundaries, not cryptographic/qualification guarantees.

## Variants, native generation and preserved evidence

The native API mode is apidiff at api/baseline.txt, not prose-bearing go-doc.
Inspect .golib.yaml plus verification/package.mk and scripts/check-docs.sh;
there is no source generator or snapshot rewrite for comments. Three main-package
examples exist, with protected-write assertions; no executable Example function
or TestMain is present. Root has two fuzz targets, memory one model target;
root has four benchmarks and PostgreSQL one cleanup benchmark. Live PostgreSQL/
Valkey tests require explicit environment/fault/continuity configuration and are
not covered by an unconfigured local test result. Retain all inline lint/security
and deprecated-package/type/function markers exactly.

The module declares no specification inventory or typed decision records. Preserve
backend SQL/Lua and test/fixture literals, native source policy/catalogs, API baselines,
module dependencies and any dated historical evidence. Required Make routes also
need PostgreSQL/Valkey and are not selected for this prose-only pass.

## Validation and final findings

Pinned Go 1.27.2 formatted changed source and parsed all 62 tracked files.
All position/comment-free executable ASTs match the base; every scanner token,
literal and implicit semicolon matches except exactly one parser-optional implicit
semicolon added by documenting/formatting the original one-line Observer interface.
That single insertion before its closing brace was proved separately; no broad
semicolon filtering or executable change was accepted. All inline directives,
lint/security rationale and deprecation markers match exactly.

There are 211 name-led public declarations/fields/interface methods, 16 canonical
package/command headers and 268 parsed HTML-rendered comment groups. Native go-doc
rendered every one of the 16 configured package boundaries; root output is 24,346
bytes. Final affected Valkey rendering was refreshed at 5,068 bytes after
ordinary wording correction. The apidiff baseline is untouched. Native source-docs
check, local-doc check and zero-record specification check passed. Module/catalog/policy/dependency/API
and SQL/Lua/test fixture identities are preserved. One initial strict token proof
exposed the optional interface semicolon and was narrowed only after AST proof.

No executable example changed, so no unchanged example/test selector, live backend,
partition/restore/failover/TLS/ACL harness, race, active fuzz, benchmark, mutation,
public-consumer or release campaign ran. Every owned GOCACHE/GOMODCACHE/GOTMPDIR,
helper and render output was removed. Read-only module-cache directory permissions
initially blocked deletion; only the exact owned session was repaired, then absence
verified. The serialized slot was handed directly to State Machine. Parent verifier/
scanner and historical evidence remain retained. Ordinary complete final diff,
affected links/secret inspection and Intent commit establish only local handoff,
not hosted CI or backend/durability qualification.

## Objective Enforcement

The checksum-pinned public Tools v2.4.0 verifier now supplies
`make source-docs-check` as a prerequisite of the existing local and CI Make
routes. Ordinary shared CI selects strict source-documentation checking and
requires its result. Native prerequisites, operation budgets, security and
release controls are retained. Hosted exact-head evidence is recorded by the
coordination ledger; configuration alone does not establish green delivery.
