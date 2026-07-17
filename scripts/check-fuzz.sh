#!/usr/bin/env bash
set -euo pipefail

duration="${1:-2s}"
go test . -run '^$' -fuzz '^FuzzKeyParsing$' -fuzztime="$duration"
go test . -run '^$' -fuzz '^FuzzPolicyBounds$' -fuzztime="$duration"
go test ./memory -run '^$' -fuzz '^FuzzLeaseStateModel$' -fuzztime="$duration"
