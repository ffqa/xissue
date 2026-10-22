package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/store"
)

func TestProjectAutoDevelopRoundTrip(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	p, err := st.CreateProject(ctx, "Auto", "desc")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.AutoDevelop {
		t.Fatal("auto develop should default to disabled")
	}
	if len(p.AutoDevelopLabels) == 0 {
		t.Fatal("expected default auto-develop labels")
	}

	enabled := true
	agent := "opencode"
	labels := []string{"bug", " 需求 ", ""}
	useText := true
	authors := []string{"@alice", " bob "}
	limit := 3
	updated, err := st.UpdateProject(ctx, p.ID, store.ProjectPatch{
		AutoDevelop:           &enabled,
		AutoDevelopAgent:      &agent,
		AutoDevelopLabels:     &labels,
		AutoDevelopText:       &useText,
		AutoDevelopAuthors:    &authors,
		AutoDevelopDailyLimit: &limit,
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if !updated.AutoDevelop || updated.AutoDevelopAgent != agent {
		t.Fatalf("unexpected project: %+v", updated)
	}
	if len(updated.AutoDevelopLabels) != 2 || updated.AutoDevelopLabels[1] != "需求" {
		t.Fatalf("labels not normalized: %#v", updated.AutoDevelopLabels)
	}
	if !updated.AutoDevelopText || updated.AutoDevelopDailyLimit != 3 {
		t.Fatalf("safety fields not saved: %+v", updated)
	}
	if len(updated.AutoDevelopAuthors) != 2 || updated.AutoDevelopAuthors[0] != "alice" {
		t.Fatalf("authors not normalized: %#v", updated.AutoDevelopAuthors)
	}

	got, err := st.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if !got.AutoDevelop || got.AutoDevelopAgent != agent || len(got.AutoDevelopLabels) != 2 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if !got.AutoDevelopText || len(got.AutoDevelopAuthors) != 2 || got.AutoDevelopDailyLimit != 3 {
		t.Fatalf("safety fields round trip mismatch: %+v", got)
	}

	// An explicitly empty list must stay empty (label-only mode), not fall back.
	empty := []string{}
	cleared, err := st.UpdateProject(ctx, p.ID, store.ProjectPatch{AutoDevelopLabels: &empty})
	if err != nil {
		t.Fatalf("clear labels: %v", err)
	}
	if len(cleared.AutoDevelopLabels) != 0 {
		t.Fatalf("empty labels should persist, got %#v", cleared.AutoDevelopLabels)
	}
}

func TestRunAutoPRFlag(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	p, err := st.CreateProject(ctx, "Auto", "desc")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	issue, err := st.CreateLocalIssue(ctx, p.ID, "title", "body", nil)
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	run, err := st.CreateRun(ctx, issue.ID, "opencode", "")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.AutoPR {
		t.Fatal("auto PR should default to false")
	}
	flag := true
	run, err = st.UpdateRun(ctx, run.ID, store.RunUpdate{AutoPR: &flag})
	if err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}
	if !run.AutoPR {
		t.Fatal("auto PR flag not persisted")
	}
	got, err := st.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if !got.AutoPR {
		t.Fatal("auto PR flag not read back")
	}
	count, err := st.CountAutoDevelopRunsSince(ctx, p.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CountAutoDevelopRunsSince: %v", err)
	}
	if count != 1 {
		t.Fatalf("auto develop count = %d, want 1", count)
	}
}
