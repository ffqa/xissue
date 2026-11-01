package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/store"
)

// newRunWorkspace builds a project with one local repo, an issue and a run whose
// worktree is prepared on agent/<key>-run-1. It returns the run's workdir and the
// worktree path of repo "app".
func newRunWorkspace(t *testing.T) (r *Runner, st *store.Store, run *domain.Run, issue *domain.Issue, repoDir, wt string) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	r = New(cfg, st, &dummyNotifier{})
	t.Cleanup(func() {
		r.Shutdown()
		st.Close()
	})

	repoDir = t.TempDir()
	if _, err := gitx.Run(ctx, repoDir, "init", "-b", "main"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	_, _ = gitx.Run(ctx, repoDir, "config", "user.name", "Tester")
	_, _ = gitx.Run(ctx, repoDir, "config", "user.email", "tester@example.com")
	if err := os.WriteFile(filepath.Join(repoDir, "seed.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.CommitAll(ctx, repoDir, "init"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	proj, err := st.CreateProject(ctx, "Workspace", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRepo(ctx, domain.Repo{ProjectID: proj.ID, Name: "app", Provider: domain.ProviderPlain, RemoteURL: repoDir, DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	issue, err = st.CreateLocalIssue(ctx, proj.ID, "workspace", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err = st.CreateRun(ctx, issue.ID, "test", "")
	if err != nil {
		t.Fatal(err)
	}
	wd := filepath.Join(t.TempDir(), "run")
	branch := "agent/" + issue.Key + "-run-1"
	if run, err = st.UpdateRun(ctx, run.ID, store.RunUpdate{Workdir: &wd, Branch: &branch}); err != nil {
		t.Fatal(err)
	}
	if proj, err = st.GetProject(ctx, proj.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.prepareWorkspace(ctx, run, proj, issue); err != nil {
		t.Fatalf("prepareWorkspace: %v", err)
	}
	return r, st, run, issue, repoDir, filepath.Join(wd, "app")
}
