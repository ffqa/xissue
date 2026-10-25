package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestCreateIssueWithRepo(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ctx := t.Context()
	proj, err := st.CreateProject(ctx, "multi", "")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := st.CreateRepo(ctx, domain.Repo{
		ProjectID: proj.ID, Name: "api", Provider: "github",
		RemoteURL: "https://github.com/example/api", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"title":"scoped","body":"x","repoId":` + strconv.FormatInt(repo.ID, 10) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+strconv.FormatInt(proj.ID, 10)+"/issues", strings.NewReader(body))
	req.Host = "127.0.0.1:7789"
	req.Header.Set("X-XIssue", "1")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create issue: %d %s", w.Code, w.Body.String())
	}
	var issue domain.Issue
	if err := json.Unmarshal(w.Body.Bytes(), &issue); err != nil {
		t.Fatal(err)
	}
	if issue.RepoID == nil || *issue.RepoID != repo.ID {
		t.Fatalf("repoId = %v, want %d", issue.RepoID, repo.ID)
	}

	other, err := st.CreateProject(ctx, "other", "")
	if err != nil {
		t.Fatal(err)
	}
	body = `{"title":"bad","repoId":` + strconv.FormatInt(repo.ID, 10) + `}`
	req = httptest.NewRequest(http.MethodPost, "/api/projects/"+strconv.FormatInt(other.ID, 10)+"/issues", strings.NewReader(body))
	req.Host = "127.0.0.1:7789"
	req.Header.Set("X-XIssue", "1")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cross-project repo should fail, got %d %s", w.Code, w.Body.String())
	}
}
