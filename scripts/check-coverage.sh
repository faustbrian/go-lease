#!/usr/bin/env bash
set -euo pipefail

profile="${TMPDIR:-/tmp}/lease-coverage.out"
packages="$(go list ./... | grep -v '/leasetest$' | grep -v '/examples/' | paste -sd, -)"
go test ./... -coverpkg="$packages" -coverprofile="$profile"
total="$(go tool cover -func="$profile" | awk '/^total:/ {print $3}')"
if [[ "$total" != "100.0%" ]]; then
  echo "meaningful production statement coverage: $total (required 100.0%)" >&2
  exit 1
fi
echo "meaningful production statement coverage: $total"
