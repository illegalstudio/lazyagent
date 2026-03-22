#!/usr/bin/env bash
# Code-sign a macOS binary for distribution.
# Called by goreleaser as a post-build hook.
#
# Required env vars:
#   APPLE_SIGNING_IDENTITY  — e.g. "Developer ID Application: Name (TEAMID)"
#
# Usage: scripts/codesign.sh <binary-path>

set -euo pipefail

BINARY="$1"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ENTITLEMENTS="$SCRIPT_DIR/entitlements.plist"

if [[ -z "${APPLE_SIGNING_IDENTITY:-}" ]]; then
  echo "⚠ APPLE_SIGNING_IDENTITY not set, skipping code signing"
  exit 0
fi

echo "Signing $BINARY ..."
codesign --force --options runtime \
  --sign "$APPLE_SIGNING_IDENTITY" \
  --entitlements "$ENTITLEMENTS" \
  --timestamp \
  "$BINARY"

echo "Verifying signature ..."
codesign --verify --verbose "$BINARY"
