#!/usr/bin/env bash
set -euo pipefail

source_root="$(pwd)"
mutation_root="$(mktemp -d)"

cleanup() {
  rm -rf "$mutation_root"
}
trap cleanup EXIT

prepare_case() {
  local name="$1"
  local case_root="$mutation_root/$name"
  mkdir -p "$case_root"
  tar --exclude=.git --exclude='.gremlins' -cf - -C "$source_root" . |
    tar -xf - -C "$case_root"
  printf '%s\n' "$case_root"
}

expect_killed() {
  local name="$1"
  local case_root="$2"
  local package="$3"
  local test_name="$4"
  if (cd "$case_root" && go test "$package" -run "^$test_name$" -count=1) \
    >"$mutation_root/$name.log" 2>&1; then
    echo "LIVED backend comparison mutation: $name" >&2
    cat "$mutation_root/$name.log" >&2
    exit 1
  fi
  echo "KILLED backend comparison mutation: $name"
}

case_root="$(prepare_case valkey-owner)"
sed -i.bak "/'owner'/s/~=/==/" "$case_root/valkey/scripts.go"
expect_killed valkey-owner "$case_root" ./valkey \
  TestScriptsUseBackendTimeAndAtomicComparisons

case_root="$(prepare_case valkey-token)"
sed -i.bak "/'token'/s/~=/==/" "$case_root/valkey/scripts.go"
expect_killed valkey-token "$case_root" ./valkey \
  TestScriptsUseBackendTimeAndAtomicComparisons

case_root="$(prepare_case postgres-owner)"
# shellcheck disable=SC2016 # Match the literal PostgreSQL placeholder $2.
sed -i.bak '/owner = \$2/s/owner = \$2/owner <> \$2/' \
  "$case_root/postgres/store.go"
expect_killed postgres-owner "$case_root" ./postgres \
  TestContinuationSQLComparesOwnerAndToken

case_root="$(prepare_case postgres-token)"
# shellcheck disable=SC2016 # Match the literal PostgreSQL placeholder $3.
sed -i.bak \
  '/fencing_token = \$3/s/fencing_token = \$3/fencing_token <> \$3/' \
  "$case_root/postgres/store.go"
expect_killed postgres-token "$case_root" ./postgres \
  TestContinuationSQLComparesOwnerAndToken
