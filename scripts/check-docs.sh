#!/usr/bin/env bash
set -euo pipefail

required=(
  README.md CHANGELOG.md SECURITY.md CONTRIBUTING.md
  docs/api.md docs/audit-evidence.md docs/backend-guarantees.md docs/failure-matrix.md
  docs/benchmark-baseline.md docs/compatibility.md docs/fuzz-report.md
  docs/faq.md docs/fencing.md docs/hardening.md docs/kubernetes.md
  docs/laravel-migration.md docs/migrations.md docs/operations.md
  docs/mutation-report.md
  docs/performance.md docs/protected-writes.md docs/quickstart-postgres.md
  docs/quickstart-valkey.md docs/renewal-and-loss.md
  docs/resource-budgets.md docs/schedulers.md docs/shutdown.md
  docs/state-machine.md docs/race-stress-report.md docs/threat-model.md
  docs/troubleshooting.md
  docs/unique-jobs.md
)
for file in "${required[@]}"; do
  test -s "$file" || { echo "missing documentation: $file" >&2; exit 1; }
done
go test ./examples/...
if rg -n 'exactly-once|guaranteed fairness' README.md docs; then
  echo "unsupported guarantee found in documentation" >&2
  exit 1
fi
