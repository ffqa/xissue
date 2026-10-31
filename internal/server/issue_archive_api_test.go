package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/store"
)

func TestIssueArchiveListAPI(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ctx := context.Background()
	proj, err := st.CreateProject(ctx, "Archive API", "")
	if err != nil {
		t.Fatal(err)
	}
	active, err := st.CreateLocalIssue(ctx, proj.ID, "Active issue", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	old, err := st.CreateLocalIssue(ctx, proj.ID, "Old done issue", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := domain.IssueDone
	if _, err := st.UpdateIssue(ctx, old.ID, store.IssuePatch{Status: &done}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ArchiveDoneIssuesBefore(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	do := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "127.0.0.1:7789"
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w
	}
	w := do("/api/projects/" + itoa(proj.ID) + "/issues")
	if w.Code != http.StatusOK {
		t.Fatalf("active list: %d %s", w.Code, w.Body.String())
	}
	var list []domain.Issue
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != active.ID {
		t.Fatalf("active list = %+v", list)
	}

	w = do("/api/projects/" + itoa(proj.ID) + "/issues/archived?q=Old&limit=1")
	if w.Code != http.StatusOK {
		t.Fatalf("archive list: %d %s", w.Code, w.Body.String())
	}
	var page struct {
		Issues []domain.Issue `json:"issues"`
		Total  int            `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Issues) != 1 || page.Issues[0].ID != old.ID || page.Issues[0].ArchivedAt == nil {
		t.Fatalf("archive page = %+v", page)
	}
}
