package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStableBrewBinary(t *testing.T) {
	// 1. 测试直接调用 Homebrew 标准软链接
	stable, ok := resolveStableBrewBinary("/usr/local/bin/xissue", "/usr/local/Cellar/xissue/0.2.8/bin/xissue")
	if !ok || stable != "/usr/local/bin/xissue" {
		t.Errorf("expected /usr/local/bin/xissue, got %q (ok=%v)", stable, ok)
	}

	stable, ok = resolveStableBrewBinary("/opt/homebrew/bin/xissue", "/opt/homebrew/Cellar/xissue/0.2.8/bin/xissue")
	if !ok || stable != "/opt/homebrew/bin/xissue" {
		t.Errorf("expected /opt/homebrew/bin/xissue, got %q (ok=%v)", stable, ok)
	}

	// 2. 测试即使 rawExe 已经是 Cellar 路径，也能反推出上层 bin 目录中的稳定软链接
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	cellarBinDir := filepath.Join(tmpDir, "Cellar", "xissue", "0.2.8", "bin")
	_ = os.MkdirAll(binDir, 0o755)
	_ = os.MkdirAll(cellarBinDir, 0o755)

	mockCellarBin := filepath.Join(cellarBinDir, "xissue")
	mockStableBin := filepath.Join(binDir, "xissue")
	_ = os.WriteFile(mockCellarBin, []byte("#!/bin/sh\n"), 0o755)
	_ = os.WriteFile(mockStableBin, []byte("#!/bin/sh\n"), 0o755)

	stable, ok = resolveStableBrewBinary(mockCellarBin, mockCellarBin)
	if !ok || stable != mockStableBin {
		t.Errorf("expected %q, got %q (ok=%v)", mockStableBin, stable, ok)
	}

	// 3. 非 Homebrew 路径应返回 false
	_, ok = resolveStableBrewBinary("/tmp/some_dir/xissue", "/tmp/some_dir/xissue")
	if ok {
		t.Errorf("expected non-brew path to return false, got true")
	}
}
