package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/store"
)

func TestProjectDevMode(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	// Default project creation: single_main
	p1, err := st.CreateProject(ctx, "Proj1", "desc1")
	if err != nil {
		t.Fatalf("CreateProject 1: %v", err)
	}
	if p1.DevMode != domain.DevModeSingleMain {
		t.Errorf("expected DevModeSingleMain, got %v", p1.DevMode)
	}
	if p1.TargetBranch != "main" {
		t.Errorf("expected default TargetBranch 'main', got %v", p1.TargetBranch)
	}

	// Create project with main_dev
	p2, err := st.CreateProject(ctx, "Proj2", "desc2", domain.DevModeMainDev)
	if err != nil {
		t.Fatalf("CreateProject 2: %v", err)
	}
	if p2.DevMode != domain.DevModeMainDev {
		t.Errorf("expected DevModeMainDev, got %v", p2.DevMode)
	}
	if p2.TargetBranch != "dev" {
		t.Errorf("expected default TargetBranch 'dev', got %v", p2.TargetBranch)
	}

	// Update project 1 to main_dev
	newMode := domain.DevModeMainDev
	newBranch := "dev"
	u1, err := st.UpdateProject(ctx, p1.ID, store.ProjectPatch{
		DevMode:      &newMode,
		TargetBranch: &newBranch,
	})
	if err != nil {
		t.Fatalf("UpdateProject 1: %v", err)
	}
	if u1.DevMode != domain.DevModeMainDev || u1.TargetBranch != "dev" {
		t.Errorf("unexpected updated values: %+v", u1)
	}

	// Re-fetch project 1
	got1, err := st.GetProject(ctx, p1.ID)
	if err != nil {
		t.Fatalf("GetProject 1: %v", err)
	}
	if got1.DevMode != domain.DevModeMainDev || got1.TargetBranch != "dev" {
		t.Errorf("re-fetched mismatch: %+v", got1)
	}
}
