package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/store"
)

func TestQueueFIFOAndCapacity(t *testing.T) {
	cfg := config.Default()
	cfg.Runner.MaxQueuedRuns = 2
	r := &Runner{
		cfg:         cfg,
		ctx:         context.Background(),
		budget:      newBudget(1, 0),
		queueSignal: make(chan struct{}, 1),
	}
	t1 := &queuedTurn{act: &activeRun{runID: 1}}
	t2 := &queuedTurn{act: &activeRun{runID: 2}}
	t3 := &queuedTurn{act: &activeRun{runID: 3}}

	if !r.reserveQueueSlot() || !r.reserveQueueSlot() {
		t.Fatal("first two queue slots should be available")
	}
	if r.reserveQueueSlot() {
		t.Fatal("third queue slot should be rejected at maxQueuedRuns=2")
	}
	r.commitQueueSlot(t1)
	r.commitQueueSlot(t2)
	if got := r.pendingTurns(); got != 2 {
		t.Fatalf("pending = %d, want 2", got)
	}
	if got := r.nextQueuedTurn(); got != t1 {
		t.Fatalf("first popped turn = %p, want %p", got, t1)
	}
	if got := r.nextQueuedTurn(); got != t2 {
		t.Fatalf("second popped turn = %p, want %p", got, t2)
	}
	if got := r.nextQueuedTurn(); got != nil {
		t.Fatalf("empty queue popped %p", got)
	}
	// A popped turn no longer consumes a pending slot.
	if !r.reserveQueueSlot() {
		t.Fatal("queue capacity should be free after pop")
	}
	r.commitQueueSlot(t3)
	if got := r.nextQueuedTurn(); got != t3 {
		t.Fatalf("third popped turn = %p, want %p", got, t3)
	}
}

func TestQueueUnlimited(t *testing.T) {
	cfg := config.Default()
	cfg.Runner.MaxQueuedRuns = 0
	r := &Runner{cfg: cfg, ctx: context.Background(), budget: newBudget(1, 0)}
	for i := range 3 {
		if !r.reserveQueueSlot() {
			t.Fatalf("unlimited queue rejected slot %d", i)
		}
	}
	if got := r.pendingTurns(); got != 0 {
		t.Fatalf("reserved-only pending = %d, want 0", got)
	}
}

func TestStartRunQueuesBehindConcurrencyLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake agent uses a POSIX shell script")
	}
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
      sleep 3600
      ;;
  esac
done
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Runner.MaxConcurrent = 1
	cfg.Runner.MaxQueuedRuns = 10
	cfg.Agents["fake"] = config.AgentConfig{Command: script, MemoryMB: 10}
	r := New(cfg, st, &dummyNotifier{})
	t.Cleanup(func() {
		r.Shutdown()
		_ = st.Close()
	})

	proj, err := st.CreateProject(ctx, "Queue", "")
	if err != nil {
		t.Fatal(err)
	}
	issue1, err := st.CreateLocalIssue(ctx, proj.ID, "first", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	issue2, err := st.CreateLocalIssue(ctx, proj.ID, "second", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run1, err := r.StartRun(ctx, issue1.ID, "fake", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, st, run1.ID, domain.RunRunning)
	run2, err := r.StartRun(ctx, issue2.ID, "fake", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitQueueStats(t, r, 1, 1)
	got2, err := st.GetRun(ctx, run2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Status != domain.RunQueued {
		t.Fatalf("second run status = %s, want queued", got2.Status)
	}
	iss2, err := st.GetIssue(ctx, issue2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if iss2.Status == domain.IssueRunning {
		t.Fatalf("queued issue should not be marked running: %+v", iss2)
	}
	if err := r.Cancel(run1.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, st, run2.ID, domain.RunRunning)
	if err := r.Cancel(run2.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, st, run2.ID, domain.RunCancelled)
}

func waitRunStatus(t *testing.T, st *store.Store, runID int64, want domain.RunStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := st.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	run, _ := st.GetRun(context.Background(), runID)
	t.Fatalf("run %d status = %s, want %s", runID, run.Status, want)
}

func waitQueueStats(t *testing.T, r *Runner, pending, running int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p, run, _, _ := r.QueueStats()
		if p == pending && run == running {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	p, run, _, _ := r.QueueStats()
	t.Fatalf("queue pending/running = %d/%d, want %d/%d", p, run, pending, running)
}
