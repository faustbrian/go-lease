#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
exec "${root}/scripts/check-api-baseline.sh" pkg/lease
