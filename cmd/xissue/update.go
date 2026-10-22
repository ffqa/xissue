package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/server"
	"github.com/xpier/xissue/internal/updater"
)

func runUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	checkOnly := fs.Bool("check", false, "仅检查是否有新版本，不执行升级")
	force := fs.Bool("force", false, "跳过完整性校验强制执行升级 (默认 fail-closed 校验)")
	_ = fs.Parse(args)

	currentVer := server.Version
	fmt.Printf("xIssue 当前版本: v%s (%s/%s)\n", currentVer, runtime.GOOS, runtime.GOARCH)

	exePath, _ := os.Executable()
	if realPath, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = realPath
	}

	isBrew := isHomebrewInstall(exePath)
	if isBrew {
		fmt.Printf("检测到安装方式: Homebrew (%s)\n", exePath)
		handleBrewUpdate(*checkOnly)
		return
	}

	fmt.Printf("检测到安装方式: 独立二进制 (%s)\n", exePath)
	handleBinaryUpdate(currentVer, exePath, *checkOnly, *force)
}

func isHomebrewInstall(exePath string) bool {
	clean := filepath.ToSlash(exePath)
	if strings.Contains(clean, "/Cellar/xissue/") ||
		strings.Contains(clean, "/opt/homebrew/") ||
		strings.Contains(clean, "/usr/local/Cellar/xissue/") {
		return true
	}

	// 尝试检查 brew list
	if _, err := exec.LookPath("brew"); err == nil {
		out, err := exec.Command("brew", "list", "--formula", "xissue").CombinedOutput()
		if err == nil && strings.Contains(string(out), "xissue") {
			return true
		}
		out, err = exec.Command("brew", "list", "--formula", "ffqa/tap/xissue").CombinedOutput()
		if err == nil && strings.Contains(string(out), "xissue") {
			return true
		}
	}
	return false
}

func handleBrewUpdate(checkOnly bool) {
	if _, err := exec.LookPath("brew"); err != nil {
		fmt.Println("未找到 brew 命令，请检查 PATH 设置。")
		return
	}

	fmt.Println("正在通过 Homebrew 检查更新...")
	_ = exec.Command("brew", "update").Run()

	out, err := exec.Command("brew", "outdated", "ffqa/tap/xissue").CombinedOutput()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		out, _ = exec.Command("brew", "outdated", "xissue").CombinedOutput()
	}

	hasUpdate := len(strings.TrimSpace(string(out))) > 0
	if !hasUpdate {
		fmt.Println("已经是最新版本，无需更新。")
		return
	}

	fmt.Printf("发现可升级版本:\n%s\n", strings.TrimSpace(string(out)))
	if checkOnly {
		fmt.Println("运行 'xissue update' 即可完成升级。")
		return
	}

	fmt.Println("正在执行 brew upgrade...")
	cmd := exec.Command("brew", "upgrade", "ffqa/tap/xissue")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		cmdFallback := exec.Command("brew", "upgrade", "xissue")
		cmdFallback.Stdout = os.Stdout
		cmdFallback.Stderr = os.Stderr
		_ = cmdFallback.Run()
	}

	fmt.Println("\n升级完成！若作为常驻后台服务运行，正在尝试重启服务...")
	restartServiceIfRunning()
}

