#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$TMP/home" "$TMP/bin"
cat >"$TMP/bin/cardbot" <<'EOF'
#!/usr/bin/env sh
echo "unrelated program"
EOF
chmod 755 "$TMP/bin/cardbot"

output="$(HOME="$TMP/home" PATH="$TMP/bin:/usr/bin:/bin" sh "$ROOT/scripts/uninstall.sh" --dry-run --no-sudo)"
case "$output" in
  *"PATH candidate is not cardBot"*) ;;
  *)
    printf 'uninstaller did not reject unrelated PATH candidate\n%s\n' "$output" >&2
    exit 1
    ;;
esac

HOME="$TMP/home" sh "$ROOT/scripts/install.sh" --dry-run --install-dir "$TMP/install" --no-sudo >/dev/null
printf 'installer/uninstaller safety QA passed\n'
