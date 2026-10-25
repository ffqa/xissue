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

func TestGiteaIssueAndPRFlow(t *testing.T) {
	var patchedLabels []any
	var mergeBody map[string]any
	var closeBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token test-token" {
			t.Errorf("missing auth header for %s", r.URL.Path)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/demo/issues":
			writeTestJSON(t, w, []map[string]any{
				{"number": 1, "title": "bug", "body": "broken", "html_url": "https://gitea.test/acme/demo/issues/1", "user": map[string]any{"login": "alice"}, "labels": []map[string]any{{"id": 1, "name": "bug"}}, "pull_request": nil},
				{"number": 2, "title": "pr", "html_url": "https://gitea.test/acme/demo/pulls/2", "user": map[string]any{"login": "bob"}, "labels": []any{}, "pull_request": map[string]any{}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/demo/issues/comments":
			writeTestJSON(t, w, []map[string]any{{
				"id": 11, "body": "same bug", "html_url": "https://gitea.test/acme/demo/issues/1#issuecomment-11",
				"issue_url": srvURL(r) + "/api/v1/repos/acme/demo/issues/1", "created_at": "2026-01-02T03:04:05Z",
				"user": map[string]any{"login": "alice"},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/acme/demo/issues":
			writeTestJSON(t, w, map[string]any{"number": 3, "title": "new", "body": "body", "html_url": "https://gitea.test/acme/demo/issues/3"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/demo/labels":
			writeTestJSON(t, w, []map[string]any{{"id": 1, "name": "bug"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/acme/demo/labels":
			writeTestJSON(t, w, map[string]any{"id": 2, "name": "feature"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/demo/issues/1":
			writeTestJSON(t, w, map[string]any{"number": 1, "labels": []map[string]any{{"id": 1, "name": "bug"}}})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/acme/demo/issues/1":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			patchedLabels, _ = body["labels"].([]any)
			writeTestJSON(t, w, map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/acme/demo/issues/1/comments":
			writeTestJSON(t, w, map[string]any{"id": 12})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/acme/demo/pulls":
			writeTestJSON(t, w, map[string]any{"number": 7, "html_url": "https://gitea.test/acme/demo/pulls/7"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/demo/commits/abc123/status":
			writeTestJSON(t, w, map[string]any{"state": "success", "statuses": []map[string]any{{"status": "success"}, {"status": "success"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/acme/demo/pulls":
			writeTestJSON(t, w, []map[string]any{
				{
					"number": 7, "title": "fix", "body": "body", "state": "closed", "merged": true, "merged_at": "2026-01-02T03:04:05Z",
					"html_url": "https://gitea.test/acme/demo/pulls/7", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
					"user": map[string]any{"login": "alice"}, "labels": []map[string]any{{"id": 1, "name": "bug"}},
					"head": map[string]any{"ref": "feat", "sha": "abc123"}, "base": map[string]any{"ref": "main"},
				},
				{
					"number": 8, "title": "wip", "state": "open",
					"html_url": "https://gitea.test/acme/demo/pulls/8", "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
					"user": map[string]any{"login": "bob"}, "labels": []any{},
					"head": map[string]any{"ref": "wip", "sha": "abc123"}, "base": map[string]any{"ref": "main"},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/acme/demo/pulls/7/merge":
			_ = json.NewDecoder(r.Body).Decode(&mergeBody)
			writeTestJSON(t, w, map[string]any{})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/repos/acme/demo/pulls/7":
			_ = json.NewDecoder(r.Body).Decode(&closeBody)
			writeTestJSON(t, w, map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("XISSUE_GITEA_URL", srv.URL)
	t.Setenv("XISSUE_GITEA_TOKEN", "test-token")
	t.Setenv("GITEA_URL", "")
	t.Setenv("GITEA_TOKEN", "")

	ctx := context.Background()
	repo := domain.Repo{ProjectID: 1, Name: "demo", Provider: domain.ProviderGitea, OwnerRepo: "acme/demo"}
	p := For(repo.Provider)
	if !p.Caps().Issues || !p.Caps().PRs {
		t.Fatalf("gitea caps = %+v", p.Caps())
	}

	issues, err := p.ListOpenIssues(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Number != 1 || issues[0].Author != "alice" || issues[0].Labels[0] != "bug" {
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
	if created.Number != 3 || created.URL != "https://gitea.test/acme/demo/issues/3" {
		t.Fatalf("created = %+v", created)
	}

	if err := p.AddLabels(ctx, repo, 1, []string{"bug", "feature"}); err != nil {
		t.Fatal(err)
	}
	if len(patchedLabels) != 2 {
		t.Fatalf("patched labels = %#v", patchedLabels)
	}

	if err := p.Comment(ctx, repo, 1, "hello"); err != nil {
		t.Fatal(err)
	}

	prURL, err := p.CreatePR(ctx, repo, "feat", "main", "fix", "body")
	if err != nil || prURL != "https://gitea.test/acme/demo/pulls/7" {
		t.Fatalf("CreatePR = %q, %v", prURL, err)
	}

	prs, err := p.ListPRs(ctx, repo, "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 || prs[0].State != "merged" || prs[0].HeadBranch != "feat" || prs[0].BaseBranch != "main" {
		t.Fatalf("prs = %+v", prs)
	}
	if prs[1].CIState != "success" || prs[1].CIPassed != 2 {
		t.Fatalf("open PR CI = %+v", prs[1])
	}

	if msg, err := p.MergePR(ctx, repo, 7, "squash"); err != nil || msg != "merged" {
		t.Fatalf("MergePR = %q, %v", msg, err)
	}
	if mergeBody["Do"] != "squash" {
		t.Fatalf("merge body = %#v", mergeBody)
	}

	if err := p.ClosePR(ctx, repo, 7); err != nil {
		t.Fatal(err)
	}
	if closeBody["state"] != "closed" {
		t.Fatalf("close body = %#v", closeBody)
	}
}

func TestGiteaPRURLStatusAndDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/acme/demo/pulls/7":
			writeTestJSON(t, w, map[string]any{
				"number": 7, "title": "fix", "state": "closed", "merged": true,
				"html_url": "https://gitea.test/acme/demo/pulls/7", "user": map[string]any{"login": "alice"},
				"head": map[string]any{"ref": "feat", "sha": "abc123"}, "base": map[string]any{"ref": "main"},
			})
		case "/api/v1/repos/acme/demo/commits/abc123/status":
			writeTestJSON(t, w, map[string]any{"state": "failure", "statuses": []map[string]any{{"status": "failure"}, {"status": "success"}}})
		case "/api/v1/repos/acme/demo/issues/7/comments":
			writeTestJSON(t, w, []map[string]any{{
				"id": 21, "body": "LGTM", "created_at": "2026-01-02T03:04:05Z", "user": map[string]any{"login": "bob"},
			}})
		case "/api/v1/repos/acme/demo/pulls/7/reviews":
			writeTestJSON(t, w, []map[string]any{{
				"id": 31, "body": "ok", "state": "APPROVED", "submitted_at": "2026-01-02T03:04:05Z", "user": map[string]any{"login": "bob"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("XISSUE_GITEA_URL", srv.URL)
	t.Setenv("XISSUE_GITEA_TOKEN", "test-token")
	prURL := srv.URL + "/acme/demo/pulls/7"

	state, title, err := CheckPRStatus(context.Background(), prURL)
	if err != nil || state != "merged" || title != "fix" {
		t.Fatalf("CheckPRStatus = %q %q %v", state, title, err)
	}
	detail, err := GetPRDetails(context.Background(), prURL)
	if err != nil {
		t.Fatal(err)
	}
	if detail.State != "merged" || detail.Author != "alice" || detail.HeadRefName != "feat" || detail.BaseRefName != "main" || detail.HeadSHA != "abc123" {
		t.Fatalf("detail = %+v", detail)
	}
	if len(detail.Comments) != 1 || detail.Comments[0].Body != "LGTM" {
		t.Fatalf("comments = %+v", detail.Comments)
	}
	if detail.CIState != "failure" || detail.CIFailed != 1 || detail.CIPassed != 1 {
		t.Fatalf("ci = %s passed=%d failed=%d", detail.CIState, detail.CIPassed, detail.CIFailed)
	}
	if len(detail.LatestReviews) != 1 || detail.LatestReviews[0].State != "approved" {
		t.Fatalf("reviews = %+v", detail.LatestReviews)
	}
}

func TestGiteaRequiresEnvironment(t *testing.T) {
	t.Setenv("XISSUE_GITEA_URL", "")
	t.Setenv("GITEA_URL", "")
	if _, err := (gitea{}).ListOpenIssues(context.Background(), domain.Repo{Name: "demo", OwnerRepo: "acme/demo"}); err == nil || !strings.Contains(err.Error(), "XISSUE_GITEA_URL") {
		t.Fatalf("missing config error = %v", err)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode: %v", err)
	}
}

func srvURL(r *http.Request) string {
	return "http://" + r.Host
}

func TestUnconfiguredGiteaFallsBackToPlainCapabilities(t *testing.T) {
	t.Setenv("XISSUE_GITEA_URL", "")
	t.Setenv("GITEA_URL", "")
	t.Setenv("XISSUE_GITEA_TOKEN", "")
	t.Setenv("GITEA_TOKEN", "")
	t.Setenv("GITEA_ACCESS_TOKEN", "")
	if caps := (gitea{}).Caps(); caps.Issues || caps.PRs {
		t.Fatalf("unconfigured gitea caps = %+v, want none", caps)
	}
	t.Setenv("XISSUE_GITEA_URL", "https://gitea.example.com")
	if caps := (gitea{}).Caps(); !caps.Issues || !caps.PRs {
		t.Fatalf("configured gitea caps = %+v, want issues+PRs", caps)
	}
}
