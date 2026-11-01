package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/store"
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

func TestMCPRunToolDefinitions(t *testing.T) {
	names := map[string]bool{}
	for _, def := range mcpToolDefinitions() {
		if n, ok := def["name"].(string); ok {
			names[n] = true
		}
	}
	for _, want := range []string{"xissue_get_run_changes", "xissue_send_message", "xissue_submit_run"} {
		if !names[want] {
			t.Fatalf("missing MCP tool %q", want)
		}
	}
	text, isErr := (&Server{}).callMCPTool(t.Context(), "xissue_submit_run", map[string]any{"runId": float64(1), "mode": "bogus"})
	if !isErr || !strings.Contains(text, "mode must be") {
		t.Fatalf("invalid mode call = %q, err=%v", text, isErr)
	}
}

// merge_local is UI-only: an unattended agent must not merge into the target
// branch, and auto-develop runs belong to the harness's own submit flow.
func TestMCPSubmitRunGuards(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ctx := t.Context()

	proj, err := st.CreateProject(ctx, "MCPGate", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "demo", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(ctx, issue.ID, "fake", "")
	if err != nil {
		t.Fatal(err)
	}

	text, isErr := srv.callMCPTool(ctx, "xissue_submit_run", map[string]any{"runId": float64(run.ID), "mode": "merge_local"})
	if !isErr || !strings.Contains(text, "merge_local") {
		t.Fatalf("merge_local via MCP = %q, err=%v", text, isErr)
	}

	autoPR := true
	run, err = st.UpdateRun(ctx, run.ID, store.RunUpdate{AutoPR: &autoPR})
	if err != nil {
		t.Fatal(err)
	}
	text, isErr = srv.callMCPTool(ctx, "xissue_submit_run", map[string]any{"runId": float64(run.ID), "mode": "pr"})
	if !isErr || !strings.Contains(text, "auto-develop") {
		t.Fatalf("auto-PR submit via MCP = %q, err=%v", text, isErr)
	}
}
