package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentRolesSettingsRoundTrip(t *testing.T) {
	srv, _, _ := newTestServer(t)
	request := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/settings", strings.NewReader(body))
		req.Host = "127.0.0.1:7789"
		req.Header.Set("X-XIssue", "1")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w
	}

	w := request("PATCH", `{"agentRoles":{"issueReview":"reviewer","issueDevelop":"developer","prReview":"pr-reviewer","autoMerge":"merger"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH settings: %d %s", w.Code, w.Body.String())
	}
	roles := srv.cfg.AgentRolesSnapshot()
	if roles.IssueReview != "reviewer" || roles.IssueDevelop != "developer" || roles.PRReview != "pr-reviewer" || roles.AutoMerge != "merger" {
		t.Fatalf("roles = %+v", roles)
	}

	w = request("GET", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET settings: %d %s", w.Code, w.Body.String())
	}
	for _, want := range []string{"issueReview", "developer", "prReview", "pr-reviewer", "autoMerge", "merger"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("GET settings missing %q: %s", want, w.Body.String())
		}
	}
}
