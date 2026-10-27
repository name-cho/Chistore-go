#!/usr/bin/env bash
set -euo pipefail

REPO="${CHISTORE_RELEASE_REPO:-name-cho/Chistore-go}"
BIN_DIR="${CHISTORE_BIN_DIR:-${XDG_BIN_HOME:-$HOME/.local/bin}}"

die() {
    printf '[x] %s\n' "$1" >&2
    exit 1
}

command -v curl >/dev/null 2>&1 || die "curl is required"

case "$(uname -s)" in
    Linux*)  os="linux" ;;
    Darwin*) os="darwin" ;;
    *)       die "unsupported OS: $(uname -s) (use install.ps1 on Windows)" ;;
esac

case "$(uname -m)" in
    x86_64|amd64)  arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *)             die "unsupported architecture: $(uname -m)" ;;
esac

asset="chistore_${os}_${arch}"
base="https://github.com/${REPO}/releases/latest/download"
mkdir -p "$BIN_DIR"

tmp="$(mktemp "${BIN_DIR}/.chistore.XXXXXX")"
sums="$(mktemp "${TMPDIR:-/tmp}/chistore-sums.XXXXXX")"
trap 'rm -f "$tmp" "$sums"' EXIT

printf '[i] Downloading %s\n' "$asset"
curl -fsSL "${base}/${asset}" -o "$tmp" || die "download failed"

if curl -fsSL "${base}/checksums.txt" -o "$sums" 2>/dev/null; then
    expected="$(awk -v name="$asset" '$2 == name { print $1; exit }' "$sums")"
    if [ -n "$expected" ]; then
        if command -v sha256sum >/dev/null 2>&1; then
            actual="$(sha256sum "$tmp" | awk '{print $1}')"
        elif command -v shasum >/dev/null 2>&1; then
            actual="$(shasum -a 256 "$tmp" | awk '{print $1}')"
        else
            actual=""
            printf '[!] sha256sum/shasum not found; skipping checksum verification\n' >&2
        fi
        if [ -n "$actual" ] && [ "$actual" != "$expected" ]; then
            die "checksum mismatch for $asset"
        fi
    fi
fi

chmod +x "$tmp"
mv -f "$tmp" "${BIN_DIR}/chistore"
printf '[+] Installed chistore to %s\n' "${BIN_DIR}/chistore"

case ":$PATH:" in
    *":${BIN_DIR}:"*) ;;
    *) printf '[!] Add %s to PATH to run chistore from anywhere\n' "$BIN_DIR" ;;
esac
