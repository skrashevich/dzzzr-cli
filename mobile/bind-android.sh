#!/usr/bin/env bash
# Builds dzzzr.aar from mobile/dzzrmobile.
#
# Needs the Android SDK and NDK: set ANDROID_HOME (or ANDROID_SDK_ROOT) and
# ANDROID_NDK_HOME. minSdk is 24, which is what gomobile supports without
# extra flags.
#
# Usage: ./mobile/bind-android.sh [output-dir]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OUT_DIR="${1:-$ROOT/mobile/build}"
mkdir -p "$OUT_DIR"

SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
if [ -z "$SDK" ]; then
  echo "ANDROID_HOME (or ANDROID_SDK_ROOT) is not set; install the Android SDK first." >&2
  exit 1
fi
if [ -z "${ANDROID_NDK_HOME:-}" ]; then
  # gomobile finds the NDK inside the SDK when it is installed there.
  if [ -d "$SDK/ndk" ]; then
    ANDROID_NDK_HOME="$(find "$SDK/ndk" -mindepth 1 -maxdepth 1 -type d | sort | tail -1)"
    export ANDROID_NDK_HOME
    echo "==> Using NDK $ANDROID_NDK_HOME"
  else
    echo "ANDROID_NDK_HOME is not set and no NDK was found under $SDK/ndk." >&2
    exit 1
  fi
fi

echo "==> Installing the gomobile toolchain..."
go install golang.org/x/mobile/cmd/gomobile
go install golang.org/x/mobile/cmd/gobind
PATH="$(go env GOPATH)/bin:$PATH"
export PATH
gomobile init

echo "==> Running the binding tests..."
go test ./mobile/dzzrmobile/ -count=1

echo "==> Building dzzzr.aar..."
gomobile bind -target=android -androidapi 24 -o "$OUT_DIR/dzzzr.aar" ./mobile/dzzrmobile

echo "==> Done: $OUT_DIR/dzzzr.aar"
