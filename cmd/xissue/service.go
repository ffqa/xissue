package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const serviceLabel = "dev.xissue"

func runService(args []string) {
	if len(args) == 0 {
		printServiceUsage()
		return
	}

	cmd := args[0]
	switch cmd {
	case "install":
		installService()
	case "uninstall":
		uninstallService()
	case "status":
		statusService()
	case "restart":
		restartService()
	case "help", "-h", "--help":
		printServiceUsage()
	default:
		fmt.Fprintf(os.Stderr, "未知服务子命令: %s\n\n", cmd)
		printServiceUsage()
		os.Exit(1)
	}
}

func printServiceUsage() {
	fmt.Println("用法: xissue service [install|uninstall|status|restart]")
	fmt.Println()
	fmt.Println("子命令:")
	fmt.Println("  install    将 xissue 安装到持久化目录 (~/.local/bin) 并注册为系统自启常驻服务")
	fmt.Println("  uninstall  停止并卸载常驻后台服务")
	fmt.Println("  status     查看后台常驻服务运行状态与监听端口")
	fmt.Println("  restart    重启后台常驻服务")
}

func getPersistentBinary() (string, error) {
	rawExe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法获取当前二进制路径: %w", err)
	}
	realExe := rawExe
	if r, err := filepath.EvalSymlinks(rawExe); err == nil {
		realExe = r
	}

	// 1. 如果属于 Homebrew 安装，必须返回版本无关的稳定软链接（如 /usr/local/bin/xissue 或 /opt/homebrew/bin/xissue），
	//    绝不能使用包含特定版本号的 Cellar 路径（如 /usr/local/Cellar/xissue/0.2.8/bin/xissue），
	//    这样在后续 brew upgrade 时系统服务无需重新安装即可平滑生效。
	if stableBrew, ok := resolveStableBrewBinary(rawExe, realExe); ok {
		return stableBrew, nil
	}

	// 2. 如果已经是用户持久路径 ~/.local/bin/xissue
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法获取用户家目录: %w", err)
	}
	targetDir := filepath.Join(home, ".local", "bin")
	targetBin := filepath.Join(targetDir, "xissue")
	if runtime.GOOS == "windows" {
		targetBin += ".exe"
	}

	if rawExe == targetBin || realExe == targetBin {
		return targetBin, nil
	}

	// 3. 否则无论当前运行在 ~/Downloads、/tmp 还是源码目录，一律复制到 ~/.local/bin/xissue
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("创建目录 %s 失败: %w", targetDir, err)
	}

	cleanReal := filepath.ToSlash(realExe)
	if cleanReal != filepath.ToSlash(targetBin) {
		fmt.Printf("正在将二进制复制到持久化路径: %s ...\n", targetBin)
		tmpTarget := targetBin + ".tmp"
		if err := copyExecutableFile(realExe, tmpTarget); err != nil {
			return "", fmt.Errorf("复制二进制文件失败: %w", err)
		}
		if err := os.Rename(tmpTarget, targetBin); err != nil {
			// 如果直接 rename 失败，尝试先 remove
			_ = os.Remove(targetBin)
			if err2 := os.Rename(tmpTarget, targetBin); err2 != nil {
				return "", fmt.Errorf("安装二进制文件失败: %w", err2)
			}
		}
	}

	_ = os.Chmod(targetBin, 0o755)
	if runtime.GOOS == "darwin" {
		_ = exec.Command("xattr", "-r", "-c", targetBin).Run()
		_ = exec.Command("xattr", "-dr", "com.apple.quarantine", targetBin).Run()
	}
	return targetBin, nil
}

