package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
)

func TestSyncAllProjectIssuesWithFakeGH(t *testing.T) {
	srv, st, tmpDir := newTestServer(t)

	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gh := filepath.Join(binDir, "gh")
	script := `#!/bin/sh
printf '%s' '[{"number":42,"title":"App crashes on start","body":"error","labels":[],"url":"https://example.test/issues/42","author":{"login":"alice"}}]'
`
	if err := os.WriteFile(gh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	proj, err := st.CreateProject(t.Context(), "SyncProj", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRepo(t.Context(), domain.Repo{
		ProjectID:     proj.ID,
		Name:          "sync-repo",
		Provider:      domain.ProviderGitHub,
		OwnerRepo:     "acme/sync-repo",
		DefaultBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}

	res := srv.syncAllProjectIssues(context.Background())
	if len(res.Errors) != 0 {
		t.Fatalf("sync errors: %v", res.Errors)
	}
	if res.Projects != 1 || res.Imported != 1 {
		t.Fatalf("unexpected sync result: %+v", res)
	}
	issues, err := st.ListIssues(context.Background(), proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Key != "acme/sync-repo#42" || issues[0].Title != "App crashes on start" {
		t.Fatalf("unexpected imported issues: %+v", issues)
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0] != "bug" {
		t.Fatalf("expected triage bug label, got %+v", issues[0].Labels)
	}

	// A second pass must be idempotent.
	res = srv.syncAllProjectIssues(context.Background())
	if res.Imported != 0 {
		t.Fatalf("second sync imported %d duplicates", res.Imported)
	}
}

func TestGlobalAutoDevelopGateBypassesProjectOptIn(t *testing.T) {
	srv, st, tmpDir := newTestServer(t)
	if err := srv.cfg.UpdateMonitor(config.MonitorConfig{
		Enabled:           true,
		AutoDevelopIssues: true,
		SyncIntervalMin:   10,
	}); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf '%s' '[{"number":9,"title":"Crash on launch","body":"error","labels":[],"url":"https://example.test/issues/9","author":{"login":"carol"}}]'
`
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	proj, err := st.CreateProject(t.Context(), "GlobalAuto", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRepo(t.Context(), domain.Repo{
		ProjectID:     proj.ID,
		Name:          "global-auto",
		Provider:      domain.ProviderGitHub,
		OwnerRepo:     "acme/global-auto",
		DefaultBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}

	srv.syncAllProjectIssues(context.Background())
	issue, err := st.FindIssueByKey(context.Background(), proj.ID, "acme/global-auto#9")
	if err != nil {
		t.Fatal(err)
	}
	if len(issue.Labels) != 1 || issue.Labels[0] != "bug" {
		t.Fatalf("expected bug label, got %+v", issue.Labels)
	}
	comments, err := st.ListComments(context.Background(), issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range comments {
		if strings.Contains(c.Body, "未配置任何 Agent") {
			found = true
		}
	}
	if !found {
		t.Fatalf("global auto-develop gate was not reached; comments=%+v", comments)
	}
}

func TestEnablingGlobalSyncTriggersBackgroundImport(t *testing.T) {
	srv, st, tmpDir := newTestServer(t)

	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf '%s' '[{"number":7,"title":"auto sync","body":"","labels":[],"url":"https://example.test/issues/7","author":{"login":"bob"}}]'
`
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	proj, err := st.CreateProject(t.Context(), "AutoSyncProj", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRepo(t.Context(), domain.Repo{
		ProjectID:     proj.ID,
		Name:          "auto-sync-repo",
		Provider:      domain.ProviderGitHub,
		OwnerRepo:     "acme/auto-sync-repo",
		DefaultBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/monitor/config",
		strings.NewReader(`{"enabled":true,"syncIssues":true,"syncIntervalMin":60}`))
	req.Host = "127.0.0.1:7789"
	req.Header.Set("X-XIssue", "1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch config: %d %s", w.Code, w.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		issues, err := st.ListIssues(context.Background(), proj.ID)
		if err == nil && len(issues) == 1 && issues[0].Key == "acme/auto-sync-repo#7" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	issues, _ := st.ListIssues(context.Background(), proj.ID)
	b, _ := json.Marshal(issues)
	t.Fatalf("background sync did not import issue: %s", b)
}
