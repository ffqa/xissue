package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
)

func TestRepoLatestTagPicksSemver(t *testing.T) {
	ctx := context.Background()
	srv, st, _ := newTestServer(t)

	repoDir := t.TempDir()
	mustGit := func(args ...string) {
		t.Helper()
		if _, err := gitx.Run(ctx, repoDir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	mustGit("init", "-b", "main")
	mustGit("config", "user.name", "Tester")
	mustGit("config", "user.email", "tester@example.com")
	if err := os.WriteFile(filepath.Join(repoDir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "-A")
	mustGit("commit", "-m", "init")
	mustGit("tag", "v1.9.0")
	mustGit("tag", "v1.10.0")
	mustGit("tag", "nightly")

	proj, err := st.CreateProject(ctx, "Versioned", "")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := st.CreateRepo(ctx, domain.Repo{ProjectID: proj.ID, Name: "app", Provider: domain.ProviderPlain, RemoteURL: repoDir, DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	mirror := filepath.Join(srv.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repo.ID))
	if err := gitx.EnsureMirror(ctx, repoDir, mirror); err != nil {
		t.Fatalf("EnsureMirror: %v", err)
	}
	if got := srv.repoLatestTag(ctx, repo.ID); got != "v1.10.0" {
		t.Fatalf("repoLatestTag = %q, want v1.10.0", got)
	}
}
