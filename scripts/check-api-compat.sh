#!/usr/bin/env bash
set -euo pipefail

while read -r package symbol; do
  [[ -z "$package" || "$package" == \#* ]] && continue
  go doc "$package.$symbol" >/dev/null
done < api/baseline.txt
