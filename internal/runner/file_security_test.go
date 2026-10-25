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

func TestWriteFileRejectsSymlinkEscape(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	r := New(cfg, st, nil)

	proj, err := st.CreateProject(ctx, "Symlink", "")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := st.CreateRepo(ctx, domain.Repo{ProjectID: proj.ID, Name: "repo", RemoteURL: t.TempDir(), DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "Issue", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(ctx, issue.ID, "test", "")
	if err != nil {
		t.Fatal(err)
	}
	workdir := t.TempDir()
	if _, err := st.UpdateRun(ctx, run.ID, store.RunUpdate{Workdir: &workdir}); err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(workdir, repo.Name)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repoDir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := r.WriteFile(ctx, run.ID, repo.ID, "link/pwned.txt", "pwned"); err == nil {
		t.Fatal("WriteFile followed a symlink outside the repository")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); err == nil {
		t.Fatal("write escaped the repository worktree")
	}
	if _, err := r.ReadFile(ctx, run.ID, repo.ID, "link/secret.txt", "work"); err == nil {
		t.Fatal("ReadFile followed a symlink outside the repository")
	}
	if err := r.WriteFile(ctx, run.ID, repo.ID, "nested/new.txt", "ok"); err != nil {
		t.Fatalf("normal nested write failed: %v", err)
	}
	got, err := r.ReadFile(ctx, run.ID, repo.ID, "nested/new.txt", "work")
	if err != nil || got != "ok" {
		t.Fatalf("normal read = %q, %v", got, err)
	}
}

func TestWriteFileRejectsTraversal(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	r := New(cfg, st, nil)
	proj, _ := st.CreateProject(ctx, "Traversal", "")
	repo, _ := st.CreateRepo(ctx, domain.Repo{ProjectID: proj.ID, Name: "repo", RemoteURL: t.TempDir(), DefaultBranch: "main"})
	issue, _ := st.CreateLocalIssue(ctx, proj.ID, "Issue", "", nil)
	run, _ := st.CreateRun(ctx, issue.ID, "test", "")
	workdir := t.TempDir()
	_, _ = st.UpdateRun(ctx, run.ID, store.RunUpdate{Workdir: &workdir})
	if err := r.WriteFile(ctx, run.ID, repo.ID, "../escape.txt", "x"); err == nil {
		t.Fatal("WriteFile accepted a parent-directory path")
	}
	if err := r.WriteFile(ctx, run.ID, repo.ID, "/tmp/escape.txt", "x"); err == nil {
		t.Fatal("WriteFile accepted an absolute path")
	}
}

func TestRepoWorktreePathRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"../escape", "a/b", `a\b`, ".git", "..", ".hidden"} {
		if _, err := repoWorktreePath("/tmp/run", name); err == nil {
			t.Fatalf("repoWorktreePath accepted %q", name)
		}
	}
	for _, name := range []string{"repo", "repo-1", "Repo Two", "a.b"} {
		got, err := repoWorktreePath("/tmp/run", name)
		if err != nil || !strings.HasPrefix(got, filepath.Join("/tmp/run")+string(filepath.Separator)) {
			t.Fatalf("repoWorktreePath(%q) = %q, %v", name, got, err)
		}
	}
}
