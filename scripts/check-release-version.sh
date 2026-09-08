#!/usr/bin/env bash
# Validate a stable release tag against VERSION and existing tags on stdin.
# Example: git tag --list | bash scripts/check-release-version.sh v0.10.0 "$(< VERSION)"
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

# Include the candidate so an initial release needs no special case. Ignore
# non-stable tags; the release workflow does not publish prereleases. The current
# tag may already exist (or this may be a workflow rerun), so equality is valid.
highest=$(
  {
    printf '%s\n' "$tag"
    while IFS= read -r existing; do
      if [[ "$existing" =~ $stable_pattern ]]; then
        printf '%s\n' "$existing"
      fi
    done
  } | LC_ALL=C sort -V | tail -n 1
)
[[ "$tag" == "$highest" ]] || fail "$tag is older than existing stable tag $highest"
printf 'Release version accepted: %s\n' "$tag"
