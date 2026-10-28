package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAndDeleteMergedBranchesOnlyMine(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	remote := filepath.Join(root, "remote.git")
	mirror := filepath.Join(root, "mirror.git")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit := func(dir string, args ...string) string {
		t.Helper()
		out, err := Run(ctx, dir, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	mustGit(work, "init", "-b", "main")
	mustGit(work, "config", "user.name", "Mine")
	mustGit(work, "config", "user.email", "mine@example.com")
	mustGit(work, "commit", "--allow-empty", "-m", "init")
	mustGit(work, "checkout", "-b", "merged-mine")
	mustGit(work, "commit", "--allow-empty", "-m", "mine feature")
	mustGit(work, "checkout", "main")
	mustGit(work, "merge", "--no-ff", "-m", "merge mine", "merged-mine")
	mustGit(work, "checkout", "-b", "open-mine")
	mustGit(work, "commit", "--allow-empty", "-m", "unmerged feature")
	mustGit(work, "checkout", "main")
	mustGit(work, "checkout", "-b", "merged-other")
	mustGit(work, "-c", "user.name=Other", "-c", "user.email=other@example.com", "commit", "--allow-empty", "-m", "other feature")
	mustGit(work, "checkout", "main")
	mustGit(work, "merge", "--no-ff", "-m", "merge other", "merged-other")
	if _, err := Run(ctx, root, "init", "--bare", remote); err != nil {
		t.Fatal(err)
	}
	mustGit(work, "remote", "add", "origin", remote)
	mustGit(work, "push", "origin", "--all")
	if err := EnsureMirror(ctx, remote, mirror); err != nil {
		t.Fatal(err)
	}
	name, email, err := GitIdentity(ctx, work)
	if err != nil || name != "Mine" || email != "mine@example.com" {
		t.Fatalf("identity = %q %q %v", name, email, err)
	}
	branches, err := ListMergedBranches(ctx, mirror, "main", name, email)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0].Name != "merged-mine" {
		t.Fatalf("merged branches = %+v", branches)
	}
	if err := DeleteRemoteBranch(ctx, mirror, "merged-mine"); err != nil {
		t.Fatal(err)
	}
	out, err := Run(ctx, remote, "branch", "--list", "merged-mine")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("branch still exists: %q", out)
	}
}
