package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/domain"
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
	for _, want := range []string{
		"xissue_get_run_changes", "xissue_send_message", "xissue_submit_run",
		"xissue_list_project_todos", "xissue_add_todo", "xissue_add_todos", "xissue_update_todo", "xissue_delete_todo",
		"xissue_list_project_test_plans", "xissue_add_test_plan", "xissue_add_test_plans", "xissue_update_test_plan", "xissue_delete_test_plan",
	} {
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

func TestMCPProjectTaskTools(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ctx := context.Background()
	proj, err := st.CreateProject(ctx, "MCP tasks", "")
	if err != nil {
		t.Fatal(err)
	}
	pid := float64(proj.ID)

	text, isErr := srv.callMCPTool(ctx, "xissue_add_todos", map[string]any{
		"projectId": pid,
		"items": []any{
			map[string]any{"title": "登录流程", "sourceKey": "SPEC-1"},
			map[string]any{"title": "导出报表"},
		},
	})
	if isErr {
		t.Fatalf("add_todos: %s", text)
	}
	var todos []domain.ProjectTodo
	if err := json.Unmarshal([]byte(text), &todos); err != nil || len(todos) != 2 {
		t.Fatalf("add_todos result = %s (%v)", text, err)
	}

	text, isErr = srv.callMCPTool(ctx, "xissue_list_project_todos", map[string]any{"projectId": pid, "q": "登录"})
	if isErr {
		t.Fatalf("list todos: %s", text)
	}
	var found []domain.ProjectTodo
	if err := json.Unmarshal([]byte(text), &found); err != nil || len(found) != 1 || found[0].Title != "登录流程" {
		t.Fatalf("search result = %s (%v)", text, err)
	}

	text, isErr = srv.callMCPTool(ctx, "xissue_update_todo", map[string]any{"todoId": float64(todos[0].ID), "status": "done"})
	if isErr {
		t.Fatalf("update todo: %s", text)
	}
	var done domain.ProjectTodo
	if err := json.Unmarshal([]byte(text), &done); err != nil || done.Status != "done" || done.CompletedAt == nil {
		t.Fatalf("update todo result = %s", text)
	}

	text, isErr = srv.callMCPTool(ctx, "xissue_add_test_plans", map[string]any{
		"projectId": pid,
		"items": []any{
			map[string]any{"title": "回归登录"},
			map[string]any{"title": "回归登录"},
		},
	})
	if isErr {
		t.Fatalf("add_test_plans: %s", text)
	}
	var plans []domain.ProjectTestPlan
	if err := json.Unmarshal([]byte(text), &plans); err != nil || len(plans) != 2 {
		t.Fatalf("add_test_plans result = %s (%v)", text, err)
	}
	if plans[0].Title != "回归登录 x1" || plans[1].Title != "回归登录 x2" {
		t.Fatalf("plan numbering = %q, %q", plans[0].Title, plans[1].Title)
	}

	text, isErr = srv.callMCPTool(ctx, "xissue_update_test_plan", map[string]any{"planId": float64(plans[0].ID), "status": "done"})
	if isErr {
		t.Fatalf("update plan: %s", text)
	}
	text, isErr = srv.callMCPTool(ctx, "xissue_list_project_test_plans", map[string]any{"projectId": pid, "status": "done", "q": "x1"})
	if isErr {
		t.Fatalf("list plans: %s", text)
	}
	var donePlans []domain.ProjectTestPlan
	if err := json.Unmarshal([]byte(text), &donePlans); err != nil || len(donePlans) != 1 {
		t.Fatalf("done plans = %s (%v)", text, err)
	}

	text, isErr = srv.callMCPTool(ctx, "xissue_delete_todo", map[string]any{"todoId": float64(todos[0].ID)})
	if isErr || !strings.Contains(text, "\"ok\"") {
		t.Fatalf("delete todo = %s", text)
	}
	text, isErr = srv.callMCPTool(ctx, "xissue_delete_test_plan", map[string]any{"planId": float64(plans[0].ID)})
	if isErr || !strings.Contains(text, "\"ok\"") {
		t.Fatalf("delete plan = %s", text)
	}
}
