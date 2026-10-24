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
	"github.com/xpier/xissue/internal/provider"
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

func TestPRCreatedPluginReceivesPayload(t *testing.T) {
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
	repo := domain.Repo{ID: 9, Name: "web", Provider: domain.ProviderGitHub, RemoteURL: filepath.Join(tmp, "repo")}
	proj.Repos = []domain.Repo{repo}

	marker := filepath.Join(tmp, "pr-events.log")
	pluginDir := filepath.Join(tmp, "plugins", "pr-recorder")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: 1\nname: pr-recorder\nversion: 1.0.0\nentrypoint: run.sh\nevents: [pr_created]\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "run.sh"), []byte("#!/bin/sh\ncat > "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := &Runner{
		cfg:        &config.Config{DataDir: tmp},
		st:         st,
		notify:     nopNotifier{},
		automation: automation.NewEngine(filepath.Join(tmp, "plugins")),
	}
	r.dispatchPRCreated(proj, &repo, issue, &provider.PRDetailInfo{
		Number: 12, Title: "PR title", State: "open",
		URL: "https://github.com/example/web/pull/12", HeadRefName: "agent/x", BaseRefName: "main",
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(marker); err == nil {
			if strings.Contains(string(data), `"event":"pr_created"`) && strings.Contains(string(data), `"number":12`) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, _ := os.ReadFile(marker)
	t.Fatalf("pr_created payload missing: %q", string(data))
}

func TestLifecycleActionAllowed(t *testing.T) {
	cases := []struct {
		event  string
		action string
		want   bool
	}{
		{automation.EventIssueCreated, "notify", true},
		{automation.EventIssueCreated, "add_labels", true},
		{automation.EventIssueCreated, "create_issue", false},
		{automation.EventRunFinished, "create_issue", true},
		{automation.EventRunFinished, "trigger_run", false},
		{automation.EventPRCreated, "trigger_run", true},
		{automation.EventPRCreated, "create_issue", false},
		{automation.EventNotification, "notify", true},
		{automation.EventNotification, "trigger_run", false},
	}
	for _, c := range cases {
		if got := lifecycleActionAllowed(c.event, c.action); got != c.want {
			t.Errorf("lifecycleActionAllowed(%s, %s) = %v, want %v", c.event, c.action, got, c.want)
		}
	}
}

func TestLifecycleAddLabelsAndCreateIssue(t *testing.T) {
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
	issue, err := st.CreateLocalIssue(t.Context(), proj.ID, "run failed", "boom", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(t.Context(), issue.ID, "opencode", "")
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{cfg: &config.Config{DataDir: tmp}, st: st, notify: nopNotifier{}}
	r.executeLifecycleActions(t.Context(), automation.PREventContext{
		Event:   automation.EventRunFinished,
		Project: *proj,
		Issue:   issue,
		Run:     run,
	}, []domain.RuleAction{
		{Type: "add_labels", Labels: []string{"needs-fix", "flaky"}},
		{Type: "create_issue", Prompt: "跟进失败", Labels: []string{"follow-up"}},
	})
	updated, err := st.GetIssue(t.Context(), issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Labels) != 2 {
		t.Fatalf("labels = %v", updated.Labels)
	}
	issues, err := st.ListIssues(t.Context(), proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %+v", issues)
	}
	var followUp *domain.Issue
	for i := range issues {
		if issues[i].ID != issue.ID {
			followUp = &issues[i]
		}
	}
	if followUp == nil || followUp.Title != "跟进失败" || !strings.Contains(followUp.Body, issue.Key) {
		t.Fatalf("follow-up = %+v", followUp)
	}
}

func TestLifecycleTriggerRunRejectedOutsidePRCreated(t *testing.T) {
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
	issue, err := st.CreateLocalIssue(t.Context(), proj.ID, "title", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{cfg: &config.Config{DataDir: tmp}, st: st, notify: nopNotifier{}}
	r.executeLifecycleActions(t.Context(), automation.PREventContext{
		Event:   automation.EventIssueCreated,
		Project: *proj,
		Issue:   issue,
	}, []domain.RuleAction{{Type: "trigger_run", Agent: "opencode"}})
	runs, err := st.ListRuns(t.Context(), issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("trigger_run ran outside pr_created: %+v", runs)
	}
}

func TestRunFinishedPluginCanMutateIssue(t *testing.T) {
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
	pluginDir := filepath.Join(tmp, "plugins", "mutator")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: 1\nname: mutator\nversion: 1.0.0\nentrypoint: run.sh\nevents: [run_finished]\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
cat >/dev/null
printf '%s' '{"actions":[{"type":"add_labels","labels":["plugin-label"]},{"type":"create_issue","prompt":"Plugin follow-up"}]}'
`
	if err := os.WriteFile(filepath.Join(pluginDir, "run.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Runner{
		cfg:        &config.Config{DataDir: tmp},
		st:         st,
		notify:     nopNotifier{},
		automation: automation.NewEngine(filepath.Join(tmp, "plugins")),
	}
	r.dispatchRunEvent(run.ID, automation.EventRunFinished)
	updated, err := st.GetIssue(t.Context(), issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Labels) != 1 || updated.Labels[0] != "plugin-label" {
		t.Fatalf("labels = %v", updated.Labels)
	}
	issues, err := st.ListIssues(t.Context(), proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %+v", issues)
	}
}
