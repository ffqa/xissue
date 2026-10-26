package runner

import (
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestRequirePRSupport(t *testing.T) {
	proj := &domain.Project{Repos: []domain.Repo{{Name: "gitlab", Provider: domain.ProviderGitLab}}}
	if err := requirePRSupport(proj, &domain.Issue{}); err == nil {
		t.Fatal("expected gitlab PR support to be rejected")
	}
	proj.Repos[0].Provider = domain.ProviderGitHub
	if err := requirePRSupport(proj, &domain.Issue{}); err != nil {
		t.Fatalf("github PR support rejected: %v", err)
	}
}
