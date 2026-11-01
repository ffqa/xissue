package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangesIncludesWorktreePathAndPatch(t *testing.T) {
	ctx := context.Background()
	r, _, run, _, _, wt := newRunWorkspace(t)
	if err := os.WriteFile(filepath.Join(wt, "new.txt"), []byte("hello patch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes, err := r.Changes(ctx, run.ID, true)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %+v", changes)
	}
	if changes[0].WorktreePath != wt {
		t.Fatalf("worktreePath = %q, want %q", changes[0].WorktreePath, wt)
	}
	if !strings.Contains(changes[0].Patch, "new.txt") || !strings.Contains(changes[0].Patch, "hello patch") {
		t.Fatalf("patch missing untracked file:\n%s", changes[0].Patch)
	}

	plain, err := r.Changes(ctx, run.ID, false)
	if err != nil {
		t.Fatalf("Changes without patch: %v", err)
	}
	if plain[0].Patch != "" {
		t.Fatalf("patch returned without ?patch=1: %q", plain[0].Patch)
	}
}
