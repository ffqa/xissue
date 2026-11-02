#!/usr/bin/env bash
# Sign a macOS binary with the stable local identity created by
# scripts/setup-codesign-macos.sh.
#
# Why: an ad-hoc Go binary's designated requirement is just its cdhash, so every
# rebuild looks like a new app to TCC and macOS re-asks for folder access.
# A fixed certificate makes the requirement stable across rebuilds.
#
# Usage: sign-macos.sh <binary>
# Env:   XISSUE_CODESIGN_IDENTITY (default: "xIssue Codesign")
set -euo pipefail

bin="${1:?usage: sign-macos.sh <binary>}"
[ -f "$bin" ] || { echo "codesign: $bin not found" >&2; exit 1; }
[ "$(uname -s)" = "Darwin" ] || exit 0

identity="${XISSUE_CODESIGN_IDENTITY:-xIssue Codesign}"
keychain="$HOME/Library/Keychains/xissue-signing.keychain-db"
pwfile="$HOME/.xissue/codesign.keychain-password"


# codesign has no --keychain option: it only searches the user keychain list.
# Add our signing keychain there (idempotent) so the identity is discoverable.
ensure_keychain_in_search_list() {
  local kc_path="$1"
  local -a list=()
  local line
  while IFS= read -r line; do
    line="$(printf '%s' "$line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//')"
    [ -n "$line" ] && list+=("$line")
  done < <(security list-keychains -d user)
  for l in "${list[@]}"; do
    [ "$l" = "$kc_path" ] && return 0
  done
  list+=("$kc_path")
  security list-keychains -d user -s "${list[@]}" >/dev/null
}

sign() {
  codesign --force --sign "$identity" --identifier dev.xissue "$bin"
}

if [ -f "$keychain" ] && [ -f "$pwfile" ]; then
  security unlock-keychain -p "$(cat "$pwfile")" "$keychain" >/dev/null 2>&1 || true
  if security find-identity -v -p codesigning "$keychain" 2>/dev/null | grep -qF "$identity"; then
    ensure_keychain_in_search_list "$keychain"
    sign
    echo "codesigned $bin with \"$identity\""
    exit 0
  fi
fi

# Fallback: an identity trusted in the default keychain search list.
if security find-identity -v -p codesigning 2>/dev/null | grep -qF "$identity"; then
  sign
  echo "codesigned $bin with \"$identity\""
  exit 0
fi

# Optional feature: releases are built by users, so a missing identity is fine.
if [ "${XISSUE_CODESIGN_VERBOSE:-}" = "1" ]; then
  echo "codesign skipped: identity \"$identity\" not found (run scripts/setup-codesign-macos.sh)" >&2
fi
exit 0
