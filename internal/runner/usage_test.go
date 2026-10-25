package runner

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/store"
)

func TestRunUsageFromACPUpdatesAndPromptResult(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "fake-acp.sh")
	body := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{},"agentInfo":{"name":"fake"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"fake-session"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"fake-session","update":{"sessionUpdate":"usage_update","used":1000,"size":200000,"cost":{"amount":0.02,"currency":"USD"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"fake-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ok"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":100,"outputTokens":20,"totalTokens":120}}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Monitor.Enabled = false
	cfg.Runner.MaxConcurrent = 1
	cfg.Agents["fake"] = config.AgentConfig{Command: script, MemoryMB: 10}
	r := New(cfg, st, &dummyNotifier{})
	t.Cleanup(func() {
		r.Shutdown()
		_ = st.Close()
	})
	proj, err := st.CreateProject(ctx, "Usage", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "usage", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := r.StartRun(ctx, issue.ID, "fake", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, st, run.ID, domain.RunCompleted)
	got, err := st.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.InputTokens != 100 || got.OutputTokens != 20 || got.TotalTokens != 120 {
		t.Fatalf("tokens = in:%d out:%d total:%d", got.InputTokens, got.OutputTokens, got.TotalTokens)
	}
	if got.ContextUsed != 1000 || got.ContextSize != 200000 {
		t.Fatalf("context = %d/%d", got.ContextUsed, got.ContextSize)
	}
	if math.Abs(got.CostAmount-0.02) > 1e-9 || got.CostCurrency != "USD" {
		t.Fatalf("cost = %f %s", got.CostAmount, got.CostCurrency)
	}
	if got.UsageUpdatedAt == nil {
		t.Fatal("usage timestamp not set")
	}
}

func TestPromptCostDoesNotDoubleCountLaterCumulativeUpdate(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := &Runner{st: st, cfg: config.Default()}
	proj, err := st.CreateProject(ctx, "Cost", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "cost", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(ctx, issue.ID, "fake", "")
	if err != nil {
		t.Fatal(err)
	}
	act := &activeRun{}
	r.recordTurnUsage(act, run.ID, issue.ID, &acp.Usage{
		Cost: &acp.Cost{Amount: 0.10, Currency: "USD"},
	})
	r.recordUsage(act, run.ID, issue.ID, map[string]any{
		"cost": map[string]any{"amount": 0.22, "currency": "USD"},
	})
	got, err := st.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got.CostAmount-0.22) > 1e-9 {
		t.Fatalf("cost = %f, want 0.22 (prompt cost was double counted)", got.CostAmount)
	}
	if math.Abs(got.UsageCumulativeCost-0.22) > 1e-9 {
		t.Fatalf("cumulative baseline = %f, want 0.22", got.UsageCumulativeCost)
	}
}

func TestRemoteNewSessionResetsCostBaseline(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := &Runner{st: st, cfg: config.Default()}
	proj, err := st.CreateProject(ctx, "Baseline", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "baseline", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(ctx, issue.ID, "fake", "")
	if err != nil {
		t.Fatal(err)
	}
	oldSession := "old"
	baseline := 1.2
	cost := 1.2
	if _, err := st.UpdateRun(ctx, run.ID, store.RunUpdate{
		SessionID: &oldSession, UsageCumulativeCost: &baseline, CostAmount: &cost,
	}); err != nil {
		t.Fatal(err)
	}
	r.UpdateRemoteWorkspace(run.ID, "", "new-session")
	got, err := st.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "new-session" {
		t.Fatalf("session = %q", got.SessionID)
	}
	if got.UsageCumulativeCost != 0 {
		t.Fatalf("cumulative baseline = %f, want 0", got.UsageCumulativeCost)
	}
	if math.Abs(got.CostAmount-1.2) > 1e-9 {
		t.Fatalf("total cost changed to %f", got.CostAmount)
	}
}
