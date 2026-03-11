#!/usr/bin/env bash
set -euo pipefail

OUTPUT_DIR="${1:-release}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CLIENT_DIR="$REPO_ROOT/client"
SERVER_DIR="$REPO_ROOT/server"
CLIENT_DIST_DIR="$CLIENT_DIR/apps/web/dist"
EMBEDDED_DIST_DIR="$SERVER_DIR/internal/webui/dist"
RELEASE_DIR="$REPO_ROOT/$OUTPUT_DIR"
OUTPUT_BIN="$RELEASE_DIR/litesync"

echo "==> Build web UI"
cd "$CLIENT_DIR"
CI=true pnpm install --frozen-lockfile
pnpm --filter web build

if [[ ! -d "$CLIENT_DIST_DIR" ]]; then
  echo "Web build output not found: $CLIENT_DIST_DIR" >&2
  exit 1
fi

echo "==> Sync web assets into Go embed directory"
rm -rf "$EMBEDDED_DIST_DIR"
mkdir -p "$EMBEDDED_DIST_DIR"
cp -R "$CLIENT_DIST_DIR"/. "$EMBEDDED_DIST_DIR"

echo "==> Build standalone binary"
mkdir -p "$RELEASE_DIR"
cd "$SERVER_DIR"
go mod tidy
go build -o "$OUTPUT_BIN" ./cmd/litesync-server

echo ""
echo "Build complete:"
echo "  $OUTPUT_BIN"

