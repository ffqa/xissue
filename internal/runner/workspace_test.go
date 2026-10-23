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
