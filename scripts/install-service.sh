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
      svc_path="$HOME/.opencode/bin:/opt/homebrew/bin:$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:${PATH:-}"
      if [[ -f "$root/deploy/dev.xissue.plist" ]]; then
        sed -e "s|__BIN__|$binary|g" \
            -e "s|__HOME__|$HOME|g" \
            -e "s|__PATH__|$svc_path|g" \
            -e "s|__LOG__|$log|g" \
            "$root/deploy/dev.xissue.plist" > "$target"
      else
        cat <<EOF > "$target"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$label</string>
  <key>ProgramArguments</key>
  <array>
    <string>$binary</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>$HOME</string>
    <key>PATH</key>
    <string>$svc_path</string>
  </dict>
  <key>WorkingDirectory</key>
  <string>$HOME</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Background</string>
  <key>StandardOutPath</key>
  <string>$log</string>
  <key>StandardErrorPath</key>
  <string>$log</string>
</dict>
</plist>
EOF
      fi
      pkill -f "$binary" 2>/dev/null || true
      launchctl bootout "gui/$(id -u)/$label" 2>/dev/null || true
      launchctl bootstrap "gui/$(id -u)" "$target"
      echo "installed $label -> $target"
      echo "binary: $binary"
      echo "log: $log"
    else
      mkdir -p "$HOME/.config/systemd/user"
      target="$HOME/.config/systemd/user/xissue.service"
      if [[ -f "$root/deploy/xissue.service" ]]; then
        sed "s|/usr/local/bin/xissue|$binary|g" "$root/deploy/xissue.service" > "$target"
      else
        cat <<EOF > "$target"
[Unit]
Description=xIssue — Personal AI Agent Issue & Multi-Repo Orchestrator
After=network-online.target tailscaled.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=$binary
Restart=on-failure
RestartSec=3
OOMScoreAdjust=-500
Environment=HOME=%h

[Install]
WantedBy=default.target
EOF
      fi
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
      if ! launchctl kickstart -k "gui/$(id -u)/$label" 2>/dev/null; then
        "$0" install
      else
        echo "restarted $label"
      fi
    else
      systemctl --user restart xissue.service || "$0" install
      echo "restarted xissue.service"
    fi
    ;;
  *)
    echo "usage: $0 [install|uninstall|status|restart]" >&2
    exit 64
    ;;
esac
