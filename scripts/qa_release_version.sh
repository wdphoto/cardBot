#!/usr/bin/env bash
# Pure validation fixtures: no tags, commits, releases, or network writes.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
checker="$script_dir/check-release-version.sh"

check() {
  local expected="$1" tag="$2" version="$3" tags="$4" actual output
  if output=$(printf '%s\n' "$tags" | bash "$checker" "$tag" "$version" 2>&1); then
    actual=pass
  else
    actual=fail
  fi
  if [[ "$actual" != "$expected" ]]; then
    printf 'FAIL: %s with %s: expected %s, got %s\n%s\n' "$tag" "$version" "$expected" "$actual" "$output" >&2
    exit 1
  fi
}

# The actual public baseline includes six higher, pre-baseline tags. No
# unrecognised tag is exempt, even if it is next to one of the reserved names.
legacy=(v0.1.2 v0.8.0 v0.8.1 v0.8.2 v0.8.3 v0.9.0)
history=$(printf '%s\n' v0.0.10 "${legacy[@]}")
check pass v0.0.11 0.0.11-dev "$history"
check pass v0.0.11 0.0.11-dev "$history"$'\nv0.0.11' # workflow rerun
check pass v0.0.12 0.0.12-dev "$history"$'\nv0.0.11'
check pass v0.1.0 0.1.0-dev "$history"$'\nv0.0.11\nv0.0.12'
check pass v0.1.3 0.1.3-dev "$history"$'\nv0.1.0\nv0.1.1' # v0.1.2 stays reserved
check pass v1.0.0 1.0.0-dev "$history"$'\nv0.99.99'
check pass v0.0.11 0.0.11-dev "$history"$'\nv0.1.0-rc.1\nother-tag'

for reserved in v0.0.10 "${legacy[@]}"; do
  check fail "$reserved" "${reserved#v}-dev" "$history"
done
check fail v0.0.9 0.0.9-dev "$history"
check fail v0.0.11 0.0.11-dev "$history"$'\nv0.0.12'
check fail v0.0.11 0.0.11-dev "$history"$'\nv0.1.0'
check fail v0.1.0 0.1.0-dev "$history"$'\nv0.8.4'
check fail v0.1.0 0.1.0-dev "$history"$'\nv0.9.1'
check fail v0.0.11 0.0.11-dev ''
check fail v0.0.11 0.0.11-dev 'v0.9.0'
check fail v0.10.0 0.0.11-dev "$history"
check fail v0.0.11 0.0.11 "$history"
check fail v0.0.11 v0.0.11-dev "$history"
check fail v0.0.11 0.0.11-dev.1 "$history"
for invalid in 0.0.11 v0.11 v0.00.11 v0.0.011 v0.0.11-rc.1 v0.0.11+build v0.0.11junk; do
  check fail "$invalid" "${invalid#v}-dev" "$history"
done

# The checked-in development version must also produce a valid candidate.
development_version="$(< "$script_dir/../VERSION")"
check pass "v${development_version%-dev}" "$development_version" "$history"
printf 'Release version policy checks passed\n'
