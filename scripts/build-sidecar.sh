#!/usr/bin/env bash
# Builds the Go backend and places it where Tauri expects the sidecar:
#   src-tauri/binaries/flapp-core-<target-triple>[.exe]
# Tauri appends the Rust host target triple to externalBin entries, so the
# compiled binary must carry that exact suffix. Run this once before
# `npm run tauri build` (and again whenever the Go code changes).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="$ROOT/src-tauri/binaries"
mkdir -p "$BIN_DIR"

# Resolve the Rust host target triple (e.g. x86_64-unknown-linux-gnu).
if ! command -v rustc >/dev/null 2>&1; then
  echo "error: rustc not found. Install the Rust toolchain (https://rustup.rs)." >&2
  exit 1
fi
TRIPLE="$(rustc -Vv | sed -n 's/host: //p')"

EXT=""
case "$TRIPLE" in
  *windows*) EXT=".exe" ;;
esac

OUT="$BIN_DIR/flapp-core-$TRIPLE$EXT"

echo "Building Go backend → $OUT"
cd "$ROOT/backend"
go mod tidy
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$OUT" ./cmd/flapp-core

echo "Done: $OUT"
