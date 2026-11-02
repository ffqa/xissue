#!/usr/bin/env bash
# Sign a macOS binary with a stable local identity.
#
# Why: an unsigned/ad-hoc Go binary has a designated requirement of just its
# cdhash, so every rebuild looks like a new app to TCC and macOS re-asks for
# Desktop/Documents/Downloads/network-volume access. Signing with one fixed
# certificate makes the requirement "identifier + certificate leaf", which is
# stable across rebuilds.
#
# Usage: sign-macos.sh <binary>
# Env:   XISSUE_CODESIGN_IDENTITY (default: "xIssue Codesign")
set -euo pipefail

bin="${1:?usage: sign-macos.sh <binary>}"
[ -f "$bin" ] || { echo "codesign: $bin not found" >&2; exit 1; }
if [ "$(uname -s)" != "Darwin" ]; then
  exit 0
fi

identity="${XISSUE_CODESIGN_IDENTITY:-xIssue Codesign}"
if ! security find-identity -v -p codesigning 2>/dev/null | grep -qF "$identity"; then
  # Signing is optional: releases are built by users themselves.
  if [ "${XISSUE_CODESIGN_VERBOSE:-}" = "1" ]; then
    echo "codesign skipped: identity \"$identity\" not found (run scripts/setup-codesign-macos.sh)" >&2
  fi
  exit 0
fi

codesign --force --sign "$identity" --identifier dev.xissue "$bin"
echo "codesigned $bin with \"$identity\""
