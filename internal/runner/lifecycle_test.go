package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/automation"
	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/store"
)

type nopNotifier struct{}

func (nopNotifier) BroadcastRunEvent(int64, int64, *domain.RunEvent) {}
func (nopNotifier) BroadcastRunUpdated(*domain.Run)                  {}
func (nopNotifier) BroadcastIssueUpdated(*domain.Issue)              {}
func (nopNotifier) BroadcastNotification(*domain.Notification)       {}

func TestRunLifecyclePluginReceivesRunFinished(t *testing.T) {
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
	issue, err := st.CreateLocalIssue(t.Context(), proj.ID, "title", "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(t.Context(), issue.ID, "opencode", "")
	if err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(tmp, "run-events.log")
	pluginDir := filepath.Join(tmp, "plugins", "run-recorder")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: 1\nname: run-recorder\nversion: 1.0.0\nentrypoint: run.sh\nevents: [run_finished]\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat > " + marker + "\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "run.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	r := &Runner{
		cfg:        &config.Config{DataDir: tmp},
		st:         st,
		notify:     nopNotifier{},
		automation: automation.NewEngine(filepath.Join(tmp, "plugins")),
	}
	r.dispatchRunEvent(run.ID, automation.EventRunStarted)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("plugin should not receive unsubscribed event: %v", err)
	}
	r.dispatchRunEvent(run.ID, automation.EventRunFinished)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(marker); err == nil {
			if strings.Contains(string(data), `"event":"run_finished"`) && strings.Contains(string(data), `"status":"queued"`) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, _ := os.ReadFile(marker)
	t.Fatalf("run_finished payload missing: %q", string(data))
}
