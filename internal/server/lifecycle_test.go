package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLifecyclePluginsReceiveIssueEvents(t *testing.T) {
	srv, st, tmpDir := newTestServer(t)
	proj, err := st.CreateProject(t.Context(), "Plugins", "")
	if err != nil {
		t.Fatal(err)
	}
	projID := fmt.Sprintf("%d", proj.ID)

	marker := filepath.Join(tmpDir, "events.log")
	pluginDir := filepath.Join(tmpDir, "plugins", "lifecycle-recorder")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: 1
name: lifecycle-recorder
version: 1.0.0
description: records lifecycle events
entrypoint: run.py
events:
  - issue_created
  - issue_status_changed
permissions:
  - process
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`import json, sys
p = json.load(sys.stdin)
with open(%q, "a") as f:
    f.write("%%s|%%s|%%s\n" %% (p["event"], p.get("previousStatus", ""), p["issue"]["status"]))
`, marker)
	if err := os.WriteFile(filepath.Join(pluginDir, "run.py"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	post := httptest.NewRequest("POST", "/api/projects/"+projID+"/issues", strings.NewReader(`{"title":"plugin smoke"}`))
	post.Host = "127.0.0.1:7789"
	post.Header.Set("X-XIssue", "1")
	post.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, post)
	if w.Code != http.StatusCreated {
		t.Fatalf("create issue: %d %s", w.Code, w.Body.String())
	}
	var issue struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &issue); err != nil {
		t.Fatal(err)
	}

	patch := httptest.NewRequest("PATCH", fmt.Sprintf("/api/issues/%d", issue.ID), strings.NewReader(`{"status":"done"}`))
	patch.Host = "127.0.0.1:7789"
	patch.Header.Set("X-XIssue", "1")
	patch.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, patch)
	if w.Code != http.StatusOK {
		t.Fatalf("update issue: %d %s", w.Code, w.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	var lines []string
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(marker)
		if err == nil {
			lines = strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) >= 2 {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "issue_created||todo") {
		t.Fatalf("missing issue_created event: %q", joined)
	}
	if !strings.Contains(joined, "issue_status_changed|todo|done") {
		t.Fatalf("missing issue_status_changed event: %q", joined)
	}
}
