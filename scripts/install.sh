#!/usr/bin/env sh
set -eu

REPO="${REPO:-wdphoto/cardBot}"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
DEFAULT_INSTALL_DIR="/usr/local/bin"
STATE_DIR="${CARDBOT_STATE_DIR:-${HOME}/.cardbot}"
INSTALL_RECORD="${STATE_DIR}/install-path"
NO_SUDO=0
DRY_RUN=0
EXPLICIT_INSTALL_DIR=0

usage() {
  cat <<'EOF'
cardBot installer

Usage:
  sh install.sh [options]

Options:
  --version <version>     Install a specific version (example: v0.0.10). Default: latest
  --install-dir <path>    Install directory. Default: /usr/local/bin
  --repo <owner/repo>     GitHub repo. Default: wdphoto/cardBot
  --no-sudo               Do not attempt sudo for protected install dirs
  --dry-run               Print actions without installing
  -h, --help              Show help

Examples:
  sh install.sh
  sh install.sh --version v0.0.10
  sh install.sh --install-dir "$HOME/.local/bin" --no-sudo
EOF
}

say() {
  printf '%s\n' "$*"
}

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

normalize_version() {
  v="$1"
  if [ "$v" = "latest" ]; then
    printf '%s' "$v"
    return
  fi
  case "$v" in
    v*) printf '%s' "$v" ;;
    *) printf 'v%s' "$v" ;;
  esac
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || die "--version requires a value"
      VERSION="$2"
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || die "--install-dir requires a value"
      INSTALL_DIR="$2"
      EXPLICIT_INSTALL_DIR=1
      shift 2
      ;;
    --repo)
      [ "$#" -ge 2 ] || die "--repo requires a value"
      REPO="$2"
      shift 2
      ;;
    --no-sudo)
      NO_SUDO=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown option: $1 (use --help)"
      ;;
  esac
done

need_cmd uname
need_cmd curl
need_cmd mktemp
need_cmd install
need_cmd awk
need_cmd grep

if command -v shasum >/dev/null 2>&1; then
  CHECKSUM_CMD="shasum"
elif command -v sha256sum >/dev/null 2>&1; then
  CHECKSUM_CMD="sha256sum"
else
  die "missing checksum tool (need shasum or sha256sum)"
fi

OS_RAW="$(uname -s)"
ARCH_RAW="$(uname -m)"

case "$OS_RAW" in
  Darwin) OS="darwin" ;;
  Linux) OS="linux" ;;
  *) die "unsupported OS: $OS_RAW" ;;
esac

case "$ARCH_RAW" in
  arm64|aarch64) ARCH="arm64" ;;
  x86_64|amd64) ARCH="amd64" ;;
  *) die "unsupported architecture: $ARCH_RAW" ;;
esac

VERSION="$(normalize_version "$VERSION")"
ASSET="cardbot-${OS}-${ARCH}"

BASE_URL="https://github.com/${REPO}/releases"
if [ "$VERSION" = "latest" ]; then
  BIN_URL="${BASE_URL}/latest/download/${ASSET}"
  SUM_URL="${BASE_URL}/latest/download/checksums.txt"
else
  BIN_URL="${BASE_URL}/download/${VERSION}/${ASSET}"
  SUM_URL="${BASE_URL}/download/${VERSION}/checksums.txt"
fi

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t cardbot-install)"
# shellcheck disable=SC2329 # invoked indirectly by the trap below
cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

BIN_PATH="${TMP_DIR}/${ASSET}"
SUM_PATH="${TMP_DIR}/checksums.txt"

say "==> cardBot installer"
say "Repo: ${REPO}"
say "Version: ${VERSION}"
say "Asset: ${ASSET}"
say "Install dir: ${INSTALL_DIR}"

if [ "$DRY_RUN" -eq 1 ]; then
  say ""
  say "[dry-run] Would download: ${BIN_URL}"
  say "[dry-run] Would verify with: ${SUM_URL}"
  say "[dry-run] Would install to: ${INSTALL_DIR}/cardbot"
  say "[dry-run] Would record install at: ${INSTALL_RECORD}"
  exit 0
fi

record_install() {
  installed_path="$1"
  mkdir -p "$STATE_DIR"
  chmod 700 "$STATE_DIR"
  record_tmp="${INSTALL_RECORD}.tmp.$$"
  printf '%s\n' "$installed_path" >"$record_tmp"
  chmod 600 "$record_tmp"
  mv "$record_tmp" "$INSTALL_RECORD"
}

say "==> Downloading binary"
curl -fsSL -o "$BIN_PATH" "$BIN_URL"

say "==> Downloading checksums"
curl -fsSL -o "$SUM_PATH" "$SUM_URL"

EXPECTED_SUM="$(awk -v f="$ASSET" '{name=$2; gsub(/^\*/, "", name); if (name==f) {print $1; exit}}' "$SUM_PATH")"
[ -n "$EXPECTED_SUM" ] || die "could not find checksum for ${ASSET}"

if [ "$CHECKSUM_CMD" = "shasum" ]; then
  ACTUAL_SUM="$(shasum -a 256 "$BIN_PATH" | awk '{print $1}')"
else
  ACTUAL_SUM="$(sha256sum "$BIN_PATH" | awk '{print $1}')"
fi

[ "$ACTUAL_SUM" = "$EXPECTED_SUM" ] || die "checksum mismatch for ${ASSET}"
say "==> Checksum verified"

install_to_dir() {
  target_dir="$1"
  target_bin="${target_dir}/cardbot"
  mkdir -p "$target_dir"
  install -m 755 "$BIN_PATH" "$target_bin"
  record_install "$target_bin" || say "Warning: could not record install path at ${INSTALL_RECORD}"
  say "==> Installed: ${target_bin}"
  "$target_bin" --version || true
  case ":$PATH:" in
    *":${target_dir}:"*) ;;
    *)
      say ""
      say "Note: ${target_dir} is not currently in PATH"
      say "Add this to your shell config:"
      say "  export PATH=\"${target_dir}:\$PATH\""
      ;;
  esac
}

if [ -d "$INSTALL_DIR" ] && [ -w "$INSTALL_DIR" ]; then
  install_to_dir "$INSTALL_DIR"
  exit 0
fi

if [ ! -d "$INSTALL_DIR" ] && [ -w "$(dirname "$INSTALL_DIR")" ]; then
  install_to_dir "$INSTALL_DIR"
  exit 0
fi

if [ "$NO_SUDO" -eq 0 ] && command -v sudo >/dev/null 2>&1; then
  say "==> ${INSTALL_DIR} requires elevated permissions; attempting sudo install"
  if sudo mkdir -p "$INSTALL_DIR" && sudo install -m 755 "$BIN_PATH" "${INSTALL_DIR}/cardbot"; then
    record_install "${INSTALL_DIR}/cardbot" || say "Warning: could not record install path at ${INSTALL_RECORD}"
    say "==> Installed: ${INSTALL_DIR}/cardbot"
    "${INSTALL_DIR}/cardbot" --version || true
    exit 0
  fi
  say "==> sudo install failed"
fi

if [ "$INSTALL_DIR" = "$DEFAULT_INSTALL_DIR" ] && [ "$EXPLICIT_INSTALL_DIR" -eq 0 ]; then
  FALLBACK_DIR="${HOME}/.local/bin"
  say "==> Falling back to user install: ${FALLBACK_DIR}"
  install_to_dir "$FALLBACK_DIR"
  exit 0
fi

die "could not install to ${INSTALL_DIR}; try --install-dir \"$HOME/.local/bin\" --no-sudo"