// resolveStableBrewBinary 解析 Homebrew 环境下的稳定软链接路径。
// 必须返回版本无关的稳定软链接（如 /usr/local/bin/xissue 或 /opt/homebrew/bin/xissue），
// 严禁返回包含具体版本号的临时 Cellar 路径（如 /usr/local/Cellar/xissue/0.2.8/bin/xissue）。
func resolveStableBrewBinary(rawExe, realExe string) (string, bool) {
	cleanRaw := filepath.ToSlash(rawExe)
	cleanReal := filepath.ToSlash(realExe)

	// 1. 如果原始可执行路径本身就是 Homebrew 的 bin 目录软链接，直接使用它
	for _, p := range []string{"/opt/homebrew/bin/xissue", "/usr/local/bin/xissue", "/home/linuxbrew/.linuxbrew/bin/xissue"} {
		if cleanRaw == p {
			return rawExe, true
		}
	}
	if strings.HasPrefix(cleanRaw, "/opt/homebrew/bin/") ||
		strings.HasPrefix(cleanRaw, "/usr/local/bin/") ||
		strings.HasPrefix(cleanRaw, "/home/linuxbrew/.linuxbrew/bin/") {
		return rawExe, true
	}

	// 2. 如果真实路径位于 Cellar（例如用户直接在 /usr/local/Cellar/xissue/0.2.8/bin/xissue 执行），
	//    从 Cellar 路径反推其上层 Homebrew prefix 的 bin/xissue 稳定软链接
	if idx := strings.Index(cleanReal, "/Cellar/xissue/"); idx != -1 {
		prefix := cleanReal[:idx]
		stable := filepath.Join(prefix, "bin", "xissue")
		if _, err := os.Stat(stable); err == nil {
			return stable, true
		}
	}

	// 3. 尝试通过 brew --prefix 查询标准软链接
	if _, err := exec.LookPath("brew"); err == nil {
		if out, err := exec.Command("brew", "--prefix").Output(); err == nil {
			prefix := strings.TrimSpace(string(out))
			if prefix != "" {
				stable := filepath.Join(prefix, "bin", "xissue")
				if _, err := os.Stat(stable); err == nil {
					return stable, true
				}
			}
		}
	}

	return "", false
}

// checkAndMigrateCellarService 检查现有服务配置，若发现写死了特定版本 Cellar 路径，自动无缝迁移至稳定软链接。
func checkAndMigrateCellarService() bool {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		plistPath := filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist")
		data, err := os.ReadFile(plistPath)
		if err != nil {
			return false
		}
		content := string(data)
		if !strings.Contains(content, "/Cellar/xissue/") {
			return false
		}

		stableBin, err := getPersistentBinary()
		if err != nil || strings.Contains(stableBin, "/Cellar/xissue/") {
			return false
		}

		fmt.Printf("💡 检测到常驻服务配置指向固定版本的 Cellar 物理路径（brew upgrade 后将无法自动生效）\n")
		fmt.Printf("   正在自动平滑迁移至版本无关的稳定软链接: %s ...\n", stableBin)
		installService()
		return true
	}

	if runtime.GOOS == "linux" {
		serviceFile := filepath.Join(home, ".config", "systemd", "user", "xissue.service")
		data, err := os.ReadFile(serviceFile)
		if err != nil {
			return false
		}
		content := string(data)
		if !strings.Contains(content, "/Cellar/xissue/") {
			return false
		}

		stableBin, err := getPersistentBinary()
		if err != nil || strings.Contains(stableBin, "/Cellar/xissue/") {
			return false
		}

		fmt.Printf("💡 检测到 systemd 服务配置指向特定版本的 Cellar 物理路径\n")
		fmt.Printf("   正在自动平滑迁移至稳定软链接: %s ...\n", stableBin)
		installService()
		return true
	}

	return false
}

// terminateProcessesByPath stops other processes whose command line contains
// path. It excludes the current process: pkill -f matches its own invocation
// when the binary lives under ~/.local/bin, which aborted service install.
func terminateProcessesByPath(path string) {
	out, err := exec.Command("pgrep", "-f", path).Output()
	if err != nil {
		return
	}
	self := os.Getpid()
	var procs []*os.Process
	for _, field := range strings.Fields(string(out)) {
		pid, perr := strconv.Atoi(field)
		if perr != nil || pid == self {
			continue
		}
		if proc, ferr := os.FindProcess(pid); ferr == nil {
			_ = proc.Signal(os.Interrupt)
			procs = append(procs, proc)
		}
	}
	if len(procs) == 0 {
		return
	}
	time.Sleep(400 * time.Millisecond)
	for _, proc := range procs {
		_ = proc.Kill()
	}
}

func copyExecutableFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func installService() {
	persistentBin, err := getPersistentBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "安装失败: %v\n", err)
		os.Exit(1)
	}

	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".xissue")
	logFile := filepath.Join(logDir, "xissue.log")
	_ = os.MkdirAll(logDir, 0o755)

	if runtime.GOOS == "darwin" {
		launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
		_ = os.MkdirAll(launchAgentsDir, 0o755)
		targetPlist := filepath.Join(launchAgentsDir, serviceLabel+".plist")

		svcPath := fmt.Sprintf("%s/.opencode/bin:/opt/homebrew/bin:%s/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:%s",
			home, home, os.Getenv("PATH"))

		plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>%s</string>
    <key>PATH</key>
    <string>%s</string>
  </dict>
  <key>WorkingDirectory</key>
  <string>%s</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Background</string>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, serviceLabel, persistentBin, home, svcPath, home, logFile, logFile)

		if err := os.WriteFile(targetPlist, []byte(plistContent), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "写入 plist 文件失败: %v\n", err)
			os.Exit(1)
		}

		uid := os.Getuid()
		terminateProcessesByPath(persistentBin)
		_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, serviceLabel)).Run()
		if out, err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), targetPlist).CombinedOutput(); err != nil {
			// 如果 bootstrap 报错已存在，尝试 kickstart
			_ = exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", uid, serviceLabel)).Run()
			_ = out
		}

		fmt.Println("✓ 已成功安装为开机自启常驻服务 (macOS LaunchAgent):")
		fmt.Printf("  • 服务标签: %s\n", serviceLabel)
		fmt.Printf("  • 配置文件: %s\n", targetPlist)
		if strings.Contains(persistentBin, "/bin/xissue") && (strings.HasPrefix(persistentBin, "/opt/homebrew/") || strings.HasPrefix(persistentBin, "/usr/local/")) {
			fmt.Printf("  • 二进制文件: %s (Homebrew 稳定软链接，版本无关，后续 brew upgrade 自动生效)\n", persistentBin)
		} else {
			fmt.Printf("  • 二进制文件: %s (持久化路径，可安全删除解压/编译目录)\n", persistentBin)
		}
		fmt.Printf("  • 日志文件: %s\n", logFile)
		fmt.Println("  • 服务地址: http://127.0.0.1:7789")
		return
	}

	if runtime.GOOS == "linux" {
		systemdDir := filepath.Join(home, ".config", "systemd", "user")
		_ = os.MkdirAll(systemdDir, 0o755)
		targetService := filepath.Join(systemdDir, "xissue.service")

		serviceContent := fmt.Sprintf(`[Unit]
Description=xIssue — Personal AI Agent Issue & Multi-Repo Orchestrator
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=3
OOMScoreAdjust=-500
Environment=HOME=%s

[Install]
WantedBy=default.target
`, persistentBin, home)

		if err := os.WriteFile(targetService, []byte(serviceContent), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "写入 systemd service 失败: %v\n", err)
			os.Exit(1)
		}

		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		_ = exec.Command("systemctl", "--user", "enable", "--now", "xissue.service").Run()

		fmt.Println("✓ 已成功安装为开机自启常驻服务 (Linux systemd user service):")
		fmt.Printf("  • 配置文件: %s\n", targetService)
		if strings.Contains(persistentBin, "/bin/xissue") && strings.HasPrefix(persistentBin, "/home/linuxbrew/") {
			fmt.Printf("  • 二进制文件: %s (Homebrew 稳定软链接，后续 brew upgrade 自动生效)\n", persistentBin)
		} else {
			fmt.Printf("  • 二进制文件: %s (持久化路径，可安全删除解压/编译目录)\n", persistentBin)
		}
		fmt.Printf("  • 日志查看: journalctl --user -u xissue -f\n")
		fmt.Println("  • 服务地址: http://127.0.0.1:7789")
		return
	}

	fmt.Printf("当前操作系统 (%s) 暂不支持自动注册系统服务，请直接运行: %s\n", runtime.GOOS, persistentBin)
}

func uninstallService() {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		uid := os.Getuid()
		_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, serviceLabel)).Run()
		plistPath := filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist")
		_ = os.Remove(plistPath)
		fmt.Printf("✓ 已停止并卸载常驻服务: %s\n", serviceLabel)
		return
	}

	if runtime.GOOS == "linux" {
		_ = exec.Command("systemctl", "--user", "disable", "--now", "xissue.service").Run()
		serviceFile := filepath.Join(home, ".config", "systemd", "user", "xissue.service")
		_ = os.Remove(serviceFile)
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		fmt.Println("✓ 已停止并卸载 systemd 服务: xissue.service")
		return
	}

	fmt.Println("当前操作系统不支持自动卸载。")
}

func statusService() {
	if runtime.GOOS == "darwin" {
		uid := os.Getuid()
		out, err := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/%s", uid, serviceLabel)).CombinedOutput()
		if err != nil {
			fmt.Printf("服务 %s 未加载或未运行。\n", serviceLabel)
		} else {
			lines := strings.Split(string(out), "\n")
			fmt.Printf("服务 %s 运行状态:\n", serviceLabel)
			hasCellarPath := false
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "state =") ||
					strings.HasPrefix(trimmed, "pid =") ||
					strings.HasPrefix(trimmed, "program =") {
					fmt.Printf("  %s\n", trimmed)
					if strings.Contains(trimmed, "/Cellar/xissue/") {
						hasCellarPath = true
					}
				}
			}
			if hasCellarPath {
				fmt.Printf("\n⚠️ 警告: 检测到当前常驻服务配置指向特定版本的 Cellar 物理路径（升级后将无法自动生效）。\n   运行 'xissue service restart' 即可自动迁移为版本无关的稳定软链接。\n")
			}
		}
		// 检查端口
		checkPort(7789)
		return
	}

	if runtime.GOOS == "linux" {
		out, _ := exec.Command("systemctl", "--user", "status", "xissue.service", "--no-pager").CombinedOutput()
		fmt.Println(string(out))
		if strings.Contains(string(out), "/Cellar/xissue/") {
			fmt.Printf("\n⚠️ 警告: 检测到当前 systemd 配置指向特定版本的 Cellar 物理路径。\n   运行 'xissue service restart' 即可自动迁移为稳定软链接。\n")
		}
		checkPort(7789)
		return
	}

	checkPort(7789)
}

