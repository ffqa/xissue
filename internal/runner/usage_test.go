package runner

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

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
