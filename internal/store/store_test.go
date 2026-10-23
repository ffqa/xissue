package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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

func TestDeleteProjectCascading(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	p, err := st.CreateProject(ctx, "Proj1", "desc")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	repo, err := st.CreateRepo(ctx, domain.Repo{
		ProjectID:     p.ID,
		Name:          "repo1",
		Provider:      "github",
		RemoteURL:     "https://github.com/test/repo1",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	iss, err := st.CreateLocalIssue(ctx, p.ID, "Issue 1", "body", nil)
	if err != nil {
		t.Fatalf("CreateLocalIssue: %v", err)
	}

	_, err = st.AddComment(ctx, iss.ID, "user", "comment 1")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	run, err := st.CreateRun(ctx, iss.ID, "opencode", "default")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	_, err = st.AppendEvent(ctx, run.ID, "user", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	_, err = st.UpsertBranchWatch(ctx, p.ID, repo.ID, "main", "sha1", "msg", "author")
	if err != nil {
		t.Fatalf("UpsertBranchWatch: %v", err)
	}

	// Now delete project
	if err := st.DeleteProject(ctx, p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	// Verify all cascades
	repos, err := st.ListRepos(ctx, p.ID)
	if err != nil || len(repos) > 0 {
		t.Errorf("expected 0 repos, got %d, err=%v", len(repos), err)
	}

	issues, err := st.ListIssues(ctx, p.ID)
	if err != nil || len(issues) > 0 {
		t.Errorf("expected 0 issues, got %d, err=%v", len(issues), err)
	}

	runs, err := st.ListRuns(ctx, iss.ID)
	if err != nil || len(runs) > 0 {
		t.Errorf("expected 0 runs, got %d, err=%v", len(runs), err)
	}

	watches, err := st.ListBranchWatches(ctx, p.ID)
	if err != nil || len(watches) > 0 {
		t.Errorf("expected 0 watches, got %d, err=%v", len(watches), err)
	}
}

func TestProjectPullBranches(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	p, err := st.CreateProject(ctx, "Branches", "desc")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.PullBranches == nil || len(p.PullBranches) != 0 {
		t.Fatalf("expected empty pull branches, got %#v", p.PullBranches)
	}
	branches := []string{" main ", "dev", ""}
	updated, err := st.UpdateProject(ctx, p.ID, store.ProjectPatch{PullBranches: &branches})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	want := []string{"main", "dev"}
	if len(updated.PullBranches) != len(want) {
		t.Fatalf("pull branches = %#v, want %#v", updated.PullBranches, want)
	}
	for i := range want {
		if updated.PullBranches[i] != want[i] {
			t.Fatalf("pull branches = %#v, want %#v", updated.PullBranches, want)
		}
	}
	got, err := st.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if len(got.PullBranches) != 2 || got.PullBranches[0] != "main" || got.PullBranches[1] != "dev" {
		t.Fatalf("re-fetched pull branches = %#v", got.PullBranches)
	}
}

func TestUpsertRemoteCommentIsIdempotent(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proj, err := st.CreateProject(ctx, "Comments", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "Issue", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddComment(ctx, issue.ID, "me", "local note"); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	added, updated, err := st.UpsertRemoteComment(ctx, issue.ID, "IC_1", "alice", "first", "https://example.test/1", created)
	if err != nil || !added || updated {
		t.Fatalf("first upsert = added:%v updated:%v err:%v", added, updated, err)
	}
	added, updated, err = st.UpsertRemoteComment(ctx, issue.ID, "IC_1", "alice", "first", "https://example.test/1", created)
	if err != nil || added || updated {
		t.Fatalf("duplicate upsert = added:%v updated:%v err:%v", added, updated, err)
	}
	added, updated, err = st.UpsertRemoteComment(ctx, issue.ID, "IC_1", "alice", "edited", "https://example.test/1", created)
	if err != nil || added || !updated {
		t.Fatalf("edited upsert = added:%v updated:%v err:%v", added, updated, err)
	}
	comments, err := st.ListComments(ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 || comments[0].Body != "edited" || comments[0].RemoteID != "IC_1" || comments[1].Body != "local note" {
		t.Fatalf("comments = %+v", comments)
	}
}

func TestUpsertRemoteCommentAdoptsLocalCopy(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proj, err := st.CreateProject(ctx, "Adopt", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "Issue", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	body := "Submitted branch `agent/x`."
	if _, err := st.AddComment(ctx, issue.ID, "system", "```text\n"+body+"\n```"); err != nil {
		t.Fatal(err)
	}
	added, updated, err := st.UpsertRemoteComment(ctx, issue.ID, "IC_9", "me", body, "https://example.test/9", time.Now())
	if err != nil || added || !updated {
		t.Fatalf("adopt = added:%v updated:%v err:%v", added, updated, err)
	}
	comments, err := st.ListComments(ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].RemoteID != "IC_9" || comments[0].Author != "system" {
		t.Fatalf("comments = %+v", comments)
	}
}
