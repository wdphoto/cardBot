#!/usr/bin/env bash
# Validate a stable release tag against VERSION and tag names from the release remote.
# In a clean checkout: git tag --list | bash scripts/check-release-version.sh v0.0.11 "$(< VERSION)"
set -euo pipefail

fail() {
  printf 'Release version rejected: %s\n' "$*" >&2
  exit 1
}

[ "$#" -eq 2 ] || fail 'expected release tag and development version; existing tags must be supplied on stdin'
tag="$1"
development_version="$2"
stable_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

[[ "$tag" =~ $stable_pattern ]] || fail 'release workflow accepts only stable vMAJOR.MINOR.PATCH tags'
[[ "$development_version" == *-dev ]] || fail 'VERSION must end in -dev'
[[ "$tag" == "v${development_version%-dev}" ]] || fail "$tag does not match VERSION ($development_version)"

# These exact public tags predate v0.0.10. Their names stay reserved, but they
# do not set the floor for new releases. Do not ignore whole version ranges.
legacy_tag() {
  case "$1" in
    v0.1.2|v0.8.0|v0.8.1|v0.8.2|v0.8.3|v0.9.0) return 0 ;;
    *) return 1 ;;
  esac
}

baseline=v0.0.10
if [[ "$tag" == "$baseline" ]] || legacy_tag "$tag"; then
  fail "$tag is a reserved historical tag"
fi

# A list missing the known baseline is incomplete; fail closed. Include
# the candidate for a preflight; an equal active tag is fine on a workflow rerun.
found_baseline=false
active=("$tag")
while IFS= read -r existing || [[ -n "$existing" ]]; do
  if [[ "$existing" == "$baseline" ]]; then found_baseline=true; fi
  if [[ "$existing" =~ $stable_pattern ]] && ! legacy_tag "$existing"; then
    active+=("$existing")
  fi
done
[[ "$found_baseline" == true ]] || fail "tag list is missing $baseline; supply all tags from the release remote"
highest=$(printf '%s\n' "${active[@]}" | LC_ALL=C sort -V | tail -n 1)
[[ "$tag" == "$highest" ]] || fail "$tag is older than active stable tag $highest"
printf 'Release version accepted: %s\n' "$tag"
