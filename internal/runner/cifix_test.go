package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/store"
)

func TestMaybeAutoFixCIDedupCapAndClear(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Monitor.Enabled = false
	cfg.Monitor.AutoFixCIFailures = true
	cfg.Runner.MaxConcurrent = 1
	cfg.Agents["fake"] = config.AgentConfig{Command: "/usr/bin/true", MemoryMB: 10}
	r := New(cfg, st, &dummyNotifier{})
	t.Cleanup(func() {
		r.Shutdown()
		_ = st.Close()
	})

	proj, err := st.CreateProject(ctx, "CI", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "fix CI", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(ctx, issue.ID, "fake", "")
	if err != nil {
		t.Fatal(err)
	}
	submitted := domain.RunSubmitted
	prURL := "https://example.test/acme/repo/pull/1"
	open := "open"
	if _, err := st.UpdateRun(ctx, run.ID, store.RunUpdate{Status: &submitted, PRURL: &prURL, PRState: &open}); err != nil {
		t.Fatal(err)
	}
	run, err = st.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	pr := &provider.PRDetailInfo{State: "open", URL: prURL, HeadSHA: "sha1", CIState: "failure"}

	r.maybeAutoFixCI(ctx, issue, run, pr)
	wantAttempts(t, r, run.ID, 1)

	// Same head while the queued/running turn still exists must not enqueue again.
	r.maybeAutoFixCI(ctx, issue, run, pr)
	wantAttempts(t, r, run.ID, 1)

	waitNoActiveRun(t, r, run.ID)
	run, _ = st.GetRun(ctx, run.ID)
	pr.HeadSHA = "sha2"
	r.maybeAutoFixCI(ctx, issue, run, pr)
	wantAttempts(t, r, run.ID, 2)

	// A permanent failure must stop after maxCIFixAttempts.
	r.ciFixMu.Lock()
	r.ciFixAttempts[run.ID] = ciFixAttempt{head: "sha2", attempts: maxCIFixAttempts}
	r.ciFixMu.Unlock()
	pr.HeadSHA = "sha3"
	r.maybeAutoFixCI(ctx, issue, run, pr)
	wantAttempts(t, r, run.ID, maxCIFixAttempts)

	// A successful pipeline resets the counter, allowing a later failure to fix.
	pr.CIState = "success"
	r.maybeAutoFixCI(ctx, issue, run, pr)
	r.ciFixMu.Lock()
	_, ok := r.ciFixAttempts[run.ID]
	r.ciFixMu.Unlock()
	if ok {
		t.Fatal("successful CI should clear the attempt counter")
	}
}

func wantAttempts(t *testing.T, r *Runner, runID int64, want int) {
	t.Helper()
	r.ciFixMu.Lock()
	got := r.ciFixAttempts[runID].attempts
	r.ciFixMu.Unlock()
	if got != want {
		t.Fatalf("attempts = %d, want %d", got, want)
	}
}

func waitNoActiveRun(t *testing.T, r *Runner, runID int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		active := r.active[runID] != nil
		r.mu.Unlock()
		if !active {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = r.Cancel(runID)
	t.Fatalf("run %d stayed active", runID)
}

func TestSubmitPushKeepsExistingPRMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	seed := filepath.Join(root, "seed")
	workdir := filepath.Join(root, "run")
	wt := filepath.Join(workdir, "demo")
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(root, "init", "--bare", remote)
	runGit(seed, "init", "-b", "main")
	runGit(seed, "config", "user.email", "test@example.com")
	runGit(seed, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(seed, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(seed, "add", ".")
	runGit(seed, "commit", "-m", "init")
	runGit(seed, "remote", "add", "origin", remote)
	runGit(seed, "push", "origin", "main")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(workdir, "clone", remote, "demo")
	runGit(wt, "config", "user.email", "test@example.com")
	runGit(wt, "config", "user.name", "Test")
	runGit(wt, "checkout", "-b", "agent/fix")
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	proj, err := st.CreateProject(ctx, "Push", "")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := st.CreateRepo(ctx, domain.Repo{
		ProjectID: proj.ID, Name: "demo", RemoteURL: remote, DefaultBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssueInRepo(ctx, proj.ID, &repo.ID, "fix CI", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(ctx, issue.ID, "fake", "")
	if err != nil {
		t.Fatal(err)
	}
	submitted := domain.RunSubmitted
	branch := "agent/fix"
	prURL := "https://example.test/acme/demo/pull/9"
	open := "open"
	if _, err := st.UpdateRun(ctx, run.ID, store.RunUpdate{
		Status: &submitted, Workdir: &workdir, Branch: &branch, PRURL: &prURL, PRState: &open,
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	r := &Runner{cfg: cfg, st: st, notify: &dummyNotifier{}, active: map[int64]*activeRun{}, warm: map[int64]*warmSession{}}
	if _, err := r.Submit(ctx, run.ID, SubmitRequest{Mode: "push", CommitMessage: "fix: CI"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PRURL != prURL || got.PRState != open {
		t.Fatalf("push cleared PR metadata: url=%q state=%q", got.PRURL, got.PRState)
	}
	gotIssue, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotIssue.Status != domain.IssuePROpen {
		t.Fatalf("issue status = %s, want pr_open", gotIssue.Status)
	}
}

func TestCIFixResumeRecreatesWorkspaceFromRemoteBranch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	seed := filepath.Join(root, "seed")
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(root, "init", "--bare", remote)
	runGit(seed, "init", "-b", "main")
	runGit(seed, "config", "user.email", "test@example.com")
	runGit(seed, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(seed, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(seed, "add", ".")
	runGit(seed, "commit", "-m", "init")
	runGit(seed, "remote", "add", "origin", remote)
	runGit(seed, "push", "origin", "main")

	agent := filepath.Join(root, "fake-acp.sh")
	body := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{},"agentInfo":{"name":"fake"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"fake-session"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      echo fix >> demo/a.txt
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"fake-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"fixed"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}}\n' "$id"
      ;;
  esac
done
`
	if err := os.WriteFile(agent, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(root, "data")
	cfg.Monitor.Enabled = false
	cfg.Runner.MaxConcurrent = 1
	cfg.Agents["fake"] = config.AgentConfig{Command: agent, MemoryMB: 10}
	r := New(cfg, st, &dummyNotifier{})
	t.Cleanup(r.Shutdown)

	proj, err := st.CreateProject(ctx, "CI Fix Resume", "")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := st.CreateRepo(ctx, domain.Repo{ProjectID: proj.ID, Name: "demo", RemoteURL: remote, DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssueInRepo(ctx, proj.ID, &repo.ID, "fix ci", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := r.StartRun(ctx, issue.ID, "fake", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, st, run.ID, domain.RunCompleted)
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		busy := r.active[run.ID] != nil
		r.mu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run stayed active after completion")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := r.Submit(ctx, run.ID, SubmitRequest{Mode: "push", CommitMessage: "initial"}); err != nil {
		t.Fatal(err)
	}
	afterSubmit, err := st.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterSubmit.Status != domain.RunSubmitted || afterSubmit.Workdir != "" {
		t.Fatalf("after submit status=%s workdir=%q", afterSubmit.Status, afterSubmit.Workdir)
	}
	// Simulate the CI-fix path: a follow-up turn must recreate the worktree
	// from the pushed branch, not from main, then auto-push.
	if err := r.sendMessage(ctx, run.ID, "fix the CI failure", true); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		final, err := st.GetRun(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if final.Status == domain.RunSubmitted && final.Workdir == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after CI fix status=%s workdir=%q", final.Status, final.Workdir)
		}
		time.Sleep(20 * time.Millisecond)
	}
	logOut, err := exec.CommandContext(ctx, "git", "--git-dir", remote, "log", "--oneline", "main.."+afterSubmit.Branch).Output()
	if err != nil {
		t.Fatalf("read remote branch: %v", err)
	}
	if lines := strings.Count(strings.TrimSpace(string(logOut)), "\n") + 1; lines < 2 {
		t.Fatalf("CI fix did not add a commit to the existing branch: %s", logOut)
	}
}
