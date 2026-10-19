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
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法获取当前二进制路径: %w", err)
	}
	realExe, err := filepath.EvalSymlinks(exe)
	if err == nil {
		exe = realExe
	}

	// 如果已经在 Homebrew 标准路径或系统 bin 路径，直接使用该持久路径
	clean := filepath.ToSlash(exe)
	if strings.Contains(clean, "/Cellar/xissue/") ||
		strings.HasPrefix(clean, "/opt/homebrew/bin/") ||
		strings.HasPrefix(clean, "/usr/local/bin/") {
		return exe, nil
	}

	// 否则无论当前运行在 ~/Downloads、/tmp 还是源码目录，一律复制到 ~/.local/bin/xissue
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法获取用户家目录: %w", err)
	}
	targetDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("创建目录 %s 失败: %w", targetDir, err)
	}
	targetBin := filepath.Join(targetDir, "xissue")
	if runtime.GOOS == "windows" {
		targetBin += ".exe"
	}

	if exe != targetBin {
		fmt.Printf("正在将二进制复制到持久化路径: %s ...\n", targetBin)
		tmpTarget := targetBin + ".tmp"
		if err := copyExecutableFile(exe, tmpTarget); err != nil {
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
		_ = exec.Command("pkill", "-f", persistentBin).Run()
		_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, serviceLabel)).Run()
		if out, err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), targetPlist).CombinedOutput(); err != nil {
			// 如果 bootstrap 报错已存在，尝试 kickstart
			_ = exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", uid, serviceLabel)).Run()
			_ = out
		}

		fmt.Println("✓ 已成功安装为开机自启常驻服务 (macOS LaunchAgent):")
		fmt.Printf("  • 服务标签: %s\n", serviceLabel)
		fmt.Printf("  • 配置文件: %s\n", targetPlist)
		fmt.Printf("  • 二进制文件: %s (持久化路径，可安全删除解压目录)\n", persistentBin)
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
		fmt.Printf("  • 二进制文件: %s (持久化路径，可安全删除解压目录)\n", persistentBin)
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
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "state =") ||
					strings.HasPrefix(trimmed, "pid =") ||
					strings.HasPrefix(trimmed, "program =") {
					fmt.Printf("  %s\n", trimmed)
				}
			}
		}
		// 检查端口
		checkPort(7789)
		return
	}

	if runtime.GOOS == "linux" {
		out, _ := exec.Command("systemctl", "--user", "status", "xissue.service", "--no-pager").CombinedOutput()
		fmt.Println(string(out))
		checkPort(7789)
		return
	}

	checkPort(7789)
}

func restartService() {
	// 如果本地编译了新二进制，先自动同步到 ~/.local/bin/xissue
	if persistentBin, err := getPersistentBinary(); err == nil {
		_ = persistentBin
	}

	if runtime.GOOS == "darwin" {
		uid := os.Getuid()
		killPort(7789)
		if err := exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", uid, serviceLabel)).Run(); err != nil {
			fmt.Println("服务未就绪，正在重新安装并启动服务...")
			installService()
			return
		}
		fmt.Printf("✓ 已重启服务: %s\n", serviceLabel)
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

func checkPort(port int) {
	out, err := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port)).CombinedOutput()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		fmt.Printf("\n端口 %d 监听正常:\n%s\n", port, strings.TrimSpace(string(out)))
	} else {
		fmt.Printf("\n提示: 端口 %d 当前未被监听\n", port)
	}
}

func killPort(port int) {
	out, err := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%d", port)).CombinedOutput()
	if err == nil {
		for _, pidStr := range strings.Fields(string(out)) {
			if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
				p, _ := os.FindProcess(pid)
				if p != nil {
					_ = p.Kill()
				}
			}
		}
	}
}
