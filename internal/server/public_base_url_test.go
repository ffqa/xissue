package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestObservedBaseURLFromForwardedHeaders(t *testing.T) {
	srv, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7789/api/health", nil)
	req.Host = "127.0.0.1:7789"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "issue.example.com")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("health status = %d", w.Code)
	}
	if got := srv.cfg.PublicBaseURL(); got != "https://issue.example.com" {
		t.Fatalf("notification base = %q, want https://issue.example.com", got)
	}

	// A later direct loopback request must not downgrade the learned URL.
	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7789/api/health", nil)
	req.Host = "127.0.0.1:7789"
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if got := srv.cfg.PublicBaseURL(); got != "https://issue.example.com" {
		t.Fatalf("loopback request downgraded base URL to %q", got)
	}
}