func restartService() {
	// 如果检测到写死了固定版本的 Cellar 物理路径，自动迁移为稳定软链接
	if checkAndMigrateCellarService() {
		return
	}

	// 如果本地编译了新二进制，先自动同步到 ~/.local/bin/xissue
	if persistentBin, err := getPersistentBinary(); err == nil {
		_ = persistentBin
	}

	if runtime.GOOS == "darwin" {
		uid := os.Getuid()
		before := listenerPIDs(7789)
		kickstart := func() error {
			return exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", uid, serviceLabel)).Run()
		}
		// 优先委托 launchd 进行平滑重载 (-k: kill old process & restart immediately).
		// 注意：仅检查端口是否监听会被旧进程占用的端口骗过，必须确认监听 PID 已变化。
		if err := kickstart(); err == nil {
			if waitForListenerRestart(7789, before, 5*time.Second) {
				fmt.Printf("✓ 已重启服务: %s\n", serviceLabel)
				return
			}
			fmt.Println("⚠️ launchd 已响应，但端口仍由旧进程占用，正在停止旧进程后重试...")
		} else {
			fmt.Println("launchd 未响应重启请求，正在停止旧进程后重试...")
		}

		// 清理占用端口的旧进程（可能是升级前手动启动、brew services 或
		// 其他残留实例），再让 launchd 重新拉起当前服务。
		killPort(7789)
		if err := kickstart(); err == nil {
			if waitForListenerRestart(7789, before, 5*time.Second) {
				fmt.Printf("✓ 已重启服务: %s\n", serviceLabel)
				return
			}
		}

		// 最后兜底：重新注册服务。
		fmt.Println("服务仍未接管端口，正在重新安装并启动服务...")
		installService()
		return
	}

	if runtime.GOOS == "linux" {
		if err := exec.Command("systemctl", "--user", "restart", "xissue.service").Run(); err != nil {
			fmt.Println("服务未就绪，正在重新安装并启动服务...")
			installService()
			return
		}
		fmt.Println("✓ 已重启 systemd 服务: xissue.service")
		return
	}

	fmt.Println("请手动重启 xissue 进程。")
}

func listenerPIDs(port int) []int {
	out, err := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%d", port), "-sTCP:LISTEN").CombinedOutput()
	if err != nil {
		return nil
	}
	var pids []int
	for _, field := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(field); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

func isPortListening(port int) bool {
	return len(listenerPIDs(port)) > 0
}

// waitForListenerRestart waits until at least one listener PID differs from
// the snapshot taken before the restart. A still-listening old PID is not a
// successful restart.
func waitForListenerRestart(port int, before []int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if hasNewListener(before, listenerPIDs(port)) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func hasNewListener(before, now []int) bool {
	old := make(map[int]bool, len(before))
	for _, pid := range before {
		old[pid] = true
	}
	for _, pid := range now {
		if !old[pid] {
			return true
		}
	}
	return false
}

func checkPort(port int) {
	out, err := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port)).CombinedOutput()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		fmt.Printf("\n端口 %d 监听正常:\n%s\n", port, strings.TrimSpace(string(out)))
	} else {
		fmt.Printf("\n提示: 端口 %d 当前未被监听\n", port)
	}
}

func killPort(port int) {
	// 仅查找真正处于 LISTEN 状态的监听进程，避免误杀连接中的客户端 socket。
	pids := listenerPIDs(port)
	if len(pids) == 0 {
		return
	}

	// 优先发送 SIGTERM 优雅退出
	for _, pid := range pids {
		_ = terminateProcess(pid)
	}

	// 等待 1.5 秒确认端口是否释放
	for i := 0; i < 15; i++ {
		time.Sleep(100 * time.Millisecond)
		if !isPortListening(port) {
			return
		}
	}

	// 超时后若依然占用，强制发送 SIGKILL
	for _, pid := range pids {
		_ = killProcess(pid)
	}
	time.Sleep(200 * time.Millisecond)
}
