package gitx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestDetectRepoRemoteDowngradesLocalOnlyRepo(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	if _, err := Run(ctx, dir, "init", "-b", "main"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	repo := domain.Repo{Name: "h5-v2", Provider: domain.ProviderGitHub, OwnerRepo: "acme/h5", RemoteURL: dir}
	if !DetectRepoRemote(ctx, &repo) {
		t.Fatal("expected detection to report a change")
	}
	if repo.Provider != domain.ProviderPlain || repo.OwnerRepo != "" {
		t.Fatalf("repo = %+v, want plain with empty owner", repo)
	}
}

func TestDetectRepoRemoteUpgradesLocalRepoWithOrigin(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	if _, err := Run(ctx, dir, "init", "-b", "main"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if _, err := Run(ctx, dir, "remote", "add", "origin", "https://github.com/acme/h5.git"); err != nil {
		t.Fatalf("remote add: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := domain.Repo{Name: "h5", Provider: domain.ProviderPlain, RemoteURL: dir}
	if !DetectRepoRemote(ctx, &repo) {
		t.Fatal("expected detection to report a change")
	}
	if repo.Provider != domain.ProviderGitHub || repo.OwnerRepo != "acme/h5" {
		t.Fatalf("repo = %+v, want github acme/h5", repo)
	}
}
