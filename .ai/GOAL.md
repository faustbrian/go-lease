# Goal: Fenced Distributed Leases

## Objective

Build a production-grade distributed lease package with explicit ownership,
expiry, renewal, release, and monotonically increasing fencing tokens for
Valkey and PostgreSQL.

The package MUST provide the coordination primitive required by unique queue
jobs, scheduler overlap prevention, single-owner maintenance work, and bounded
distributed leadership without pretending that an ordinary cache lock provides
stronger guarantees than the backend can deliver.

## Product Principles

- A lease is time-bounded ownership, not an indefinitely held mutex.
- Every successful acquisition returns an opaque owner identity and fencing
  token that protected resources can use to reject stale owners.
- Expiry, renewal, loss, release, and backend uncertainty are distinct states.
- No process may assume ownership after its lease deadline or uncertain renewal.
- Backend guarantees and clock assumptions are explicit and testable.
- Context cancellation and shutdown never imply successful remote release.

## Core Model

- Typed, bounded, namespaced lease keys.
- Immutable acquisition policy: TTL, wait, retry, jitter, renewal, and failure
  behavior.
- Lease handle exposing owner, fencing token, acquired time, deadline, state,
  renew, validate, and release.
- `TryAcquire`, bounded `Acquire`, explicit `Renew`, and idempotent `Release`.
- Optional managed renewal with explicit goroutine ownership and loss channel.
- Stable errors for contention, timeout, cancellation, lost lease, stale owner,
  backend unavailable, invalid state, and ambiguous outcome.
- Test clock and deterministic retry source without production global state.

## Correctness Semantics

- Fencing tokens MUST be monotonically increasing for a key within documented
  backend continuity guarantees.
- Renewal MUST compare owner identity and current token atomically.
- Release MUST compare ownership atomically and never delete a successor lease.
- A stale handle MUST never renew or release a newer owner's lease.
- TTL and safety margin account for network delay, pauses, scheduling, and
  backend clock behavior.
- Acquisition fairness is explicitly documented; no unsupported fairness claim.
- Multi-key atomic leases are out of scope unless a proven backend transaction
  model and deadlock policy are added later.

## Valkey Adapter

- Native `valkey-go` implementation using atomic server-side scripts/functions.
- Cluster-safe key layout, script loading, `NOSCRIPT`, failover, reconnect,
  timeout, ACL, TLS, and rolling-version behavior.
- Backend/server time SHOULD anchor expiry semantics where practical.
- Define fencing continuity after failover, restore, flush, or data loss.
- No Redlock claims or multi-independent-master algorithm by default.

## PostgreSQL Adapter

- Native `pgx` implementation with transactional acquisition and renewal.
- Durable lease row and monotonic fencing sequence semantics.
- Indexed schema, cleanup, contention, isolation, deadlock, failover, and
  connection-loss behavior.
- Migrations owned through `go-migrations`.
- PostgreSQL advisory locks MAY be evaluated separately but MUST NOT be confused
  with durable TTL leases or reused across pooled sessions unsafely.

## Integration

- `go-queue` middleware for unique jobs and non-overlapping handlers.
- `go-scheduler` adapter for `onOneServer` and `withoutOverlapping` semantics.
- `go-idempotency` MAY consume lease/fencing primitives where its stronger
  operation state machine remains intact.
- `go-service` lifecycle integration for managed renewal and shutdown.
- Optional `go-log` and `go-telemetry` observations with hashed bounded keys.
- Protected-resource examples MUST demonstrate fencing checks; acquiring a
  lease alone is not sufficient safety documentation.

## Security And Resource Bounds

- Cryptographically random owner identities with injectable test source.
- Bounded key, owner, waiters, retry attempts, renewal goroutines, observations,
  cleanup batches, and backend operations.
- Keys and owner identities MUST not leak through default logs or metric labels.
- Threat-model stale writers, split brain, clock anomalies, replay, token
  overflow, key collision, backend rollback, restore, and malicious contention.
- Callbacks MUST not execute while internal locks are held.

## Non-Goals

- No distributed transaction, consensus system, membership service, election
  platform, semaphore, idempotency state machine, or queue.
- No guarantee that expired work stopped; fencing is required for protected
  resources when stale work is dangerous.
- No hidden infinite waiting or retry.
- No in-memory adapter presented as distributed.
- No Redis/Valkey compatibility through one ambiguous adapter.

## Package Shape

- Root: keys, policies, handles, states, errors, retry, observations.
- `memory`: deterministic process-local reference and tests only, clearly scoped.
- `valkey` and `postgres`: native distributed adapters.
- `leasequeue`, `leasescheduler`, and `leaseservice`: integrations.
- `leasetest`: conformance, clocks, fault injection, and fencing assertions.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Required evidence:

- state-machine and model-based tests for acquisition through final release
- cross-backend lease and fencing conformance
- race/stress tests for contention, renewal, loss, cancellation, and shutdown
- Valkey and PostgreSQL failover, restart, timeout, partition, and fault tests
- stale-owner and successor-protection tests at every operation boundary
- clock skew/jump, process pause, token overflow, and retry fuzzing
- mutation testing of ownership comparisons and stale-owner rejection
- benchmarks for contention, renewal load, latency, allocations, and cleanup

## Documentation Deliverables

- Five-minute Valkey and PostgreSQL quickstarts.
- Formal state machine, fencing model, backend guarantees, and API reference.
- Guides for unique jobs, schedulers, protected writes, renewal, loss handling,
  shutdown, Kubernetes, failover, and migrations.
- Laravel lock/unique-job migration guide, threat model, operations runbook,
  performance, FAQ, troubleshooting, examples, and changelog.
- Every exported API and user-facing scenario MUST be documented.

## Automation And Release

Use the latest stable Go release as the minimum at implementation time. Pin all
tools and dependencies. GitHub Actions MUST run formatting, vet, Staticcheck,
strict golangci-lint, advisory NilAway, tests, meaningful 100% coverage, race,
fuzz smoke, mutation checks, Valkey/PostgreSQL matrices, vulnerability scans,
benchmarks, docs, API compatibility, and releases. All blocking commands MUST
be locally reproducible through documented `make` targets.

## Execution Plan

1. Specify states, owner identity, fencing, timing, errors, and conformance.
2. Implement deterministic reference behavior and native Valkey adapter.
3. Implement PostgreSQL adapter and migration contract.
4. Add queue, scheduler, service, logging, and telemetry integrations.
5. Complete failover, stale-owner, race, mutation, and performance hardening.
6. Publish complete operational documentation and release v1.

## Acceptance Criteria

- Stale owners cannot renew, release, or overwrite protected successor work.
- Every backend satisfies the documented fencing and lease state machine.
- Lease uncertainty and loss always stop ownership-dependent admission.
- Resource use, retries, wait, renewal, and shutdown are bounded.
- Meaningful 100% coverage and every required CI gate pass.
