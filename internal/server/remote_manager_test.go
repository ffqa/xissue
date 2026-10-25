package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/remoterunner"
	"github.com/xpier/xissue/internal/runner"
	"github.com/xpier/xissue/internal/store"
)

func TestRemoteRunnerLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("remote lifecycle uses git and a fake ACP process")
	}
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "seed")
	mustGit(t, root, "init", "--bare", remote)
	mustGit(t, root, "init", work)
	mustGit(t, work, "config", "user.email", "test@example.com")
	mustGit(t, work, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", ".")
	mustGit(t, work, "commit", "-m", "seed")
	mustGit(t, work, "branch", "-M", "main")
	mustGit(t, work, "remote", "add", "origin", remote)
	mustGit(t, work, "push", "origin", "main")

	agent := filepath.Join(root, "fake-acp.sh")
	body := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{},"agentInfo":{"name":"fake"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      mkdir -p repo
      printf 'remote\n' >> repo/remote.txt
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"fake-remote-session"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"fake-remote-session","update":{"sessionUpdate":"usage_update","used":321,"size":200000,"cost":{"amount":0.01,"currency":"USD"}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"fake-remote-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"remote done"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":11,"outputTokens":7,"totalTokens":18}}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(agent, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.DataDir = filepath.Join(root, "server-data")
	cfg.Monitor.Enabled = false
	cfg.Agents["fake"] = config.AgentConfig{Command: agent, MemoryMB: 10}
	st, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := New(cfg, st)
	defer srv.Shutdown()
	hs := httptest.NewServer(srv.Handler())
	defer hs.Close()

	remoteData := filepath.Join(root, "runner-data")
	runnerCtx, stopRunner := context.WithCancel(ctx)
	defer stopRunner()
	go func() {
		_ = remoterunner.Run(runnerCtx, remoterunner.Options{
			Server: hs.URL, Name: "test-runner", DataDir: remoteData, Version: "test",
		})
	}()
	waitFor(t, 10*time.Second, "runner registration", func() bool {
		return srv.remote.Online("test-runner")
	})

	proj, err := st.CreateProject(ctx, "Remote", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRepo(ctx, domain.Repo{
		ProjectID: proj.ID, Name: "repo", Provider: domain.ProviderPlain,
		RemoteURL: remote, DefaultBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	runnerID := "test-runner"
	if _, err := st.UpdateProject(ctx, proj.ID, store.ProjectPatch{RunnerID: &runnerID}); err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "remote task", "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := srv.runner.StartRun(ctx, issue.ID, "fake", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if run.RunnerID != runnerID {
		t.Fatalf("run runnerId = %q, want %q", run.RunnerID, runnerID)
	}
	waitRunStatusServer(t, st, run.ID, domain.RunCompleted, 20*time.Second)
	got, err := st.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Workdir, filepath.Join(remoteData, "runs")) {
		t.Fatalf("remote workdir = %q", got.Workdir)
	}
	if got.InputTokens != 11 || got.TotalTokens != 18 {
		t.Fatalf("usage tokens = %d/%d", got.InputTokens, got.TotalTokens)
	}
	if got.CostAmount <= 0 {
		t.Fatalf("usage cost = %f", got.CostAmount)
	}
	events, _ := st.ListEvents(ctx, run.ID, 0)
	if !eventMessage(events, "remote done") {
		t.Fatalf("missing remote message event: %+v", events)
	}

	changes, err := srv.runner.Changes(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || len(changes[0].Files) == 0 {
		t.Fatalf("remote changes = %+v", changes)
	}
	content, err := srv.runner.ReadFile(ctx, run.ID, changes[0].RepoID, "remote.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(content) != "remote" {
		t.Fatalf("remote file content = %q", content)
	}
	if err := srv.runner.WriteFile(ctx, run.ID, changes[0].RepoID, "remote.txt", "edited\n"); err != nil {
		t.Fatal(err)
	}
	content, _ = srv.runner.ReadFile(ctx, run.ID, changes[0].RepoID, "remote.txt", "")
	if strings.TrimSpace(content) != "edited" {
		t.Fatalf("edited remote file = %q", content)
	}

	// Follow-up turns should reuse the remote workspace and append another event.
	before := countEvents(events, "message")
	if err := srv.runner.SendMessage(ctx, run.ID, "follow-up"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "remote follow-up", func() bool {
		evs, _ := st.ListEvents(ctx, run.ID, 0)
		return countEvents(evs, "message") >= before+1
	})
	waitRunStatusServer(t, st, run.ID, domain.RunCompleted, 20*time.Second)

	results, err := srv.runner.Submit(ctx, run.ID, runner.SubmitRequest{Mode: "push", CommitMessage: "remote submit"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Commit == "" || results[0].Error != "" {
		t.Fatalf("submit results = %+v", results)
	}
	submitted, _ := st.GetRun(ctx, run.ID)
	if submitted.Status != domain.RunSubmitted {
		t.Fatalf("submitted status = %s", submitted.Status)
	}
	if submitted.Workdir != "" {
		t.Fatalf("remote workdir should be cleared after submit, got %q", submitted.Workdir)
	}
	if _, err := os.Stat(got.Workdir); !os.IsNotExist(err) {
		t.Fatalf("remote workdir still exists after submit: %v", err)
	}
	branchOut, err := exec.Command("git", "--git-dir", remote, "branch", "--list", got.Branch).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(branchOut), got.Branch) {
		t.Fatalf("pushed branch %q not found: %s", got.Branch, branchOut)
	}
}

func countEvents(events []domain.RunEvent, typ string) int {
	n := 0
	for _, ev := range events {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

func eventMessage(events []domain.RunEvent, text string) bool {
	for _, ev := range events {
		if ev.Type == "message" && strings.Contains(string(ev.Data), text) {
			return true
		}
	}
	return false
}

func waitRunStatusServer(t *testing.T, st *store.Store, runID int64, want domain.RunStatus, timeout time.Duration) {
	t.Helper()
	waitFor(t, timeout, "run "+string(want), func() bool {
		run, err := st.GetRun(context.Background(), runID)
		return err == nil && run.Status == want
	})
}

func waitFor(t *testing.T, timeout time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRemoteRunnerOfflineRejectsStart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = root
	cfg.Monitor.Enabled = false
	cfg.Agents["fake"] = config.AgentConfig{Command: "fake-acp"}
	st, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := New(cfg, st)
	defer srv.Shutdown()
	proj, err := st.CreateProject(ctx, "Offline", "")
	if err != nil {
		t.Fatal(err)
	}
	offline := "missing-runner"
	if _, err := st.UpdateProject(ctx, proj.ID, store.ProjectPatch{RunnerID: &offline}); err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "offline task", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRepo(ctx, domain.Repo{ProjectID: proj.ID, Name: "repo", RemoteURL: t.TempDir(), DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.runner.StartRun(ctx, issue.ID, "fake", "", ""); !errors.Is(err, runner.ErrRemoteRunnerOffline) {
		t.Fatalf("StartRun error = %v, want ErrRemoteRunnerOffline", err)
	}
	runs, err := st.ListRuns(ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("offline start created %d runs", len(runs))
	}
}

func TestRemoteRunnerPermissionRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("remote permission uses a fake ACP process")
	}
	ctx := context.Background()
	root := t.TempDir()
	agent := filepath.Join(root, "fake-perm-acp.sh")
	body := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{},"agentInfo":{"name":"fake"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"perm-session"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":900,"method":"session/request_permission","params":{"toolCall":{"title":"deploy","kind":"other"},"options":[{"optionId":"allow-1","name":"Allow","kind":"allow_once"},{"optionId":"reject-1","name":"Reject","kind":"reject_once"}]}}\n'
      IFS= read -r resp
      case "$resp" in
        *'"optionId":"allow-1"'*) text='permission-approved' ;;
        *) text='permission-denied' ;;
      esac
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"perm-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"%s"}}}}\n' "$text"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(agent, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = filepath.Join(root, "server-data")
	cfg.Monitor.Enabled = false
	cfg.Agents["fake"] = config.AgentConfig{Command: agent, MemoryMB: 10}
	st, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := New(cfg, st)
	defer srv.Shutdown()
	hs := httptest.NewServer(srv.Handler())
	defer hs.Close()
	runnerCtx, stopRunner := context.WithCancel(ctx)
	defer stopRunner()
	go func() {
		_ = remoterunner.Run(runnerCtx, remoterunner.Options{Server: hs.URL, Name: "perm-runner", DataDir: filepath.Join(root, "runner-data")})
	}()
	waitFor(t, 10*time.Second, "permission runner registration", func() bool {
		return srv.remote.Online("perm-runner")
	})
	proj, err := st.CreateProject(ctx, "Perm", "")
	if err != nil {
		t.Fatal(err)
	}
	rid := "perm-runner"
	if _, err := st.UpdateProject(ctx, proj.ID, store.ProjectPatch{RunnerID: &rid}); err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "permission task", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := srv.runner.StartRun(ctx, issue.ID, "fake", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var requestID string
	waitFor(t, 20*time.Second, "permission request", func() bool {
		evs, _ := st.ListEvents(ctx, run.ID, 0)
		for _, ev := range evs {
			if ev.Type != "permission_request" {
				continue
			}
			var data map[string]any
			if json.Unmarshal(ev.Data, &data) == nil {
				requestID, _ = data["requestId"].(string)
			}
			if requestID != "" {
				return true
			}
		}
		return false
	})
	got, _ := st.GetRun(ctx, run.ID)
	if got.Status != domain.RunAwaiting {
		t.Fatalf("status while awaiting = %s", got.Status)
	}
	if err := srv.runner.RespondPermission(run.ID, requestID, "allow-1"); err != nil {
		t.Fatal(err)
	}
	waitRunStatusServer(t, st, run.ID, domain.RunCompleted, 20*time.Second)
	evs, _ := st.ListEvents(ctx, run.ID, 0)
	if !eventMessage(evs, "permission-approved") {
		t.Fatalf("permission response did not reach agent: %+v", evs)
	}
}

func TestRemoteRunnerRejectsBrowserOrigin(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = root
	cfg.Monitor.Enabled = false
	st, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := New(cfg, st)
	defer srv.Shutdown()
	req := httptest.NewRequest("GET", "/api/runners/ws", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("browser origin status = %d, want 403", rec.Code)
	}
}
