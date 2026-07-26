# Hardening Goal: Fenced Distributed Leases

## Objective

Prove that `lease` prevents stale-owner corruption and successor deletion
under contention, pauses, clock anomalies, backend failure, failover, restore,
network uncertainty, cancellation, and rolling deployments.

## Required Audits

### State And Fencing Audit

- Model every acquisition, contention, renewal, expiry, loss, validation,
  release, and successor transition.
- Mutation-test owner and token comparisons capable of accepting stale work.
- Prove fencing tokens remain monotonic within documented continuity bounds.
- Verify stale handles cannot affect a successor after every failure point.

### Timing And Concurrency Audit

- Inject clock rollback/jump, scheduler delay, process pause, stop-the-world-like
  delay, network latency, frozen time, and late responses.
- Race and stress-test acquisition, managed renewal, explicit renewal, release,
  cancellation, loss notification, and shutdown.
- Prove callbacks and observations cannot deadlock lease state transitions.
- Enforce wait, retry, jitter, goroutine, and renewal safety margins.

### Backend Failure Audit

- Exercise Valkey failover, restart, `NOSCRIPT`, ACL/TLS rotation, disconnect,
  restore, flush, and ambiguous write outcomes.
- Exercise PostgreSQL failover, deadlocks, transaction abort, pool churn,
  isolation, sequence behavior, restore, and cleanup races.
- Document exactly when fencing continuity can reset and how operators detect it.
- Test rolling clients with compatible and incompatible schema/script versions.

### Security And Integration Audit

- Threat-model split brain, stale writer, replay, owner spoofing, key collision,
  denial of service, token overflow, and sensitive key leakage.
- Prove queue and scheduler integrations stop ownership-sensitive work on loss.
- Prove protected-write examples reject stale fencing tokens transactionally.
- Verify errors, logs, metrics, traces, and inspection output redact identifiers.

## Required Deliverables

- Formal state machine, fencing proof matrix, and backend continuity contract.
- Threat model, failure matrix, resource budgets, and hardening findings.
- Valkey/PostgreSQL failover, restore, fault, and rolling-upgrade evidence.
- Race, fuzz, mutation, stale-owner, and benchmark reports.
- Updated API, operations, security, migration, FAQ, and troubleshooting docs.

## Release Blockers

- Any stale owner that can renew, release, or commit a protected write.
- Any successor lease deleted by an earlier handle.
- Any undocumented fencing reset, ambiguous ownership treated as valid, race,
  deadlock, panic, leaked renewal goroutine, or unbounded wait/retry.
- Missing meaningful 100% coverage, mutation evidence, or green blocking CI.

## Completion Criteria

- State, fencing, timing, backend, restore, and integration suites pass.
- Every ambiguity fails ownership closed and is observable.
- Race, fuzz, vulnerability, compatibility, and performance gates pass.
- NilAway runs visibly as advisory without blocking findings.
- No release blocker remains and the changelog is current.
