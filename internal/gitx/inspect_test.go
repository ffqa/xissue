package gitx

import (
	"context"
	"os/exec"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestDetectRepoRemote(t *testing.T) {
	tmpDir := t.TempDir()
	// Initialize git repo in tmpDir
	cmd := exec.Command("git", "init", tmpDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	repo := domain.Repo{
		ID:        1,
		ProjectID: 1,
		Name:      "test",
		Provider:  domain.ProviderPlain,
		RemoteURL: tmpDir,
	}

	ctx := context.Background()
	// Initially no remotes -> should not change
	changed := DetectRepoRemote(ctx, &repo)
	if changed {
		t.Errorf("expected no change without remote, got changed")
	}

	// Add remote origin pointing to GitHub
	cmd = exec.Command("git", "-C", tmpDir, "remote", "add", "origin", "https://github.com/ffqa/xissue.git")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	// Now DetectRepoRemote should recognize GitHub and ffqa/xissue
	changed = DetectRepoRemote(ctx, &repo)
	if !changed {
		t.Errorf("expected changed=true after adding remote origin")
	}
	if repo.Provider != domain.ProviderGitHub {
		t.Errorf("expected provider github, got %s", repo.Provider)
	}
	if repo.OwnerRepo != "ffqa/xissue" {
		t.Errorf("expected ownerRepo ffqa/xissue, got %s", repo.OwnerRepo)
	}
	if repo.RemoteURL != "https://github.com/ffqa/xissue.git" {
		t.Errorf("expected remoteUrl https://github.com/ffqa/xissue.git, got %s", repo.RemoteURL)
	}
}
