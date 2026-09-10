#!/usr/bin/env bash
# Builds dzzzr.xcframework from mobile/dzzzrmobile.
#
# Usage: ./mobile/bind-ios.sh [output-dir]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OUT_DIR="${1:-$ROOT/mobile/build}"
mkdir -p "$OUT_DIR"

echo "==> Installing the gomobile toolchain..."
go install golang.org/x/mobile/cmd/gomobile
go install golang.org/x/mobile/cmd/gobind
PATH="$(go env GOPATH)/bin:$PATH"
export PATH
gomobile init

echo "==> Running the binding tests..."
go test ./mobile/dzzzrmobile/ -count=1

echo "==> Building dzzzr.xcframework..."
gomobile bind -target=ios -o "$OUT_DIR/dzzzr.xcframework" ./mobile/dzzzrmobile

echo "==> Done: $OUT_DIR/dzzzr.xcframework"
