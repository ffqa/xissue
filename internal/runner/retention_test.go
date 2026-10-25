package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/store"
)

func TestQueueStreamEventCoalescesChunks(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	r := New(cfg, st, nil)
	t.Cleanup(r.Shutdown)
	proj, _ := st.CreateProject(ctx, "Stream", "")
	issue, _ := st.CreateLocalIssue(ctx, proj.ID, "issue", "", nil)
	run, _ := st.CreateRun(ctx, issue.ID, "test", "")
	act := &activeRun{runID: run.ID, issueID: issue.ID}
	r.queueStreamEvent(act, "stderr", map[string]any{"text": "line1\n"})
	r.queueStreamEvent(act, "stderr", map[string]any{"text": "line2\n"})
	r.queueStreamEvent(act, "message", map[string]any{"text": "hello "})
	r.queueStreamEvent(act, "message", map[string]any{"text": "world"})
	r.flushStreams(act)
	events, err := st.ListEvents(ctx, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("coalesced events = %d, want 2", len(events))
	}
	got := map[string]string{}
	for _, ev := range events {
		var data map[string]any
		_ = json.Unmarshal(ev.Data, &data)
		got[ev.Type], _ = data["text"].(string)
	}
	if got["stderr"] != "line1\nline2\n" || got["message"] != "hello world" {
		t.Fatalf("coalesced payloads = %#v", got)
	}
}

func TestPruneRunStorageArchivesDeletesEventsAndWorkdir(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Runner.EventRetentionDays = 1
	cfg.Runner.WorkspaceRetentionDays = 1
	r := New(cfg, st, nil)
	t.Cleanup(r.Shutdown)
	proj, _ := st.CreateProject(ctx, "Retention", "")
	issue, _ := st.CreateLocalIssue(ctx, proj.ID, "issue", "", nil)
	run, _ := st.CreateRun(ctx, issue.ID, "test", "")
	workdir := filepath.Join(cfg.RunsDir(), "run-1")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := domain.RunFailed
	if _, err := st.UpdateRun(ctx, run.ID, store.RunUpdate{Status: &done, Workdir: &workdir, Ended: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendEvent(ctx, run.ID, "message", map[string]any{"text": "done"}); err != nil {
		t.Fatal(err)
	}
	stats, err := r.PruneRunStorage(ctx, time.Now().Add(3*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if stats.EventRows != 1 || stats.Workdirs != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	events, err := st.ListEvents(ctx, run.ID, 0)
	if err != nil || len(events) != 0 {
		t.Fatalf("events after prune = %d, %v", len(events), err)
	}
	if _, err := os.Stat(workdir); !os.IsNotExist(err) {
		t.Fatalf("workdir still exists: %v", err)
	}
	updated, err := st.GetRun(ctx, run.ID)
	if err != nil || updated.Workdir != "" {
		t.Fatalf("workdir pointer = %q, %v", updated.Workdir, err)
	}
	archive := filepath.Join(cfg.DataDir, "issue", sanitizeFileName(proj.Name), sanitizeFileName(issue.Key), "run-1")
	if _, err := os.Stat(filepath.Join(archive, "events.log")); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
}

func TestPruneRunStorageKeepsCompletedWorkspace(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Runner.EventRetentionDays = 1
	cfg.Runner.WorkspaceRetentionDays = 1
	r := New(cfg, st, nil)
	t.Cleanup(r.Shutdown)
	proj, _ := st.CreateProject(ctx, "Retention", "")
	issue, _ := st.CreateLocalIssue(ctx, proj.ID, "issue", "", nil)
	run, _ := st.CreateRun(ctx, issue.ID, "test", "")
	workdir := filepath.Join(cfg.RunsDir(), "run-1")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "a.txt"), []byte("unreviewed"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := domain.RunCompleted
	if _, err := st.UpdateRun(ctx, run.ID, store.RunUpdate{Status: &done, Workdir: &workdir, Ended: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendEvent(ctx, run.ID, "message", map[string]any{"text": "done"}); err != nil {
		t.Fatal(err)
	}
	stats, err := r.PruneRunStorage(ctx, time.Now().Add(3*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if stats.EventRows != 1 || stats.Workdirs != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if _, err := os.Stat(filepath.Join(workdir, "a.txt")); err != nil {
		t.Fatalf("completed workdir should survive: %v", err)
	}
	updated, err := st.GetRun(ctx, run.ID)
	if err != nil || updated.Workdir != workdir {
		t.Fatalf("workdir pointer = %q, %v", updated.Workdir, err)
	}
}

func TestPruneRunStorageKeepsSubmittedWorkspace(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Runner.EventRetentionDays = 1
	cfg.Runner.WorkspaceRetentionDays = 1
	r := New(cfg, st, nil)
	t.Cleanup(r.Shutdown)
	proj, _ := st.CreateProject(ctx, "Retention", "")
	issue, _ := st.CreateLocalIssue(ctx, proj.ID, "issue", "", nil)
	run, _ := st.CreateRun(ctx, issue.ID, "test", "")
	workdir := filepath.Join(cfg.RunsDir(), "run-1")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "unpushed.txt"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := domain.RunSubmitted
	if _, err := st.UpdateRun(ctx, run.ID, store.RunUpdate{Status: &done, Workdir: &workdir, Ended: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendEvent(ctx, run.ID, "message", map[string]any{"text": "submitted"}); err != nil {
		t.Fatal(err)
	}
	stats, err := r.PruneRunStorage(ctx, time.Now().Add(3*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if stats.EventRows != 1 || stats.Workdirs != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	if _, err := os.Stat(filepath.Join(workdir, "unpushed.txt")); err != nil {
		t.Fatalf("submitted workdir should survive partial push failures: %v", err)
	}
}
