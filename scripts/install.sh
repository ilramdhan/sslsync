#!/bin/sh
# sslsync installer — https://github.com/ilramdhan/sslsync
#
#   curl -fsSL https://ilramdhan.github.io/sslsync/install.sh | sh
#
# Options (environment variables):
#   SSLSYNC_VERSION=v1.2.3      install this version instead of the latest
#   SSLSYNC_INSTALL_DIR=~/bin   install here instead of /usr/local/bin (or ~/.local/bin)
#
# What it does: detects your OS/CPU, downloads the matching archive from the
# GitHub release, verifies its SHA-256 against checksums.txt, and copies the
# binary into place. Nothing else is changed. Read it before running it.
set -eu

REPO="ilramdhan/sslsync"
BIN="sslsync"

say()  { printf '%s\n' "$*"; }
fail() { printf 'install: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "'$1' is required but not installed"; }

need uname
need tar
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --retry 3 "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO- "$1"; }
else
  fail "curl or wget is required"
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  fail "sha256sum or shasum is required to verify the download"
fi

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) fail "unsupported OS $(uname -s) — on Windows download the .zip from https://github.com/$REPO/releases" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported CPU $(uname -m)" ;;
esac

version="${SSLSYNC_VERSION:-}"
if [ -z "$version" ]; then
  version=$(fetch "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
  [ -n "$version" ] || fail "could not find the latest release (GitHub API rate limit? set SSLSYNC_VERSION=vX.Y.Z)"
fi
case "$version" in v*) ;; *) version="v$version" ;; esac

archive="${BIN}_${version#v}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading $BIN $version for $os/$arch …"
fetch "$base/$archive" > "$tmp/$archive" || fail "download failed: $base/$archive"
fetch "$base/checksums.txt" > "$tmp/checksums.txt" || fail "download failed: $base/checksums.txt"

want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
[ -n "$want" ] || fail "$archive is not listed in checksums.txt"
got=$(sha256 "$tmp/$archive")
[ "$want" = "$got" ] || fail "checksum mismatch for $archive — download corrupted or tampered with"
say "Checksum OK."

tar -xzf "$tmp/$archive" -C "$tmp"
src=$(find "$tmp" -type f -name "$BIN" | head -n1)
[ -n "$src" ] || fail "$BIN not found in $archive"
chmod +x "$src"

dir="${SSLSYNC_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ]; then
    dir=/usr/local/bin
  else
    dir="$HOME/.local/bin"
  fi
fi
mkdir -p "$dir"
if [ -w "$dir" ]; then
  mv "$src" "$dir/$BIN"
else
  say "Installing to $dir needs sudo:"
  sudo mv "$src" "$dir/$BIN"
fi
# downloaded binaries are not notarized; allow macOS to run it
if [ "$os" = darwin ]; then xattr -d com.apple.quarantine "$dir/$BIN" 2>/dev/null || true; fi

say "Installed $("$dir/$BIN" version) to $dir/$BIN"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "Add $dir to your PATH:  echo 'export PATH=\"$dir:\$PATH\"' >> ~/.zshrc" ;;
esac
say ""
say "Next:  mkdir my-ssl && cd my-ssl && $BIN init"
say "Docs:  https://ilramdhan.github.io/sslsync"
