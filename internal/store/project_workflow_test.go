package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestProjectWorkflowPatchAndNormalize(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proj, err := st.CreateProject(ctx, "Workflow", "")
	if err != nil {
		t.Fatal(err)
	}
	if proj.Workflow.Enabled() {
		t.Fatalf("workflow should default to off: %+v", proj.Workflow)
	}

	w := domain.ProjectWorkflow{DDD: true, TDD: true, UBMode: true, STE: true, TestLoop: true, MaxIterations: 0}
	got, err := st.UpdateProject(ctx, proj.ID, ProjectPatch{Workflow: &w})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Workflow.Enabled() || got.Workflow.MaxIterations != 2 {
		t.Fatalf("normalized workflow = %+v", got.Workflow)
	}

	w.MaxIterations = 99
	got, err = st.UpdateProject(ctx, proj.ID, ProjectPatch{Workflow: &w})
	if err != nil || got.Workflow.MaxIterations != 5 {
		t.Fatalf("clamped workflow = %+v, %v", got.Workflow, err)
	}

	// Reload from disk to confirm the JSON column round-trips.
	again, err := st.GetProject(ctx, proj.ID)
	if err != nil || !again.Workflow.UBMode || !again.Workflow.TestLoop {
		t.Fatalf("reloaded workflow = %+v, %v", again.Workflow, err)
	}
}
