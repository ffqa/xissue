package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestPublishIssueRejectsRepoFromAnotherProject(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ctx := context.Background()
	projA, err := st.CreateProject(ctx, "A", "")
	if err != nil {
		t.Fatal(err)
	}
	projB, err := st.CreateProject(ctx, "B", "")
	if err != nil {
		t.Fatal(err)
	}
	repoB, err := st.CreateRepo(ctx, domain.Repo{ProjectID: projB.ID, Name: "repo-b", Provider: domain.ProviderGitHub, RemoteURL: "https://github.com/acme/repo", DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	issueA, err := st.CreateLocalIssue(ctx, projA.ID, "Issue A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"repoId":` + itoa(repoB.ID) + `}`
	req := httptest.NewRequest("POST", "/api/issues/"+itoa(issueA.ID)+"/publish", strings.NewReader(body))
	req.Host = "127.0.0.1:7789"
	req.Header.Set("X-XIssue", "1")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cross-project publish = %d %s, want 400", w.Code, w.Body.String())
	}
	if got, _ := st.GetIssue(ctx, issueA.ID); got.RemoteNumber != nil {
		t.Fatalf("issue was published to a foreign repo: %#v", got)
	}
}
