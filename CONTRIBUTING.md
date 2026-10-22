# Contributing

## Before Editing

1. Read [`AGENTS.md`](AGENTS.md) and the affected module's docs.
2. Run standalone Go tests in the affected module.
3. Identify owned dependencies and reverse dependants in `modules.json`.
4. Preserve unrelated work and generated/corpus provenance.

## Changes

Keep commits focused and conventional. Update every affected changelog with
the behavior and migration impact. Public API changes require compatibility
evidence and documentation. Specification behavior requires a decision record,
fixture coverage, and interoperability evidence.

New direct dependencies and dependency updates must follow the
[dependency governance policy](AGENTS.md#dependencies-and-supply-chain). Package-local
update bots are forbidden; the root policy owns every module and action update.

Repository verification is provided by the released `go-library-tools` CLI.
Keep package-specific policy in `modules.json`, `packages.json`,
the repository configuration, and `verification/package.mk`; do not recreate
shared tooling in the consumer repository.

Specification-backed changes must follow the
[specification governance contract](AGENTS.md#design), update
the affected stable decision entries, and complete the Specification Decisions
section of the pull request template. An unresolved interpretation or stale
source pin is release-blocking; peer behavior cannot silently select policy.

Full scheduled and release mutation gates must finish with zero surviving viable
mutants. Local mutation is optional, not a universal contribution prerequisite.

Do not add package-local workflows, permanent replacements, machine-specific
paths, bypass flags, broad mutation exclusions, or aggregate quality metrics
that hide a failing package.

## Verification

Use Go 1.27.2 for development and CI; the module minimum remains Go 1.27.0.
Root tests need no sibling checkout or ecosystem ledger:

```bash
GOWORK=off go test ./...
```

Ordinary tests run deterministic fuzz seeds and saved corpus regressions, not
active exploration. Direct Go tests skip live backend cases without
`POSTGRES_URL` or `VALKEY_ADDRESS` and the separate fault/continuity settings.
The manifest requires PostgreSQL and Valkey for the verifier's Make routes,
including `--local`; there are no test/build tags. An unconfigured Go test pass
is not live backend, partition, restore, failover or TLS/ACL qualification.

With the checksum-pinned public verifier selected by `.golib.yaml`, use:

```bash
make inventory
make check
make ci
```

Both Make routes use bounded local checks. Base-free checks run seeds/corpora;
changed-source CI selects bounded active smoke for causally affected targets.
Ordinary CI consumes the public verifier without building it from source. One
race command selects the acquire/shutdown reservation test in `./adapters/service`,
once with a two-minute bound: cancellation returns typed errors, cleanup owns
the reservation until release, and shutdown leaves no active handle or renewal.
The deterministic backend owner/token comparison tests remain ordinary tests.

Short active exploration selects one target; optional local mutation selects
one package:

```bash
GOMAXPROCS=2 GOWORK=off go test -run '^$' -fuzz '^FuzzKeyParsing$' -fuzztime=2s -parallel=1 -timeout=2m .
golib mutation --module . --package ./memory
```

`make check-full` retains the 12 selected fencing/lifecycle tests at count 25,
four copied-source backend comparison mutants, three native 10000x fuzz
campaigns, 100ms benchmarks, docs/examples and full coverage, race, mutation,
API and security qualification. Live backend hardening is the separate explicit
`make -f verification/package.mk backend-hardening` campaign; run it only in a
disposable environment as described in [failover](docs/failover.md).
Fencing, acquisition, renewal, cancellation and adapter changes retain their
affected state-machine, refusal, cleanup and live backend assertions. Public
API changes retain compatibility and directly affected public consumers.
Release rehearsal remains `golib release dry-run --module .`. Report unavailable,
skipped and failed boundaries honestly; local tests are not hosted-CI or
public-release qualification.

## Adding A Module

Follow [repository structure policy](AGENTS.md#repository-structure). New modules
require an explicit purpose, ownership boundary, dependency review, package
catalog entry, full quality gates, documentation, changelog, license, security
policy, compatibility plan, and release dry-run.

## Source Documentation

Install the checksum-pinned public verifier declared in `.golib.yaml`.
`make source-docs-check` validates objective Go comment, marker, link and
generated-producer structure; `make check`, `make check-full` and `make ci`
require it before their existing checks. Shared CI runs the same strict
contract and includes its result in Required. This is structural enforcement,
not a substitute for source-truth review or behavioral verification.
