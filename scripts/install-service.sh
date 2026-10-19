#!/usr/bin/env bash
# Install, remove, check, or restart the background service for xIssue.
# (Delegates to built-in 'xissue service' command)
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

find_binary() {
  local cand
  for cand in "$root/bin/xissue" "$root/xissue" "$HOME/.local/bin/xissue" /opt/homebrew/bin/xissue /usr/local/bin/xissue; do
    if [[ -f "$cand" ]]; then
      chmod +x "$cand" 2>/dev/null || true
      printf '%s\n' "$cand"
      return 0
    fi
  done

  if command -v xissue >/dev/null 2>&1; then
    command -v xissue
    return 0
  fi

  if [[ -f "$root/Makefile" && -f "$root/go.mod" ]] && command -v make >/dev/null 2>&1; then
    echo "xIssue binary not found; attempting to build it with make..." >&2
    if (cd "$root" && make build) >&2; then
      for cand in "$root/bin/xissue" "$root/xissue"; do
        if [[ -f "$cand" ]]; then
          chmod +x "$cand" 2>/dev/null || true
          printf '%s\n' "$cand"
          return 0
        fi
      done
    fi
  fi
  return 1
}

binary="$(find_binary || true)"
if [[ -n "$binary" && -x "$binary" ]]; then
  exec "$binary" service "$@"
fi

echo "error: 找不到 xissue 可执行文件，请确认当前目录存在 xissue 或运行 make build" >&2
exit 1
