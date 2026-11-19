#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
work="$(cd "$work" && pwd -P)"
export HOME="$work/home" TMPDIR="$work/tmp" FIXTURE="$work"
export CARDBOT_STATE_DIR="$HOME/.cardbot" PATH="$work/bin:/usr/bin:/bin"
export REPO=wdphoto/cardBot VERSION=latest
mkdir -p "$HOME" "$TMPDIR" "$work/bin" "$work/origin" "$work/elsewhere/my bin"

# Redirect the uninstaller's two fixed legacy fallback candidates to an
# empty fixture tree. Even --dry-run probes candidates with --version.
# shellcheck disable=SC2016 # Expand FIXTURE in the copy, not this sed invocation.
sed -e 's|"/usr/local/bin/cardbot"|"$FIXTURE/legacy/usr/local/bin/cardbot"|g' \
    -e 's|"/opt/homebrew/bin/cardbot"|"$FIXTURE/legacy/opt/homebrew/bin/cardbot"|g' \
    "$ROOT/scripts/uninstall.sh" >"$work/uninstall.sh"

cat >"$work/bin/uname" <<'SH'
#!/bin/sh
case "$1" in -s) echo Linux;; -m) echo x86_64;; *) exit 1;; esac
SH
cat >"$work/binary" <<'SH'
#!/bin/sh
[ "$1" = --version ] && echo 'cardbot fixture'
SH
cat >"$work/bin/cardbot" <<'SH'
#!/bin/sh
echo 'unrelated program'
SH
# BSD mktemp -d without a template can ignore TMPDIR. Keep downloads in QA.
cat >"$work/bin/mktemp" <<'SH'
#!/bin/sh
if [ "$#" = 1 ] && [ "$1" = -d ]; then
  exec /usr/bin/mktemp -d "$FIXTURE/tmp/install.XXXXXX"
fi
exec /usr/bin/mktemp "$@"
SH
cat >"$work/bin/curl" <<'SH'
#!/bin/sh
set -eu
[ "$1" = -fsSL ] && [ "$2" = -o ]
case "$3" in "$FIXTURE"/tmp/*) ;; *) exit 1;; esac
case "$4" in
  */cardbot-linux-amd64) cp "$FIXTURE/binary" "$3";;
  */checksums.txt) cp "$FIXTURE/checksums" "$3";;
  *) exit 1;;
esac
SH
cat >"$work/bin/sudo" <<'SH'
#!/bin/sh
set -eu
# Simulate the elevated branch without privileges and only at the exact
# fixture destination. No sudo, process control or launchctl is executed.
case "$*" in
  "mkdir -p $FIXTURE/origin/new parent/bin"|"install -m 755 $FIXTURE/tmp/"*" $FIXTURE/origin/new parent/bin/cardbot") ;;
  *) exit 1;;
esac
printf '%s\n' "$1" >>"$FIXTURE/sudo-calls"
exec "$@"
SH
chmod +x "$work/bin/"* "$work/binary"
if command -v shasum >/dev/null; then sum="$(shasum -a 256 "$work/binary")"; else sum="$(sha256sum "$work/binary")"; fi
printf '%s  cardbot-linux-amd64\n' "${sum%% *}" >"$work/checksums"
record="$CARDBOT_STATE_DIR/install-path"

# A genuine-looking but unrelated cardbot at the same relative spelling.
cp "$work/binary" "$work/elsewhere/my bin/cardbot"
(cd "$work/origin" && sh "$ROOT/scripts/install.sh" --install-dir 'my bin' --no-sudo)
[[ "$(<"$record")" == "$work/origin/my bin/cardbot" && ! -e "$work/sudo-calls" ]]
(cd "$work/elsewhere" && sh "$work/uninstall.sh" --dry-run --no-sudo) >"$work/preview"
grep -q 'PATH candidate is not cardBot' "$work/preview"
grep -Fq "[dry-run] remove $work/origin/my bin/cardbot" "$work/preview"
[[ -f "$work/origin/my bin/cardbot" && -f "$record" ]]
(cd "$work/elsewhere" && sh "$work/uninstall.sh" --no-sudo)
[[ ! -e "$work/origin/my bin/cardbot" && ! -e "$record" && -f "$work/elsewhere/my bin/cardbot" ]]

# A legacy relative record is not authority; preserve it for recovery and
# let the user explicitly identify the correct installation.
cp "$work/binary" "$work/origin/my bin/cardbot"
printf 'my bin/cardbot\n' >"$record"
if (cd "$work/elsewhere" && sh "$work/uninstall.sh" --no-sudo) >"$work/legacy-log"; then
  echo 'legacy relative record unexpectedly accepted' >&2; exit 1
fi
grep -q 'Re-run with --install-dir' "$work/legacy-log"
[[ -f "$record" && -f "$work/origin/my bin/cardbot" && -f "$work/elsewhere/my bin/cardbot" ]]
(cd "$work/elsewhere" && sh "$work/uninstall.sh" --no-sudo --install-dir "$work/origin/my bin")
[[ ! -e "$work/origin/my bin/cardbot" && -f "$work/elsewhere/my bin/cardbot" ]]

# Missing intermediate parent exercises the current elevated branch even
# for privileged test users. Both branches must create the same record format.
(cd "$work/origin" && sh "$ROOT/scripts/install.sh" --install-dir 'new parent/bin')
[[ "$(<"$record")" == "$work/origin/new parent/bin/cardbot" ]]
grep -qx 'install' "$work/sudo-calls"

sh "$ROOT/scripts/install.sh" --dry-run --install-dir "$work/preview-only" --no-sudo >/dev/null
[[ ! -e "$work/preview-only" ]]
printf 'installer/uninstaller safety QA passed\n'
