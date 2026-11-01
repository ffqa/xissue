package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
)

func TestSubmitMergeLocalConflictKeepsIssueInReview(t *testing.T) {
	ctx := context.Background()
	r, st, run, issue, repoDir, wt := newRunWorkspace(t)

	// Diverging edits on the same file force the pre-merge rebase to conflict.
	if err := os.WriteFile(filepath.Join(wt, "seed.txt"), []byte("from worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.CommitAll(ctx, wt, "worktree edit"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "seed.txt"), []byte("from main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.CommitAll(ctx, repoDir, "main edit"); err != nil {
		t.Fatal(err)
	}

	results, err := r.Submit(ctx, run.ID, SubmitRequest{Mode: "merge_local", TargetBranch: "main"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(results) != 1 || results[0].Error == "" {
		t.Fatalf("expected a merge error, got %+v", results)
	}

	got, err := st.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.RunFailed {
		t.Fatalf("run status = %q, want failed", got.Status)
	}
	if got.Error == "" {
		t.Fatal("failed merge run has empty error")
	}
	iss, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if iss.Status != domain.IssueReview {
		t.Fatalf("issue status = %q, want review", iss.Status)
	}
	// The pushed temp branch must not be left behind in the local repository.
	if out, err := gitx.Run(ctx, repoDir, "branch", "--list", got.Branch); err != nil || out != "" {
		t.Fatalf("temp branch left after failed merge: %q, %v", out, err)
	}
	// main still holds only its own commit.
	if out, err := gitx.Run(ctx, repoDir, "show", "main:seed.txt"); err != nil || out != "from main" {
		t.Fatalf("main was modified by a failed merge: %q, %v", out, err)
	}
}

func TestSubmitMergeLocalSuccessMarksIssueDone(t *testing.T) {
	ctx := context.Background()
	r, st, run, issue, repoDir, wt := newRunWorkspace(t)

	// main moves on in a different file; the run edits another one.
	if err := os.WriteFile(filepath.Join(repoDir, "other.txt"), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.CommitAll(ctx, repoDir, "main moves on"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.CommitAll(ctx, wt, "add feature"); err != nil {
		t.Fatal(err)
	}

	results, err := r.Submit(ctx, run.ID, SubmitRequest{Mode: "merge_local", TargetBranch: "main"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(results) != 1 || results[0].Error != "" {
		t.Fatalf("unexpected results: %+v", results)
	}
	if _, err := gitx.Run(ctx, repoDir, "show", "main:feature.txt"); err != nil {
		t.Fatalf("feature.txt missing from main after merge: %v", err)
	}
	got, err := st.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.RunSubmitted {
		t.Fatalf("run status = %q, want submitted", got.Status)
	}
	iss, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if iss.Status != domain.IssueDone {
		t.Fatalf("issue status = %q, want done", iss.Status)
	}
	if out, err := gitx.Run(ctx, repoDir, "branch", "--list", got.Branch); err != nil || out != "" {
		t.Fatalf("temp branch left after merge: %q, %v", out, err)
	}
}
