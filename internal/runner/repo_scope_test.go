package runner

import (
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestReposForIssue(t *testing.T) {
	api := domain.Repo{ID: 10, Name: "api"}
	web := domain.Repo{ID: 11, Name: "web"}
	proj := &domain.Project{Repos: []domain.Repo{api, web}}

	if got := reposForIssue(proj, &domain.Issue{}); len(got) != 2 {
		t.Fatalf("unscoped issue repos = %d, want 2", len(got))
	}
	repoID := int64(11)
	got := reposForIssue(proj, &domain.Issue{RepoID: &repoID})
	if len(got) != 1 || got[0].Name != "web" {
		t.Fatalf("scoped repos = %+v, want web only", got)
	}
}

func TestBuildInitialPromptScopesRepo(t *testing.T) {
	api := domain.Repo{ID: 10, Name: "api", DefaultBranch: "main"}
	web := domain.Repo{ID: 11, Name: "web", DefaultBranch: "main"}
	proj := &domain.Project{Name: "p", Repos: []domain.Repo{api, web}}
	repoID := int64(11)
	prompt := buildInitialPrompt(proj, &domain.Issue{Key: "LOCAL-1", Title: "fix web", RepoID: &repoID}, "")
	if !strings.Contains(prompt, "./web/") || strings.Contains(prompt, "./api/") {
		t.Fatalf("prompt did not scope to web: %s", prompt)
	}
	if !strings.Contains(prompt, "scoped to `./web/`") {
		t.Fatalf("prompt missing scope note: %s", prompt)
	}
}
