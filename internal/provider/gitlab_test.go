package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestGitLabIssueAndPRFlow(t *testing.T) {
	var patchedLabels string
	var mergeBody map[string]any
	var closeBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Errorf("missing PRIVATE-TOKEN for %s", r.URL.Path)
		}
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && path == "/api/v4/projects/acme/demo/issues":
			writeTestJSON(t, w, []map[string]any{
				{"iid": 1, "title": "bug", "description": "broken", "web_url": "https://gitlab.test/acme/demo/-/issues/1", "author": map[string]any{"username": "alice"}, "labels": []string{"bug"}},
				{"iid": 2, "title": "other", "web_url": "https://gitlab.test/acme/demo/-/issues/2", "author": map[string]any{"username": "bob"}, "labels": []string{}},
			})
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/issues/1/notes"):
			writeTestJSON(t, w, []map[string]any{
				{"id": 11, "body": "same bug", "created_at": "2026-01-02T03:04:05Z", "author": map[string]any{"username": "alice"}},
				{"id": 12, "body": "changed label", "system": true, "created_at": "2026-01-02T03:05:05Z", "author": map[string]any{"username": "alice"}},
			})
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/issues/2/notes"):
			writeTestJSON(t, w, []map[string]any{})
		case r.Method == http.MethodPost && path == "/api/v4/projects/acme/demo/issues":
			writeTestJSON(t, w, map[string]any{"iid": 3, "title": "new", "description": "body", "web_url": "https://gitlab.test/acme/demo/-/issues/3", "author": map[string]any{"username": "alice"}})
		case r.Method == http.MethodPut && path == "/api/v4/projects/acme/demo/issues/1":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			patchedLabels, _ = body["add_labels"].(string)
			writeTestJSON(t, w, map[string]any{})
		case r.Method == http.MethodPost && path == "/api/v4/projects/acme/demo/issues/1/notes":
			writeTestJSON(t, w, map[string]any{"id": 13})
		case r.Method == http.MethodPost && path == "/api/v4/projects/acme/demo/merge_requests":
			writeTestJSON(t, w, map[string]any{"iid": 7, "web_url": "https://gitlab.test/acme/demo/-/merge_requests/7"})
		case r.Method == http.MethodGet && path == "/api/v4/projects/acme/demo/merge_requests/8/pipelines":
			writeTestJSON(t, w, []map[string]any{{"status": "success"}})
		case r.Method == http.MethodGet && path == "/api/v4/projects/acme/demo/merge_requests":
			writeTestJSON(t, w, []map[string]any{
				{"iid": 7, "title": "fix", "description": "body", "state": "merged", "merged_at": "2026-01-02T03:04:05Z", "web_url": "https://gitlab.test/acme/demo/-/merge_requests/7", "source_branch": "feat", "target_branch": "main", "author": map[string]any{"username": "alice"}, "labels": []string{"bug"}},
				{"iid": 8, "title": "wip", "state": "opened", "web_url": "https://gitlab.test/acme/demo/-/merge_requests/8", "source_branch": "wip", "target_branch": "main", "author": map[string]any{"username": "bob"}, "labels": []string{}},
			})
		case r.Method == http.MethodPut && path == "/api/v4/projects/acme/demo/merge_requests/7/merge":
			_ = json.NewDecoder(r.Body).Decode(&mergeBody)
			writeTestJSON(t, w, map[string]any{})
		case r.Method == http.MethodPut && path == "/api/v4/projects/acme/demo/merge_requests/7":
			_ = json.NewDecoder(r.Body).Decode(&closeBody)
			writeTestJSON(t, w, map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("XISSUE_GITLAB_URL", srv.URL)
	t.Setenv("XISSUE_GITLAB_TOKEN", "test-token")
	t.Setenv("GITLAB_URL", "")
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("GITLAB_PRIVATE_TOKEN", "")

	ctx := context.Background()
	repo := domain.Repo{ProjectID: 1, Name: "demo", Provider: domain.ProviderGitLab, OwnerRepo: "acme/demo"}
	p := For(repo.Provider)
	if !p.Caps().Issues || !p.Caps().PRs {
		t.Fatalf("gitlab caps = %+v", p.Caps())
	}

	issues, err := p.ListOpenIssues(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 || issues[0].Number != 1 || issues[0].Author != "alice" || issues[0].Labels[0] != "bug" {
		t.Fatalf("issues = %+v", issues)
	}

	comments, err := p.ListRepoIssueComments(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].IssueNumber != 1 || comments[0].ID != "11" || comments[0].Body != "same bug" {
		t.Fatalf("comments = %+v", comments)
	}

	created, err := p.CreateIssue(ctx, repo, "new", "body")
	if err != nil {
		t.Fatal(err)
	}
	if created.Number != 3 || created.URL != "https://gitlab.test/acme/demo/-/issues/3" {
		t.Fatalf("created = %+v", created)
	}

	if err := p.AddLabels(ctx, repo, 1, []string{"bug", "feature"}); err != nil {
		t.Fatal(err)
	}
	if patchedLabels != "bug,feature" {
		t.Fatalf("patched labels = %q", patchedLabels)
	}

	if err := p.Comment(ctx, repo, 1, "hello"); err != nil {
		t.Fatal(err)
	}

	prURL, err := p.CreatePR(ctx, repo, "feat", "main", "fix", "body")
	if err != nil || prURL != "https://gitlab.test/acme/demo/-/merge_requests/7" {
		t.Fatalf("CreatePR = %q, %v", prURL, err)
	}

	prs, err := p.ListPRs(ctx, repo, "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 || prs[0].State != "merged" || prs[0].HeadBranch != "feat" || prs[0].BaseBranch != "main" {
		t.Fatalf("prs = %+v", prs)
	}
	if prs[1].State != "open" || prs[1].CIState != "success" || prs[1].CIPassed != 1 {
		t.Fatalf("open pr = %+v", prs[1])
	}

	if msg, err := p.MergePR(ctx, repo, 7, "squash"); err != nil || msg != "merged" {
		t.Fatalf("MergePR = %q, %v", msg, err)
	}
	if mergeBody["squash"] != true {
		t.Fatalf("merge body = %#v", mergeBody)
	}

	if err := p.ClosePR(ctx, repo, 7); err != nil {
		t.Fatal(err)
	}
	if closeBody["state_event"] != "close" {
		t.Fatalf("close body = %#v", closeBody)
	}
}

func TestGitLabPRURLStatusAndDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && path == "/api/v4/projects/acme/demo/merge_requests/7":
			writeTestJSON(t, w, map[string]any{
				"iid": 7, "title": "fix", "state": "opened",
				"web_url": "https://gitlab.test/acme/demo/-/merge_requests/7", "author": map[string]any{"username": "alice"},
				"source_branch": "feat", "target_branch": "main", "diff_refs": map[string]any{"head_sha": "abc123"},
			})
		case r.Method == http.MethodGet && path == "/api/v4/projects/acme/demo/merge_requests/7/notes":
			writeTestJSON(t, w, []map[string]any{
				{"id": 21, "body": "LGTM", "created_at": "2026-01-02T03:04:05Z", "author": map[string]any{"username": "bob"}},
				{"id": 22, "body": "approved", "system": true, "created_at": "2026-01-02T03:04:06Z", "author": map[string]any{"username": "bob"}},
			})
		case r.Method == http.MethodGet && path == "/api/v4/projects/acme/demo/merge_requests/7/approvals":
			writeTestJSON(t, w, map[string]any{"approved_by": []map[string]any{{"user": map[string]any{"username": "bob"}}}})
		case r.Method == http.MethodGet && path == "/api/v4/projects/acme/demo/merge_requests/7/pipelines":
			writeTestJSON(t, w, []map[string]any{{"status": "success"}, {"status": "failed"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("XISSUE_GITLAB_URL", srv.URL)
	t.Setenv("XISSUE_GITLAB_TOKEN", "test-token")
	t.Setenv("GITLAB_URL", "")
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("GITLAB_PRIVATE_TOKEN", "")

	prURL := srv.URL + "/acme/demo/-/merge_requests/7"
	state, title, err := CheckPRStatus(context.Background(), prURL)
	if err != nil || state != "open" || title != "fix" {
		t.Fatalf("CheckPRStatus = %q, %q, %v", state, title, err)
	}
	detail, err := GetPRDetails(context.Background(), prURL)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Number != 7 || detail.HeadSHA != "abc123" || detail.HeadRefName != "feat" || detail.BaseRefName != "main" {
		t.Fatalf("detail = %+v", detail)
	}
	if len(detail.Comments) != 1 || detail.Comments[0].Body != "LGTM" || detail.Comments[0].Author != "bob" {
		t.Fatalf("comments = %+v", detail.Comments)
	}
	if len(detail.LatestReviews) != 1 || detail.LatestReviews[0].State != "approved" {
		t.Fatalf("reviews = %+v", detail.LatestReviews)
	}
	if detail.CIState != "failure" || detail.CIPassed != 1 || detail.CIFailed != 1 {
		t.Fatalf("ci = %+v", detail)
	}
}
