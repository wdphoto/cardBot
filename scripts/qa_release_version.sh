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

history=$'v0.0.10\nv0.8.3\nv0.9.0'
check pass v0.10.0 0.10.0-dev "$history"
check pass v0.10.0 0.10.0-dev "$history"$'\nv0.10.0'
check pass v0.10.1 0.10.1-dev "$history"$'\nv0.10.0'
check pass v1.0.0 1.0.0-dev "$history"$'\nv0.99.99'
check pass v0.10.0 0.10.0-dev ''
check pass v0.10.0 0.10.0-dev "$history"$'\nv0.11.0-rc.1\nother-tag'
check fail v0.0.11 0.0.11-dev "$history"
check fail v0.10.0 0.10.0-dev "$history"$'\nv0.10.1'
check fail v0.10.0 0.10.0-dev 'v0.100.0'
check fail v0.11.0 0.10.0-dev "$history"
check fail v0.10.0 0.10.0 "$history"
check fail v0.10.0 v0.10.0-dev "$history"
check fail v0.10.0 0.10.0-dev.1 "$history"
for invalid in 0.10.0 v0.10 v0.010.0 v0.10.00 v0.10.0-rc.1 v0.10.0+build v0.10.0junk; do
  check fail "$invalid" "${invalid#v}-dev" "$history"
done

# Also validate the source default. Historical fixtures ensure this checkout
# cannot silently return to the pre-0.9 numbering line.
development_version="$(< "$script_dir/../VERSION")"
check pass "v${development_version%-dev}" "$development_version" "$history"
printf 'Release version policy checks passed\n'
