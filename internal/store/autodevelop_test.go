package store_test

import (
	"context"
	"path/filepath"
	"testing"

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
	updated, err := st.UpdateProject(ctx, p.ID, store.ProjectPatch{
		AutoDevelop:       &enabled,
		AutoDevelopAgent:  &agent,
		AutoDevelopLabels: &labels,
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

	got, err := st.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if !got.AutoDevelop || got.AutoDevelopAgent != agent || len(got.AutoDevelopLabels) != 2 {
		t.Fatalf("round trip mismatch: %+v", got)
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
}