func handleBinaryUpdate(currentVer, exePath string, checkOnly, force bool) {
	fmt.Println("正在检查 GitHub Releases 最新版本...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rel, err := updater.FetchLatest(ctx)
	if err != nil {
		fmt.Printf("获取最新版本失败: %v\n", err)
		return
	}

	latestVer := strings.TrimPrefix(rel.TagName, "v")
	if !updater.IsNewer(currentVer, latestVer) {
		fmt.Printf("已经是最新版本 (v%s)，无需更新。\n", currentVer)
		return
	}

	fmt.Printf("发现新版本: %s (当前: v%s)\n", rel.TagName, currentVer)
	if checkOnly {
		fmt.Println("运行 'xissue update' 即可自动下载并升级。")
		return
	}

	assetName, assetURL := updater.FindMatchingAsset(rel, runtime.GOOS, runtime.GOARCH)
	if assetURL == "" {
		fmt.Printf("未找到适配 %s-%s 的发布包，请访问 %s 手动下载。\n",
			runtime.GOOS, runtime.GOARCH, "https://github.com/ffqa/xissue/releases/latest")
		return
	}

	fmt.Printf("正在从 %s 下载发布包 (%s)...\n", assetURL, assetName)
	tmpArchive, err := downloadFile(assetURL)
	if err != nil {
		fmt.Printf("下载失败: %v\n", err)
		return
	}
	defer os.Remove(tmpArchive)

	// 完整性校验：下载 SHA256SUMS.txt 并校验包哈希 (默认严格 fail-closed，杜绝供应链投毒)
	sumsURL := updater.FindSumsURL(rel)
	if sumsURL == "" {
		if !force {
			fmt.Println("❌ 安全拦截: 未在发布包中找到 SHA256SUMS.txt 校验和文件。\n   为防范供应链风险已终止升级（可使用 'xissue update --force' 强制升级）。")
			return
		}
		fmt.Println("⚠️ 警告: 未找到 SHA256SUMS.txt，因使用 --force 参数跳过校验。")
	} else {
		fmt.Println("正在获取 SHA256SUMS.txt 进行完整性校验...")
		tmpSums, err := downloadFile(sumsURL)
		if err != nil {
			if !force {
				fmt.Printf("❌ 获取校验和文件失败: %v\n   为防范供应链风险已终止升级（可使用 'xissue update --force' 强制升级）。\n", err)
				return
			}
			fmt.Printf("⚠️ 警告: 获取校验和文件失败 (%v)，因使用 --force 参数跳过校验。\n", err)
		} else {
			defer os.Remove(tmpSums)
			sumsBytes, err := os.ReadFile(tmpSums)
			if err != nil {
				if !force {
					fmt.Printf("❌ 读取校验和文件失败: %v\n", err)
					return
				}
			} else {
				expectedSHA := parseChecksum(string(sumsBytes), assetName)
				if expectedSHA == "" {
					if !force {
						fmt.Printf("❌ 安全拦截: SHA256SUMS.txt 中未包含文件 %s 的校验和记录。\n   为防范供应链风险已终止升级（可使用 'xissue update --force' 强制升级）。\n", assetName)
						return
					}
					fmt.Printf("⚠️ 警告: 未找到 %s 的校验和记录，因使用 --force 参数跳过校验。\n", assetName)
				} else {
					actualSHA, err := computeFileSHA256(tmpArchive)
					if err != nil {
						fmt.Printf("计算本地文件校验和失败: %v\n", err)
						return
					}
					if !strings.EqualFold(actualSHA, expectedSHA) {
						fmt.Printf("❌ 校验和不匹配！升级包已被篡改或下载不完整！\n  预期: %s\n  实际: %s\n", expectedSHA, actualSHA)
						return
					}
					fmt.Printf("✓ 完整性校验通过 (SHA256: %s)\n", actualSHA)
				}
			}
		}
	}

	// 确定安装目标路径：若 exePath 可写且不是只读临时路径，则替换它；否则默认 ~/.local/bin/xissue
	targetBin := exePath
	home, _ := os.UserHomeDir()
	localBin := filepath.Join(home, ".local", "bin", "xissue")
	if runtime.GOOS == "windows" {
		localBin += ".exe"
	}

	// 如果当前 exe 位于临时目录或不可写，安装到 localBin
	if strings.Contains(exePath, "tmp") || strings.Contains(exePath, "Downloads") {
		targetBin = localBin
	}

	fmt.Printf("正在提取并安装到 %s ...\n", targetBin)
	if err := extractAndInstallBinary(tmpArchive, targetBin); err != nil {
		// 如果替换原路径失败（比如权限不够），尝试写入 ~/.local/bin/xissue
		if targetBin != localBin {
			fmt.Printf("写入 %s 失败，尝试安装到用户目录 %s ...\n", targetBin, localBin)
			if err2 := extractAndInstallBinary(tmpArchive, localBin); err2 == nil {
				targetBin = localBin
				err = nil
			}
		}
		if err != nil {
			fmt.Printf("安装失败: %v\n", err)
			return
		}
	}

	fmt.Printf("🎉 成功升级至 %s！已安装到: %s\n", rel.TagName, targetBin)
	restartServiceIfRunning()
}

func parseChecksum(sumsContent, fileName string) string {
	lines := strings.Split(sumsContent, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			sum := parts[0]
			name := filepath.Base(parts[1])
			if name == fileName || parts[1] == fileName {
				return strings.ToLower(sum)
			}
		}
	}
	return ""
}

func computeFileSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isAllowedDownloadURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return host == "github.com" || host == "objects.githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

func downloadFile(downloadURL string) (string, error) {
	if !isAllowedDownloadURL(downloadURL) {
		return "", fmt.Errorf("安全拦截: 非信任下载域 %q", downloadURL)
	}

	tmp, err := os.CreateTemp("", "xissue-update-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer tmp.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	req.Header.Set("User-Agent", "xissue-updater")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return tmpName, nil
}

func extractAndInstallBinary(archivePath, targetBin string) error {
	binName := "xissue"
	if runtime.GOOS == "windows" {
		binName = "xissue.exe"
	}

	if err := os.MkdirAll(filepath.Dir(targetBin), 0o755); err != nil {
		return err
	}

	tmpTarget := targetBin + ".tmp"
	_ = os.Remove(tmpTarget)

	if strings.HasSuffix(archivePath, ".zip") {
		r, err := zip.OpenReader(archivePath)
		if err != nil {
			return err
		}
		defer r.Close()

		var found bool
		for _, f := range r.File {
			if filepath.Base(f.Name) == binName {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				defer rc.Close()

				out, err := os.OpenFile(tmpTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
				if err != nil {
					return err
				}
				_, err = io.Copy(out, rc)
				out.Close()
				if err != nil {
					return err
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("压缩包中未找到 %s", binName)
		}
	} else {
		// tar.gz
		f, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer f.Close()

		gzr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gzr.Close()

		tr := tar.NewReader(gzr)
		var found bool
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if filepath.Base(hdr.Name) == binName && !hdr.FileInfo().IsDir() {
				out, err := os.OpenFile(tmpTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
				if err != nil {
					return err
				}
				_, err = io.Copy(out, tr)
				out.Close()
				if err != nil {
					return err
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("压缩包中未找到 %s", binName)
		}
	}

	// 清理 macOS quarantine
	if runtime.GOOS == "darwin" {
		_ = exec.Command("xattr", "-r", "-c", tmpTarget).Run()
		_ = exec.Command("xattr", "-dr", "com.apple.quarantine", tmpTarget).Run()
	}

	return os.Rename(tmpTarget, targetBin)
}

func restartServiceIfRunning() {
	// 如果旧配置中写死了特定版本 Cellar 物理路径，自动迁移并重启
	if checkAndMigrateCellarService() {
		return
	}

	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, ".xissue")
	scriptPath := filepath.Join(logDir, "scripts", "install-service.sh")

	if _, err := os.Stat(scriptPath); err == nil {
		out, _ := exec.Command("bash", scriptPath, "restart").CombinedOutput()
		fmt.Printf("服务重启结果: %s\n", strings.TrimSpace(string(out)))
		return
	}

	if runtime.GOOS == "darwin" {
		_ = exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/dev.xissue", os.Getuid())).Run()
	} else if runtime.GOOS == "linux" {
		_ = exec.Command("systemctl", "--user", "restart", "xissue.service").Run()
	}
}
