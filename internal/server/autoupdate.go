package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/updater"
)

const (
	autoUpdateFirstDelay = 45 * time.Second
	autoUpdateInterval   = time.Hour
	autoUpdateRetryAfter = 6 * time.Hour
)

// autoUpdateLoop checks the official release feed hourly. Updates are applied
// by the existing `xissue update` command, which verifies SHA256SUMS and
// restarts the service; this loop only decides when to run it.
var (
	fetchLatestRelease = updater.FetchLatest
	canAutoUpdate      = autoUpdateEligible
	runUpdateCommand   = func(ctx context.Context, exe string) ([]byte, error) {
		return exec.CommandContext(ctx, exe, "update").CombinedOutput()
	}
)

func (s *Server) autoUpdateLoop() {
	timer := time.NewTimer(autoUpdateFirstDelay)
	defer timer.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-timer.C:
		}
		s.checkAutoUpdate()
		timer.Reset(autoUpdateInterval)
	}
}

func (s *Server) checkAutoUpdate() {
	if !s.cfg.AutoUpdateEnabled() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		s.runner.LogMonitor("WARN", "update", "无法定位可执行文件，跳过自动更新: "+err.Error())
		return
	}
	if !canAutoUpdate(exe) {
		s.runner.LogMonitor("INFO", "update", "当前进程不是持久化安装的服务二进制，跳过自动更新")
		return
	}
	s.updateMu.Lock()
	if s.updateRunning {
		s.updateMu.Unlock()
		return
	}
	s.updateMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	rel, err := fetchLatestRelease(ctx)
	cancel()
	if err != nil {
		s.runner.LogMonitor("WARN", "update", "检查更新失败: "+err.Error())
		return
	}
	latest := strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
	if latest == "" || !updater.IsNewer(Version, latest) {
		s.updateMu.Lock()
		s.latestUpdate = ""
		s.updateMu.Unlock()
		return
	}
	s.updateMu.Lock()
	s.latestUpdate = latest
	lastVersion := s.lastUpdateVersion
	lastAt := s.lastUpdateAt
	s.updateMu.Unlock()
	if lastVersion == latest && time.Since(lastAt) < autoUpdateRetryAfter {
		return
	}

	if runs, err := s.st.ListActiveRuns(context.Background()); err == nil && len(runs) > 0 {
		s.runner.LogMonitor("INFO", "update", fmt.Sprintf("发现新版本 v%s，但有 %d 个任务正在执行，稍后再更新", latest, len(runs)))
		return
	}

	s.updateMu.Lock()
	s.updateRunning = true
	s.lastUpdateVersion = latest
	s.lastUpdateAt = time.Now()
	s.updateMu.Unlock()
	defer func() {
		s.updateMu.Lock()
		s.updateRunning = false
		s.updateMu.Unlock()
	}()

	s.runner.LogMonitor("EVENT", "update", fmt.Sprintf("发现新版本 v%s (当前 v%s)，开始自动更新", latest, Version))
	s.runner.PushNotification(
		"update",
		fmt.Sprintf("🚀 发现新版本 v%s，正在自动更新", latest),
		fmt.Sprintf("当前版本 v%s。下载并校验完成后服务会自动重启。", Version),
		"",
		fmt.Sprintf(`{"version":%q}`, latest),
	)

	updateCtx, updateCancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer updateCancel()
	out, err := runUpdateCommand(updateCtx, exe)
	text := strings.TrimSpace(string(out))
	if len(text) > 4000 {
		text = text[len(text)-4000:]
	}
	if text != "" {
		s.runner.LogMonitor("INFO", "update", "更新输出:\n"+text)
	}
	if err != nil {
		s.notifyUpdateFailure(latest, err.Error())
		return
	}

	s.runner.PushNotification(
		"update",
		fmt.Sprintf("✅ 已更新到 v%s，正在重启", latest),
		"服务将在几秒内重启，浏览器会自动重连。",
		"",
		fmt.Sprintf(`{"version":%q}`, latest),
	)
	go s.restartAfterUpdate()
}

// autoUpdateEligible prevents a dev/test binary from updating and restarting a
// production service that happens to be installed on the same machine.
func autoUpdateEligible(exe string) bool {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	clean := filepath.ToSlash(exe)
	home, _ := os.UserHomeDir()
	localBin := filepath.ToSlash(filepath.Join(home, ".local", "bin", "xissue"))
	if clean == localBin || clean == localBin+".exe" {
		return true
	}
	return strings.Contains(clean, "/Cellar/xissue/") ||
		strings.Contains(clean, "/opt/homebrew/") ||
		strings.Contains(clean, "/usr/local/Cellar/xissue/")
}

func (s *Server) notifyUpdateFailure(version, reason string) {
	s.runner.LogMonitor("ERROR", "update", fmt.Sprintf("自动更新到 v%s 失败: %s", version, reason))
	s.runner.PushNotification(
		"update",
		fmt.Sprintf("⚠️ 自动更新到 v%s 失败", version),
		reason+"\n可手动运行 `xissue update` 重试。",
		"",
		"",
	)
}

func (s *Server) restartAfterUpdate() {
	s.restartProcess(1200 * time.Millisecond)
}
