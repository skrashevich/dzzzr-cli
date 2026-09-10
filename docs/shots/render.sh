#!/usr/bin/env bash
# Render every screenshot of the dzzzr web interface.
#
# Builds dzzzr and dzzzr-mock, runs the mock, signs a throwaway session in,
# seeds the web chat store with fixtures, serves `dzzzr web`, and drives
# Chromium over it with Playwright. Needs Go and Node on PATH.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SHOTS_DIR="$ROOT/docs/shots"
BIN_DIR="$SHOTS_DIR/bin"
OUT_DIR="$ROOT/docs/screenshots"

PORT_MOCK="${DZZZR_SHOTS_MOCK_PORT:-18190}"
PORT_WEB="${DZZZR_SHOTS_WEB_PORT:-18191}"

HOME_DIR="$(mktemp -d)"
MOCK_PID=""
WEB_PID=""
cleanup() {
  [ -n "$WEB_PID" ] && kill "$WEB_PID" 2>/dev/null || true
  [ -n "$MOCK_PID" ] && kill "$MOCK_PID" 2>/dev/null || true
  rm -rf "$HOME_DIR"
}
trap cleanup EXIT

command -v go >/dev/null 2>&1   || { echo "go not found on PATH" >&2; exit 1; }
command -v node >/dev/null 2>&1 || { echo "node not found on PATH" >&2; exit 1; }
command -v npm >/dev/null 2>&1  || { echo "npm not found on PATH" >&2; exit 1; }

mkdir -p "$BIN_DIR" "$OUT_DIR"

echo "==> Building dzzzr and dzzzr-mock..."
(cd "$ROOT" && go build -o "$BIN_DIR/dzzzr" ./cmd/dzzzr/)
(cd "$ROOT" && go build -o "$BIN_DIR/dzzzr-mock" ./cmd/dzzzr-mock/)

# `dzzzr web` opens the system browser on start. Shadow open(1)/xdg-open with a
# no-op so a local run does not spawn a real window; on CI neither exists.
for shim in open xdg-open; do
  printf '#!/bin/sh\nexit 0\n' > "$BIN_DIR/$shim"
  chmod +x "$BIN_DIR/$shim"
done
export PATH="$BIN_DIR:$PATH"

export HOME="$HOME_DIR"
# A throwaway HOME is not enough to keep the run off the real session:
# XDG_CONFIG_HOME is set on the GitHub runners and outranks it, so the
# configuration directory is named outright.
export DZZZR_CONFIG_DIR="$HOME_DIR/.config/dzzzr"
export DZZZR_CITY="moscow"
export DZZZR_BASE_URL="http://127.0.0.1:${PORT_MOCK}/moscow/"

echo "==> Starting dzzzr-mock on 127.0.0.1:${PORT_MOCK}..."
"$BIN_DIR/dzzzr-mock" -addr "127.0.0.1:${PORT_MOCK}" >/tmp/dzzzr-shots-mock.log 2>&1 &
MOCK_PID=$!
for _ in $(seq 1 50); do
  curl -fsS "http://127.0.0.1:${PORT_MOCK}/moscow/API/gamesList.php" >/dev/null 2>&1 && break
  sleep 0.2
done

echo "==> Signing in a throwaway session against the mock..."
"$BIN_DIR/dzzzr" login -login demo -password demo -captain demo -pin 1234 >/dev/null

echo "==> Seeding web chat fixtures..."
CHATS_DIR="$DZZZR_CONFIG_DIR/web/chats"
mkdir -p "$CHATS_DIR"
cp "$SHOTS_DIR"/fixtures/chats/*.json "$CHATS_DIR"/

export DZZZR_LLM_API_KEY="demo-key"
export DZZZR_LLM_MODEL="openrouter/anthropic/claude-3.5-sonnet"
export DZZZR_FILES_ROOT="$HOME/files"
mkdir -p "$DZZZR_FILES_ROOT"

echo "==> Serving dzzzr web on 127.0.0.1:${PORT_WEB}..."
"$BIN_DIR/dzzzr" web -web-addr "127.0.0.1:${PORT_WEB}" >/tmp/dzzzr-shots-web.log 2>&1 &
WEB_PID=$!
for _ in $(seq 1 50); do
  curl -fsS "http://127.0.0.1:${PORT_WEB}/" >/dev/null 2>&1 && break
  sleep 0.2
done

cd "$SHOTS_DIR"
if [ ! -d node_modules ]; then
  echo "==> Installing Playwright..."
  npm install --no-audit --no-fund
fi
npx --yes playwright install --with-deps chromium 2>/dev/null || npx --yes playwright install chromium

echo "==> Capturing screenshots..."
DZZZR_WEB_URL="http://127.0.0.1:${PORT_WEB}" SHOTS_OUT="$OUT_DIR" node shoot.mjs

echo "==> Done. Screenshots written to $OUT_DIR (gitignored; CI publishes to GitHub Pages):"
ls -lh "$OUT_DIR"/*.png
