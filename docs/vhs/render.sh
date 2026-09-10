#!/usr/bin/env bash
# Render every VHS demo GIF for dzzzr and dzzzr-mock.
#
# Needs vhs, ttyd and ffmpeg on PATH:
#   macOS:  brew install vhs ttyd ffmpeg
#   Linux:  see https://github.com/charmbracelet/vhs#installation
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VHS_DIR="$ROOT/docs/vhs"
BIN_DIR="$VHS_DIR/bin"
GIF_DIR="$ROOT/docs/gifs"

for tool in vhs ttyd ffmpeg; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "$tool not found on PATH; see docs/vhs/render.sh header for install hints" >&2
    exit 1
  }
done

mkdir -p "$BIN_DIR" "$GIF_DIR"

echo "==> Building dzzzr and dzzzr-mock..."
(cd "$ROOT" && go build -o "$BIN_DIR/dzzzr" ./cmd/dzzzr/)
(cd "$ROOT" && go build -o "$BIN_DIR/dzzzr-mock" ./cmd/dzzzr-mock/)

# VHS `Require` and the tapes both look the binaries up on PATH.
export PATH="$BIN_DIR:$PATH"
# A clean HOME so `dzzzr login` inside a tape never clashes with a real session.
export HOME
HOME="$(mktemp -d)"
trap 'rm -rf "$HOME"' EXIT

TAPES=(
  dzzzr-quickstart.tape
  dzzzr-gameplay.tape
  dzzzr-mock.tape
  dzzzr-admin.tape
)

cd "$VHS_DIR"
for tape in "${TAPES[@]}"; do
  echo "==> Rendering $tape..."
  vhs "$tape"
done

echo "==> Done. GIFs written to $GIF_DIR (gitignored; CI publishes to GitHub Pages):"
ls -lh "$GIF_DIR"/*.gif
