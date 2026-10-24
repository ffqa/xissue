package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestProjectReadmesDiscoversEveryRepoRoot(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "beta")
	empty := filepath.Join(root, "empty")
	for _, dir := range []string{alpha, beta, empty} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(alpha, "README.md"), []byte("# Alpha\n\nAlpha overview.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beta, "readme.markdown"), []byte("# Beta\n\nBeta overview.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	project := &domain.Project{
		ID: 1,
		Repos: []domain.Repo{
			{ID: 11, Name: "alpha", RemoteURL: alpha, DefaultBranch: "main"},
			{ID: 12, Name: "beta", RemoteURL: beta, DefaultBranch: "main"},
			{ID: 13, Name: "empty", RemoteURL: empty, DefaultBranch: "main"},
		},
	}
	readmes, err := ProjectReadmes(context.Background(), t.TempDir(), project)
	if err != nil {
		t.Fatal(err)
	}
	if len(readmes) != 3 {
		t.Fatalf("readmes = %+v", readmes)
	}
	if !readmes[0].Found || readmes[0].Path != "README.md" || readmes[0].Title != "Alpha" {
		t.Fatalf("alpha readme = %+v", readmes[0])
	}
	if !readmes[1].Found || readmes[1].Path != "readme.markdown" || readmes[1].Title != "Beta" {
		t.Fatalf("beta readme = %+v", readmes[1])
	}
	if readmes[2].Found || readmes[2].Content != "" {
		t.Fatalf("empty readme = %+v", readmes[2])
	}
}
