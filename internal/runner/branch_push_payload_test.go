package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/automation"
	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/store"
)

// A branch_push plugin must receive the pre-poll SHA as previousSha and the
// per-commit email/parents fields, so it can compute the push range without
// opening the mirror.
func TestBranchPushAutomationPayloadIncludesPreviousSHA(t *testing.T) {
	tmp := t.TempDir()
	st, err := store.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proj, err := st.CreateProject(t.Context(), "P", "")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := st.CreateRepo(t.Context(), domain.Repo{
		ProjectID: proj.ID,
		Name:      "repo",
		Provider:  domain.ProviderPlain,
		RemoteURL: filepath.Join(tmp, "repo"),
	})
	if err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(tmp, "branch-push.json")
	pluginDir := filepath.Join(tmp, "plugins", "branch-recorder")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: 1\nname: branch-recorder\nversion: 1.0.0\nentrypoint: run.sh\nevents: [branch_push]\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "run.sh"), []byte("#!/bin/sh\ncat > "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A second plugin fails so the watcher must log the aggregated warning.
	failDir := filepath.Join(tmp, "plugins", "failer")
	if err := os.MkdirAll(failDir, 0o755); err != nil {
		t.Fatal(err)
	}
	failManifest := "apiVersion: 1\nname: failer\nversion: 1.0.0\nentrypoint: run.sh\nevents: [branch_push]\n"
	if err := os.WriteFile(filepath.Join(failDir, "plugin.yaml"), []byte(failManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(failDir, "run.sh"), []byte("#!/bin/sh\necho 'plugin boom' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := &Runner{
		cfg:        &config.Config{DataDir: tmp},
		st:         st,
		notify:     nopNotifier{},
		automation: automation.NewEngine(filepath.Join(tmp, "plugins")),
	}
	w := domain.BranchWatch{ProjectID: proj.ID, RepoID: repo.ID, RepoName: "repo", Branch: "main", LastSHA: "oldsha"}
	tip := gitx.CommitInfo{SHA: "newsha", Subject: "tip", Author: "Alice", AuthorEmail: "alice@example.com", Parents: []string{"oldsha"}}
	commits := []gitx.CommitInfo{tip}
	if err := r.processBranchPushAutomation(context.Background(), w, repo, commits, tip); err != nil {
		t.Fatalf("processBranchPushAutomation: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var raw []byte
	for time.Now().Before(deadline) {
		if raw, err = os.ReadFile(marker); err == nil && len(raw) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(raw) == 0 {
		t.Fatal("branch_push plugin did not receive a payload")
	}
	var payload automation.PREventContext
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("payload is not valid JSON: %v (%s)", err, raw)
	}
	if payload.PreviousSHA != "oldsha" || payload.TipSHA != "newsha" || payload.Event != "branch_push" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if len(payload.Commits) != 1 || payload.Commits[0].AuthorEmail != "alice@example.com" || len(payload.Commits[0].Parents) != 1 {
		t.Fatalf("commit metadata missing: %+v", payload.Commits)
	}
	if payload.Repo == nil || payload.Repo.Name != "repo" {
		t.Fatalf("repo missing: %+v", payload.Repo)
	}

	r.monitorMu.Lock()
	logs := append([]MonitorLogEntry(nil), r.monitorLogs...)
	r.monitorMu.Unlock()
	found := false
	for _, e := range logs {
		if e.Level == "WARN" && strings.Contains(e.Message, "插件执行失败") && strings.Contains(e.Message, "failer") {
			found = true
		}
	}
	if !found {
		t.Fatalf("plugin failure was not logged: %+v", logs)
	}
}
