package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBranchName(t *testing.T) {
	valids := []string{"main", "dev", "feat-xxx", "feat/login", "user_123.test", "cp/main-123"}
	for _, b := range valids {
		if err := ValidateBranchName(b); err != nil {
			t.Errorf("expected %q to be valid, got: %v", b, err)
		}
	}

	invalids := []string{"", "-flag", "--opt", "feat space", "branch..name", "branch~1", "branch^", "b:name", "b?name", "b*name", "b[name"}
	for _, b := range invalids {
		if err := ValidateBranchName(b); err == nil {
			t.Errorf("expected %q to be invalid, got nil", b)
		}
	}
}

func TestValidateSHA(t *testing.T) {
	valids := []string{"66ad2c1", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", "1234567"}
	for _, sha := range valids {
		if err := ValidateSHA(sha); err != nil {
			t.Errorf("expected %q to be valid, got: %v", sha, err)
		}
	}

	invalids := []string{"", "123", "12345", "66ad2c1z", "66ad 2c1", "-flag"}
	for _, sha := range invalids {
		if err := ValidateSHA(sha); err == nil {
			t.Errorf("expected %q to be invalid, got nil", sha)
		}
	}
}

func TestMergeBranchToTargetLocal(t *testing.T) {
	ctx := t.Context()
	localDir := t.TempDir()

	// Init local repo with main branch and an initial commit
	if _, err := Run(ctx, localDir, "init", "-b", "main"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	_, _ = Run(ctx, localDir, "config", "user.name", "Tester")
	_, _ = Run(ctx, localDir, "config", "user.email", "tester@example.com")
	if _, err := Run(ctx, localDir, "commit", "--allow-empty", "-m", "init"); err != nil {
		t.Fatalf("initial commit: %v", err)
	}

	// Create a bare mirror
	mirrorDir := t.TempDir()
	if err := EnsureMirror(ctx, localDir, mirrorDir); err != nil {
		t.Fatalf("EnsureMirror: %v", err)
	}

	// Create worktree on branch feat-1
	wtDir := t.TempDir()
	if err := AddWorktree(ctx, mirrorDir, wtDir, "feat-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	defer RemoveWorktree(ctx, mirrorDir, wtDir, "feat-1")

	// Commit a new file in worktree
	newFile := filepath.Join(wtDir, "hello.txt")
	if err := os.WriteFile(newFile, []byte("world\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	sha, err := CommitAll(ctx, wtDir, "add hello.txt")
	if err != nil {
		t.Fatalf("CommitAll: %v", err)
	}

	// Merge branch feat-1 into main
	headSHA, err := MergeBranchToTarget(ctx, wtDir, mirrorDir, localDir, "feat-1", "main", "merge feat-1")
	if err != nil {
		t.Fatalf("MergeBranchToTarget: %v", err)
	}
	if headSHA != sha {
		t.Errorf("expected headSHA %s, got %s", sha, headSHA)
	}

	// Verify localDir has hello.txt
	content, err := os.ReadFile(filepath.Join(localDir, "hello.txt"))
	if err != nil {
		t.Fatalf("localDir missing hello.txt after merge: %v", err)
	}
	if string(content) != "world\n" {
		t.Errorf("unexpected content: %q", string(content))
	}
}

func TestAddWorktreeRestoresExistingRemoteBranch(t *testing.T) {
	ctx := t.Context()
	localDir := t.TempDir()
	if _, err := Run(ctx, localDir, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	_, _ = Run(ctx, localDir, "config", "user.name", "Tester")
	_, _ = Run(ctx, localDir, "config", "user.email", "tester@example.com")
	if _, err := Run(ctx, localDir, "commit", "--allow-empty", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	mirrorDir := t.TempDir()
	if err := EnsureMirror(ctx, localDir, mirrorDir); err != nil {
		t.Fatal(err)
	}
	wtDir := t.TempDir()
	if err := AddWorktree(ctx, mirrorDir, wtDir, "feat-restore", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "restored.txt"), []byte("kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := CommitAll(ctx, wtDir, "branch work")
	if err != nil {
		t.Fatal(err)
	}
	if err := Push(ctx, wtDir, "feat-restore"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveWorktree(ctx, mirrorDir, wtDir, "feat-restore"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMirror(ctx, localDir, mirrorDir); err != nil {
		t.Fatal(err)
	}
	restored := t.TempDir()
	if err := AddWorktree(ctx, mirrorDir, restored, "feat-restore", "main"); err != nil {
		t.Fatal(err)
	}
	defer RemoveWorktree(ctx, mirrorDir, restored, "feat-restore")
	if _, err := os.Stat(filepath.Join(restored, "restored.txt")); err != nil {
		t.Fatalf("restored worktree lost branch content: %v", err)
	}
	head, err := Run(ctx, restored, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(head) != sha {
		t.Fatalf("restored HEAD = %s, want %s", strings.TrimSpace(head), sha)
	}
}
