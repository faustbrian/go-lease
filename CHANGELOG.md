# Changelog

## Unreleased

### Changed

- Remove unused CLI-related indirect dependencies from canonical module
  metadata.
- Pin owned sibling modules to exact resolvable main pseudo-versions so
  standalone and clean external consumers use immutable dependency content.
- Refresh owned-module checksums against the final consolidated archives.
- Normalized standalone module metadata against the canonical owned dependency
  graph, including complete checksums for clean consumer resolution.

### Added

- Opaque Valkey ownership guards for atomic protected cache publication without
  exposing logical lease identities.
- Handle ownership snapshots for composing bounded client acquisition with
  protected adapters without reconstructing lease records.
- Fenced lease model with bounded acquisition and managed renewal.
- Deterministic memory reference backend.
- Native Valkey 9 backend with atomic server-time scripts.
- Native PostgreSQL backend with durable fence history and migrations.
- Queue, scheduler, service, and redacted observation integrations.
- Conformance, race, fuzz, mutation, benchmark, coverage, and operations gates.
- Disposable mutation checks for Valkey and PostgreSQL identity predicates.
- Restart-continuity and destructive-reset phases in the backend fault gate.
- Snapshot-rollback detection and Valkey replica-promotion evidence.
- Physical PostgreSQL replica-promotion fencing evidence.
- Valkey TLS trust, named ACL, and password rotation fault evidence.
- PostgreSQL transaction, cleanup, pool, isolation, and rolling-schema faults.
- Compatible and fail-closed incompatible Valkey rolling-script evidence.
- Direct queue and scheduler cancellation-on-loss evidence.
- Race-tested transactional protected-write fencing example.
- Blocking shuffled lifecycle stress gate and report.
- Disposable PostgreSQL replica authentication for the failover gate.
- Eager Valkey TLS rejection handling in the credential-rotation gate.
- Formal acquisition, handle, successor, and continuity epoch contracts.
- Full mutation and backend-fault gates before tagged release publication.

### Fixed

- Validate the binary API export baseline with the canonical `apidiff` gate
  instead of interpreting it as a text symbol list.
- Pin internal golib dependencies to immutable source revisions so clean
  consumers do not depend on rewritten or missing module tags.
- Bound acquisition waits independently from injectable clock anomalies.
- Prevent observer re-entrancy from deadlocking handle state transitions.
- Reject corrupt or incompatible successful responses that change ownership.
- Redact backend cause text while preserving programmatic error identity.
- Isolate blocking observers behind bounded best-effort callback slots.
- Report PostgreSQL fence exhaustion as a permanent unavailable state.
- Bound handle admission with a conservative local monotonic deadline.

## 1.0.0 - 2026-07-16

Release-ready local implementation and API baseline. Tag publication follows
the final hosted verification performed by the maintainer.
