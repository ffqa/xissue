package runner

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/store"
)

func TestPRMutationsRejectRepoFromAnotherProject(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	r := New(cfg, st, nil)
	a, err := st.CreateProject(ctx, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateProject(ctx, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	repoB, err := st.CreateRepo(ctx, domain.Repo{ProjectID: b.ID, Name: "repo-b", Provider: domain.ProviderPlain, RemoteURL: t.TempDir(), DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.MergeProjectPR(ctx, a.ID, repoB.ID, 1, "squash"); err == nil || !strings.Contains(err.Error(), "不属于") {
		t.Fatalf("MergeProjectPR cross-project = %v, want ownership error", err)
	}
	if err := r.CloseProjectPR(ctx, a.ID, repoB.ID, 1); err == nil || !strings.Contains(err.Error(), "不属于") {
		t.Fatalf("CloseProjectPR cross-project = %v, want ownership error", err)
	}
	if _, err := r.CherryPick(ctx, CherryPickRequest{ProjectID: a.ID, RepoID: repoB.ID, SHAs: []string{"deadbeef"}}); err == nil || !strings.Contains(err.Error(), "不属于") {
		t.Fatalf("CherryPick cross-project = %v, want ownership error", err)
	}
}
