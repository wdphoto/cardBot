#!/usr/bin/env bash
# Bounded, synthetic-only fuzzing. No fuzz input is opened as a card path.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
export GOMAXPROCS=2

for target in \
  cardcopy:FuzzDestinationAndNaming \
  cardcopy:FuzzVerifyBytes \
  dotfile:FuzzIngestHistory \
  update:FuzzVersions \
  update:FuzzChecksums; do
  package="${target%%:*}"
  fuzz="${target#*:}"
  printf '\nFuzzing %s/%s (20s, two workers)\n' "$package" "$fuzz"
  go test "./$package" -run '^$' -fuzz "^${fuzz}$" \
    -fuzztime=20s -parallel=2 -timeout=60s
done
