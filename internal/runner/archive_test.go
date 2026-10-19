package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/store"
)

type dummyNotifier struct{}

func (d *dummyNotifier) BroadcastRunEvent(runID, issueID int64, ev *domain.RunEvent) {}
func (d *dummyNotifier) BroadcastRunUpdated(r *domain.Run)                            {}
func (d *dummyNotifier) BroadcastIssueUpdated(i *domain.Issue)                        {}
func (d *dummyNotifier) BroadcastNotification(n *domain.Notification)                 {}

func TestArchiveRun(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = tmpDir

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	proj, err := st.CreateProject(ctx, "Test Project", "desc")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "Test Issue", "body", nil)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	run, err := st.CreateRun(ctx, issue.ID, "opencode", "default")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	changeRec := "本次改动：\n a. 修复测试\n分支：cp/main : main\nPR：\n备注：test"
	stSubmitted := domain.RunSubmitted
	run, _ = st.UpdateRun(ctx, run.ID, store.RunUpdate{
		Status:       &stSubmitted,
		ChangeRecord: &changeRec,
		Ended:        true,
	})

	_, _ = st.AppendEvent(ctx, run.ID, "user", map[string]any{"text": "hello agent"})
	_, _ = st.AppendEvent(ctx, run.ID, "thought", map[string]any{"text": "thinking about solution"})
	_, _ = st.AppendEvent(ctx, run.ID, "tool_call", map[string]any{"kind": "read", "title": "read main.go", "input": map[string]any{"path": "main.go"}})
	_, _ = st.AppendEvent(ctx, run.ID, "message", map[string]any{"role": "assistant", "text": "task finished"})

	r := New(cfg, st, &dummyNotifier{})
	defer r.Shutdown()

	dir, err := r.ArchiveRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ArchiveRun failed: %v", err)
	}

	analysisPath := filepath.Join(dir, "analysis.md")
	data, err := os.ReadFile(analysisPath)
	if err != nil {
		t.Fatalf("read analysis.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[LOCAL-1]") {
		t.Errorf("expected [LOCAL-1] in analysis.md, got %s", content)
	}
	if !strings.Contains(content, "thinking about solution") {
		t.Errorf("expected thought in analysis.md, got %s", content)
	}
	if !strings.Contains(content, "本次改动") {
		t.Errorf("expected change record in analysis.md, got %s", content)
	}

	eventsPath := filepath.Join(dir, "events.log")
	evData, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read events.log: %v", err)
	}
	if !strings.Contains(string(evData), "[USER] hello agent") {
		t.Errorf("expected event log line in events.log, got %s", string(evData))
	}

	taskReadmePath := filepath.Join(filepath.Dir(dir), "README.md")
	readmeData, err := os.ReadFile(taskReadmePath)
	if err != nil {
		t.Fatalf("read task README.md: %v", err)
	}
	if !strings.Contains(string(readmeData), "Run History") {
		t.Errorf("expected Run History in task README.md, got %s", string(readmeData))
	}
}
