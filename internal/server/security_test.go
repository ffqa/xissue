package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/store"
)

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func newTestServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	tmpDir := t.TempDir()
	st, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{DataDir: tmpDir, Listen: "127.0.0.1:7789"}
	srv := New(cfg, st)
	t.Cleanup(srv.Shutdown)
	return srv, st, tmpDir
}

func TestMonitorConfigSecretsMaskedAndPatchPreserves(t *testing.T) {
	srv, _, _ := newTestServer(t)
	if err := srv.cfg.UpdateMonitor(config.MonitorConfig{
		Enabled:          true,
		TelegramBotToken: "BOT-SECRET-123",
		TelegramChatID:   "42",
		WebhookURL:       "https://hooks.example/secret",
	}); err != nil {
		t.Fatalf("seed monitor config: %v", err)
	}

	do := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/monitor/config", strings.NewReader(body))
		req.Host = "127.0.0.1:7789"
		req.Header.Set("X-XIssue", "1")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w
	}

	w := do("GET", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET monitor config: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "BOT-SECRET-123") || strings.Contains(w.Body.String(), "hooks.example/secret") {
		t.Fatalf("GET leaked secrets: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), config.MaskedSecret) {
		t.Fatalf("GET did not mask secrets: %s", w.Body.String())
	}

	w = do("PATCH", `{"enabled":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH monitor config: %d %s", w.Code, w.Body.String())
	}
	mon := srv.cfg.MonitorSnapshot()
	if mon.Enabled || mon.TelegramBotToken != "BOT-SECRET-123" || mon.WebhookURL != "https://hooks.example/secret" {
		t.Fatalf("partial patch clobbered secrets: %+v", mon)
	}

	w = do("PUT", `{"enabled":true,"activeHours":"","publicUrl":"","telegramBotToken":"`+config.MaskedSecret+`","telegramChatId":"42","webhookUrl":"`+config.MaskedSecret+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT masked config: %d %s", w.Code, w.Body.String())
	}
	mon = srv.cfg.MonitorSnapshot()
	if mon.TelegramBotToken != "BOT-SECRET-123" || mon.WebhookURL != "https://hooks.example/secret" {
		t.Fatalf("masked PUT overwrote secrets: %+v", mon)
	}
}

func TestDeleteProjectRequiresNameAndScopesNotifications(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ctx := context.Background()
	p1, err := st.CreateProject(ctx, "Delete Me", "")
	if err != nil {
		t.Fatal(err)
	}
	p12, err := st.CreateProject(ctx, "Keep Me", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{"/projects/1", "/projects/12", "/projects/1?watch=3"} {
		if _, err := st.CreateNotification(ctx, "test", "n", "b", link, "{}"); err != nil {
			t.Fatal(err)
		}
	}

	del := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Host = "127.0.0.1:7789"
		req.Header.Set("X-XIssue", "1")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w
	}

	if w := del("DELETE", "/api/projects/"+itoa(p1.ID), ""); w.Code != http.StatusBadRequest {
		t.Fatalf("delete without name must be rejected, got %d %s", w.Code, w.Body.String())
	}
	if w := del("DELETE", "/api/projects/"+itoa(p1.ID), `{"name":"Delete Me"}`); w.Code != http.StatusOK {
		t.Fatalf("delete with name failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := st.GetProject(ctx, p1.ID); err == nil {
		t.Fatal("project should be deleted")
	}
	if _, err := st.GetProject(ctx, p12.ID); err != nil {
		t.Fatalf("other project should remain: %v", err)
	}
	notes, err := st.ListNotifications(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Link != "/projects/12" {
		t.Fatalf("notification scoping wrong: %+v", notes)
	}
}

func TestAuthMiddlewareBlocksCrossSiteRequests(t *testing.T) {
	srv, _, _ := newTestServer(t)

	tests := []struct {
		name   string
		method string
		path   string
		host   string
		origin string
		header bool
		want   int
	}{
		{name: "state change without header", method: "POST", path: "/api/notifications/read-all", host: "127.0.0.1:7789", want: http.StatusForbidden},
		{name: "state change with header", method: "POST", path: "/api/notifications/read-all", host: "127.0.0.1:7789", header: true, want: http.StatusNoContent},
		{name: "dns rebinding host", method: "GET", path: "/api/health", host: "evil.example", want: http.StatusForbidden},
		{name: "cross origin", method: "POST", path: "/api/notifications/read-all", host: "127.0.0.1:7789", origin: "http://evil.example", header: true, want: http.StatusForbidden},
		{name: "same origin allowed", method: "POST", path: "/api/notifications/read-all", host: "127.0.0.1:7789", origin: "http://127.0.0.1:7789", header: true, want: http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.header {
				req.Header.Set("X-XIssue", "1")
			}
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("%s %s: got %d, want %d (%s)", tc.method, tc.path, w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestDocsAPITraversalRejected(t *testing.T) {
	srv, st, tmpDir := newTestServer(t)
	ctx := context.Background()

	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(secret, []byte("do-not-touch"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj, err := st.CreateProject(ctx, "Traversal", "")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := st.CreateRepo(ctx, domain.Repo{
		ProjectID:     proj.ID,
		Name:          "local",
		Provider:      domain.ProviderPlain,
		RemoteURL:     repoDir,
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	detailPath := "/api/projects/" + itoa(proj.ID) + "/docs/detail?docId=local:" + itoa(repo.ID) + ":../secret.txt"
	req := httptest.NewRequest("GET", detailPath, nil)
	req.Host = "127.0.0.1:7789"
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("traversal detail must not succeed: %s", w.Body.String())
	}

	saveBody := `{"docId":"local:` + itoa(repo.ID) + `:../../tmp/xissue-server-pwned.md","content":"pwned"}`
	req = httptest.NewRequest("PUT", "/api/projects/"+itoa(proj.ID)+"/docs/save", strings.NewReader(saveBody))
	req.Host = "127.0.0.1:7789"
	req.Header.Set("X-XIssue", "1")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("traversal save must not succeed: %s", w.Body.String())
	}
	if _, err := os.Stat("/tmp/xissue-server-pwned.md"); err == nil {
		os.Remove("/tmp/xissue-server-pwned.md")
		t.Fatal("traversal save wrote outside the repository")
	}
	if got, _ := os.ReadFile(secret); string(got) != "do-not-touch" {
		t.Fatalf("secret file was modified: %q", got)
	}

	resp := srv.ProcessMCPRequest(ctx, mcpRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params: json.RawMessage(`{"name":"xissue_get_doc","arguments":{"projectId":` +
			itoa(proj.ID) + `,"docId":"local:` + itoa(repo.ID) + `:../secret.txt"}}`),
	})
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected MCP result: %#v", resp.Result)
	}
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("MCP xissue_get_doc should reject traversal: %#v", result)
	}
}
