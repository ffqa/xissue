package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectQoderConfig(t *testing.T) {
	tempHome := t.TempDir()
	// 清空 PATH 以免受到当前机器真实已安装命令（如 qoderclicn）的干扰
	t.Setenv("PATH", "")

	// 1. 无任何目录时，默认返回国际版路径且 detected = false
	cfgPath, name, detected := detectQoderConfig(tempHome)
	if detected {
		t.Fatalf("expected detected=false when no qoder directory exists, got true")
	}
	expectedIntl := filepath.Join(tempHome, ".qoder", "mcp.json")
	if cfgPath != expectedIntl {
		t.Errorf("expected cfgPath=%q, got %q", expectedIntl, cfgPath)
	}

	// 2. 创建国内版目录 ~/.qoder-cn
	cnDir := filepath.Join(tempHome, ".qoder-cn")
	if err := os.MkdirAll(cnDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath, name, detected = detectQoderConfig(tempHome)
	if !detected {
		t.Errorf("expected detected=true when .qoder-cn exists")
	}
	expectedCn := filepath.Join(tempHome, ".qoder-cn", "mcp.json")
	if cfgPath != expectedCn {
		t.Errorf("expected cfgPath=%q, got %q", expectedCn, cfgPath)
	}
	if name != "Qoder-CN (~/.qoder-cn/mcp.json)" {
		t.Errorf("unexpected name: %s", name)
	}

	// 3. 同时创建国际版目录 ~/.qoder，国际版优先级更高
	intlDir := filepath.Join(tempHome, ".qoder")
	if err := os.MkdirAll(intlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath, name, detected = detectQoderConfig(tempHome)
	if !detected {
		t.Errorf("expected detected=true when .qoder exists")
	}
	if cfgPath != expectedIntl {
		t.Errorf("expected cfgPath=%q, got %q", expectedIntl, cfgPath)
	}
	if name != "Qoder (~/.qoder/mcp.json)" {
		t.Errorf("unexpected name: %s", name)
	}
}
