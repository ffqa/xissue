package runner

import (
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestWorkspaceAGENTSDoesNotTreatDescriptionAsPath(t *testing.T) {
	repoID := int64(1)
	p := &domain.Project{
		Name:        "ide",
		Description: "/Users/example/project",
		Repos:       []domain.Repo{{ID: repoID, Name: "repo"}},
	}
	issue := &domain.Issue{Key: "acme/repo#1", Title: "Feature", RepoID: &repoID}
	got := buildWorkspaceAGENTS(p, issue)
	if strings.Contains(got, "/Users/example/project") {
		t.Fatalf("workspace prompt leaked absolute description path: %s", got)
	}
	if !strings.Contains(got, "not a filesystem path") {
		t.Fatalf("workspace prompt missing path warning: %s", got)
	}
	if !strings.Contains(got, "`./repo/`") {
		t.Fatalf("workspace prompt missing relative repo path: %s", got)
	}
}

func TestWorkspaceAGENTSWorkflowSections(t *testing.T) {
	p := &domain.Project{Name: "wf", Repos: []domain.Repo{{ID: 1, Name: "repo"}}}
	issue := &domain.Issue{Key: "LOCAL-1", Title: "Feature"}

	plain := buildWorkspaceAGENTS(p, issue)
	if strings.Contains(plain, "Engineering Workflow") || strings.Contains(plain, "ub-mode") {
		t.Fatalf("workflow sections should be off by default:\n%s", plain)
	}

	p.Workflow = domain.ProjectWorkflow{DDD: true, TDD: true, UBMode: true, STE: true, TestLoop: true, MaxIterations: 3}
	got := buildWorkspaceAGENTS(p, issue)
	for _, want := range []string{
		"Engineering Workflow (project setting)",
		"Domain-Driven Design",
		"ubiquitous language",
		"### TDD",
		"ub-mode (unclebob/ub-mode)",
		"CRAP",
		"STE summary",
		"Simplified Technical English",
		"Test loop",
		"at most 3 retries",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("AGENTS.md missing %q:\n%s", want, got)
		}
	}
}
