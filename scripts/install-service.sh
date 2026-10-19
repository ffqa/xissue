#!/usr/bin/env bash
# Install, remove, check, or restart the background service for xIssue
# (launchd on macOS, systemd --user on Linux).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
label="dev.xissue"
log_dir="$HOME/.xissue"
log="$log_dir/xissue.log"

os="$(uname -s)"

find_binary() {
  local cand
  for cand in "$root/bin/xissue" "$root/xissue" "$root/bin/xissue.exe" "$root/xissue.exe"; do
    if [[ -f "$cand" ]]; then
      printf '%s\n' "$cand"
      return 0
    fi
  done

  if [[ -f "$root/Makefile" && -f "$root/go.mod" ]] && command -v make >/dev/null 2>&1; then
    echo "xIssue binary not found; attempting to build it with make..." >&2
    if (cd "$root" && make build) >&2; then
      for cand in "$root/bin/xissue" "$root/bin/xissue.exe"; do
        if [[ -f "$cand" ]]; then
          printf '%s\n' "$cand"
          return 0
        fi
      done
    fi
  fi
  return 1
}

prepare_binary() {
  local bin="$1"
  chmod +x "$bin" 2>/dev/null || true
  if command -v xattr >/dev/null 2>&1; then
    xattr -r -c "$bin" 2>/dev/null || true
    xattr -dr com.apple.quarantine "$bin" 2>/dev/null || true
  fi
  if [[ ! -x "$bin" ]]; then
    echo "warning: $bin is not executable; you may need: chmod +x \"$bin\"" >&2
  fi
}

case "${1:-install}" in
  install)
    if ! binary="$(find_binary)"; then
      echo "missing xIssue binary; run: make build" >&2
      echo "on macOS, a downloaded binary may also need: sudo xattr -r -c \"$root/xissue\"" >&2
      exit 1
    fi
    prepare_binary "$binary"
    mkdir -p "$log_dir"
    if [[ "$os" == "Darwin" ]]; then
      mkdir -p "$HOME/Library/LaunchAgents"
      target="$HOME/Library/LaunchAgents/$label.plist"
      svc_path="$HOME/.opencode/bin:/opt/homebrew/bin:$HOME/Library/Application Support/Herd/config/nvm/versions/node/v24.16.0/bin:$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
      sed -e "s|__BIN__|$binary|g" \
          -e "s|__HOME__|$HOME|g" \
          -e "s|__PATH__|$svc_path|g" \
          -e "s|__LOG__|$log|g" \
          "$root/deploy/dev.xissue.plist" > "$target"
      pkill -f "$binary" 2>/dev/null || true
      launchctl bootout "gui/$(id -u)/$label" 2>/dev/null || true
      launchctl bootstrap "gui/$(id -u)" "$target"
      echo "installed $label -> $target"
      echo "binary: $binary"
      echo "log: $log"
    else
      mkdir -p "$HOME/.config/systemd/user"
      target="$HOME/.config/systemd/user/xissue.service"
      sed "s|/usr/local/bin/xissue|$binary|g" "$root/deploy/xissue.service" > "$target"
      systemctl --user daemon-reload
      systemctl --user enable --now xissue.service
      echo "installed systemd user service -> $target"
      echo "binary: $binary"
    fi
    ;;
  uninstall)
    if [[ "$os" == "Darwin" ]]; then
      target="$HOME/Library/LaunchAgents/$label.plist"
      launchctl bootout "gui/$(id -u)/$label" 2>/dev/null || true
      rm -f "$target"
      echo "removed $label"
    else
      systemctl --user disable --now xissue.service 2>/dev/null || true
      rm -f "$HOME/.config/systemd/user/xissue.service"
      systemctl --user daemon-reload
      echo "removed xissue.service"
    fi
    ;;
  status)
    if [[ "$os" == "Darwin" ]]; then
      if launchctl print "gui/$(id -u)/$label" >/dev/null 2>&1; then
        launchctl print "gui/$(id -u)/$label" | grep -E '^\s*(state|pid|program) =' || true
      else
        echo "$label is not loaded"
      fi
    else
      systemctl --user status xissue.service --no-pager || true
    fi
    ;;
  restart)
    if [[ "$os" == "Darwin" ]]; then
      lsof -ti tcp:7789 | xargs kill -9 2>/dev/null || true
      launchctl kickstart -k "gui/$(id -u)/$label"
      echo "restarted $label"
    else
      systemctl --user restart xissue.service
      echo "restarted xissue.service"
    fi
    ;;
  *)
    echo "usage: $0 [install|uninstall|status|restart]" >&2
    exit 64
    ;;
esac
