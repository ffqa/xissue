//go:build !windows

package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func execSelf(exe string) {
	_ = syscall.Exec(exe, os.Args, os.Environ())
}

// serviceManaged reports whether the current process is supervised by a
// service manager that will start a fresh process after we exit. A new install
// writes XISSUE_SERVICE=1 into the unit; the extra checks keep upgrades from
// older units working without requiring a reinstall.
func serviceManaged() bool {
	if os.Getenv("XISSUE_SERVICE") == "1" {
		return true
	}
	if runtime.GOOS == "linux" {
		return os.Getenv("INVOCATION_ID") != "" || os.Getenv("SYSTEMD_EXEC_PID") != ""
	}
	if runtime.GOOS != "darwin" || os.Getppid() != 1 {
		return false
	}
	// Avoid treating a detached nohup process (also PPID 1) as a LaunchAgent.
	pid := strconv.Itoa(os.Getpid())
	for _, label := range []string{"dev.xissue", "homebrew.mxcl.xissue"} {
		out, err := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)).Output()
		if err == nil && strings.Contains(string(out), "pid = "+pid) {
			return true
		}
	}
	return false
}

// requestSupervisorRestart asks the service manager to restart us before we
// exit. launchd's KeepAlive handles this automatically; systemd needs an
// explicit restart job because our unit historically used Restart=on-failure.
// It returns false when no restart request could be queued.
func requestSupervisorRestart() bool {
	if runtime.GOOS == "darwin" {
		return true
	}
	if runtime.GOOS != "linux" {
		return false
	}
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return false
	}
	unit := unitFromCgroup(string(raw))
	if unit == "" {
		return false
	}
	return exec.Command("systemctl", "--user", "--no-block", "restart", unit).Run() == nil
}

// unitFromCgroup extracts the .service unit from a /proc/self/cgroup file.
// systemd v2 uses "0::/user.slice/.../xissue.service"; older layouts prefix
// the path with "name=systemd:".
func unitFromCgroup(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.LastIndex(line, ":"); idx >= 0 {
			line = line[idx+1:]
		}
		base := filepath.Base(line)
		if strings.HasSuffix(base, ".service") {
			return base
		}
	}
	return ""
}
