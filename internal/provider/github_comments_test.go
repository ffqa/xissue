package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestListRepoIssueCommentsParsesGHOutput(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s' '[[{"id":1,"node_id":"IC_1","user":{"login":"alice"},"body":"first reply","html_url":"https://example.test/1","issue_url":"https://api.github.com/repos/acme/repo/issues/7","created_at":"2026-10-23T10:00:00Z"}]]'
`
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	comments, err := github{}.ListRepoIssueComments(context.Background(), domain.Repo{OwnerRepo: "acme/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].ID != "IC_1" || comments[0].IssueNumber != 7 || comments[0].Author != "alice" || comments[0].Body != "first reply" {
		t.Fatalf("comments = %+v", comments)
	}
	if comments[0].CreatedAt.IsZero() {
		t.Fatalf("createdAt not parsed: %+v", comments[0])
	}
}
