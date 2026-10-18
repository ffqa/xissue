// Package runner orchestrates worktrees, agent ACP processes, permission prompts, and review submissions.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/store"
)

// Notifier broadcasts state updates to connected WebSocket clients.
type Notifier interface {
	BroadcastRunEvent(runID, issueID int64, ev *domain.RunEvent)
	BroadcastRunUpdated(run *domain.Run)
	BroadcastIssueUpdated(issue *domain.Issue)
}

// AgentStatus describes a configured agent CLI and whether its binary is in PATH.
type AgentStatus struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Available bool   `json:"available"`
}

// RepoChanges groups file diffs per repository for the review screen.
type RepoChanges struct {
	RepoID   int64             `json:"repoId"`
	RepoName string            `json:"repoName"`
	Branch   string            `json:"branch"`
	Files    []gitx.FileChange `json:"files"`
}

// SubmitRequest commits and pushes (and optionally opens PRs) across modified repos.
type SubmitRequest struct {
	Mode          string `json:"mode"` // "pr" | "push"
	Title         string `json:"title"`
	Body          string `json:"body"`
	CommitMessage string `json:"commitMessage"`
}

// SubmitResult is the outcome of submitting one repository.
type SubmitResult struct {
	RepoID   int64  `json:"repoId"`
	RepoName string `json:"repoName"`
	Branch   string `json:"branch"`
	Commit   string `json:"commit"`
	PRURL    string `json:"prUrl"`
	Error    string `json:"error"`
}

type activeRun struct {
	runID   int64
	issueID int64
	client  *acp.Client
	cancel  context.CancelFunc
	permMu  sync.Mutex
	perms   map[string]chan string // requestID -> optionID
	reqSeq  atomic.Int64
	msgBuf  strings.Builder
}

// Runner manages local execution of runs.
type Runner struct {
	cfg      *config.Config
	st       *store.Store
	notify   Notifier
	budget   *budget
	mu       sync.Mutex
	active   map[int64]*activeRun // runID -> activeRun
	allowSet map[int64]bool       // runID -> allow_always chosen
}

func New(cfg *config.Config, st *store.Store, notify Notifier) *Runner {
	r := &Runner{
		cfg:      cfg,
		st:       st,
		notify:   notify,
		budget:   newBudget(cfg.Runner.MaxConcurrent, cfg.Runner.MemoryBudgetMB),
		active:   map[int64]*activeRun{},
		allowSet: map[int64]bool{},
	}
	// Mark any runs left active by a previous server process as failed.
	if runs, err := st.ListActiveRuns(context.Background()); err == nil {
		failed := domain.RunFailed
		errMsg := "server restarted while run was active"
		for _, run := range runs {
			_, _ = st.UpdateRun(context.Background(), run.ID, store.RunUpdate{
				Status: &failed, Error: &errMsg, Ended: true,
			})
		}
	}
	return r
}

// ListAgents returns configured agents and whether their command is installed.
func (r *Runner) ListAgents() []AgentStatus {
	out := make([]AgentStatus, 0, len(r.cfg.Agents))
	for name, ac := range r.cfg.Agents {
		_, err := exec.LookPath(ac.Command)
		out = append(out, AgentStatus{Name: name, Command: ac.Command, Available: err == nil})
	}
	return out
}

// StartRun creates a new Run record and launches the agent asynchronously.
func (r *Runner) StartRun(ctx context.Context, issueID int64, agentName, extraPrompt string) (*domain.Run, error) {
	ac, ok := r.cfg.Agents[agentName]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", agentName)
	}
	if _, err := exec.LookPath(ac.Command); err != nil {
		return nil, fmt.Errorf("agent binary %q not found in PATH", ac.Command)
	}
	issue, err := r.st.GetIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	proj, err := r.st.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return nil, err
	}
	run, err := r.st.CreateRun(ctx, issueID, agentName)
	if err != nil {
		return nil, err
	}
	branch := fmt.Sprintf("agent/%s-run-%d", sanitizeBranch(issue.Key), run.ID)
	workdir := filepath.Join(r.cfg.RunsDir(), fmt.Sprintf("run-%d", run.ID))
	run, err = r.st.UpdateRun(ctx, run.ID, store.RunUpdate{Branch: &branch, Workdir: &workdir})
	if err != nil {
		return nil, err
	}

	running := domain.IssueRunning
	if u, err := r.st.UpdateIssue(ctx, issueID, store.IssuePatch{Status: &running}); err == nil {
		r.notify.BroadcastIssueUpdated(u)
	}
	r.notify.BroadcastRunUpdated(run)

	prompt := buildInitialPrompt(proj, issue, extraPrompt)
	if strings.TrimSpace(extraPrompt) != "" {
		r.emitEvent(run.ID, issue.ID, "user", map[string]any{"text": extraPrompt})
	}
	go r.executeTurn(run.ID, issue.ID, proj, issue, ac, prompt, false)
	return run, nil
}

// SendMessage sends a follow-up instruction to a running or completed run (resuming in the same workdir).
func (r *Runner) SendMessage(ctx context.Context, runID int64, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("empty message")
	}
	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return err
	}
	proj, err := r.st.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return err
	}
	ac, ok := r.cfg.Agents[run.Agent]
	if !ok {
		return fmt.Errorf("unknown agent %q", run.Agent)
	}

	r.mu.Lock()
	act := r.active[runID]
	r.mu.Unlock()
	if act != nil {
		return errors.New("agent is currently busy; cancel or wait for the current turn to finish")
	}

	r.emitEvent(run.ID, issue.ID, "user", map[string]any{"text": text})
	running := domain.IssueRunning
	if u, err := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &running}); err == nil {
		r.notify.BroadcastIssueUpdated(u)
	}
	go r.executeTurn(run.ID, issue.ID, proj, issue, ac, text, true)
	return nil
}

// RespondPermission delivers the user's permission decision to a waiting run.
func (r *Runner) RespondPermission(runID int64, requestID, optionID string) error {
	r.mu.Lock()
	act := r.active[runID]
	r.mu.Unlock()
	if act == nil {
		return errors.New("run is not active")
	}
	act.permMu.Lock()
	ch := act.perms[requestID]
	act.permMu.Unlock()
	if ch == nil {
		return errors.New("permission request not found or already resolved")
	}
	select {
	case ch <- optionID:
	default:
	}
	return nil
}

// Cancel terminates an active run.
func (r *Runner) Cancel(runID int64) error {
	r.mu.Lock()
	act := r.active[runID]
	r.mu.Unlock()
	if act == nil {
		return errors.New("run is not active")
	}
	if act.client != nil {
		act.client.Cancel()
	}
	act.cancel()
	return nil
}

func (r *Runner) executeTurn(runID, issueID int64, proj *domain.Project, issue *domain.Issue, ac config.AgentConfig, prompt string, isResume bool) {
	ctx, cancel := context.WithCancel(context.Background())
	act := &activeRun{
		runID:   runID,
		issueID: issueID,
		cancel:  cancel,
		perms:   map[string]chan string{},
	}
	r.mu.Lock()
	r.active[runID] = act
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.active, runID)
		r.mu.Unlock()
	}()

	memMB := ac.MemoryMB
	if memMB <= 0 {
		memMB = 400
	}
	if err := r.budget.acquire(ctx, memMB); err != nil {
		r.failRun(runID, issueID, "cancelled while queued")
		return
	}
	defer r.budget.release(memMB)

	r.setRunStatus(runID, issueID, domain.RunStarting, true, false)

	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		return
	}
	if !isResume {
		if err := r.prepareWorkspace(ctx, run, proj, issue); err != nil {
			r.failRun(runID, issueID, fmt.Sprintf("workspace setup failed: %v", err))
			return
		}
	} else if _, err := os.Stat(run.Workdir); err != nil {
		if err := r.prepareWorkspace(ctx, run, proj, issue); err != nil {
			r.failRun(runID, issueID, fmt.Sprintf("workspace setup failed: %v", err))
			return
		}
	}

	var wrap []string
	if r.cfg.Runner.UseSystemdRun && runtime.GOOS == "linux" && ac.MemoryMB > 0 {
		if _, err := exec.LookPath("systemd-run"); err == nil {
			wrap = []string{"systemd-run", "--user", "--scope", "-q", fmt.Sprintf("-pMemoryMax=%dM", ac.MemoryMB), "--"}
		}
	}

	handlers := acp.Handlers{
		OnEvent: func(ev acp.Event) {
			if ev.Type == "message" {
				if t, ok := ev.Data["text"].(string); ok {
					act.msgBuf.WriteString(t)
				}
			}
			r.emitEvent(runID, issueID, ev.Type, ev.Data)
		},
		OnPermission: func(pctx context.Context, req acp.PermissionRequest) string {
			return r.handlePermission(pctx, act, req)
		},
	}

	client, err := acp.Start(ctx, acp.Options{
		Command: ac.Command,
		Args:    ac.Args,
		Env:     ac.Env,
		Dir:     run.Workdir,
		Wrap:    wrap,
	}, handlers)
	if err != nil {
		r.failRun(runID, issueID, fmt.Sprintf("start agent: %v", err))
		return
	}
	act.client = client
	defer client.Close()

	var sessPrompt string
	if isResume && run.SessionID != "" && client.CanLoad {
		if err := client.LoadSession(ctx, run.Workdir, run.SessionID); err == nil {
			sessPrompt = prompt
		}
	}
	if sessPrompt == "" {
		if err := client.NewSession(ctx, run.Workdir); err != nil {
			r.failRun(runID, issueID, fmt.Sprintf("session/new: %v", err))
			return
		}
		sid := client.SessionID
		_, _ = r.st.UpdateRun(context.Background(), runID, store.RunUpdate{SessionID: &sid})
		if isResume {
			sessPrompt = fmt.Sprintf("Continuing work on [%s] %s in this existing workspace.\n\nUser feedback / follow-up:\n%s", issue.Key, issue.Title, prompt)
		} else {
			sessPrompt = prompt
		}
	}

	r.setRunStatus(runID, issueID, domain.RunRunning, false, false)

	_, promptErr := client.Prompt(ctx, sessPrompt)
	if ctx.Err() != nil {
		r.setRunStatus(runID, issueID, domain.RunCancelled, false, true)
		return
	}
	if promptErr != nil {
		r.failRun(runID, issueID, promptErr.Error())
		return
	}

	summary := strings.TrimSpace(act.msgBuf.String())
	if len(summary) > 4000 {
		summary = summary[len(summary)-4000:]
	}
	done := domain.RunCompleted
	updatedRun, _ := r.st.UpdateRun(context.Background(), runID, store.RunUpdate{
		Status: &done, Summary: &summary, Ended: true,
	})
	if updatedRun != nil {
		r.emitEvent(runID, issueID, "status", map[string]any{"status": done})
		r.notify.BroadcastRunUpdated(updatedRun)
	}

	review := domain.IssueReview
	if u, err := r.st.UpdateIssue(context.Background(), issueID, store.IssuePatch{Status: &review}); err == nil {
		r.notify.BroadcastIssueUpdated(u)
	}

	r.writeRunSummaryComment(context.Background(), proj, issueID, run, summary)
	r.ExportIssueMarkdown(context.Background(), issueID)
}

func (r *Runner) prepareWorkspace(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue) error {
	if err := os.MkdirAll(run.Workdir, 0o755); err != nil {
		return err
	}
	for _, repo := range proj.Repos {
		r.emitEvent(run.ID, issue.ID, "system", map[string]any{
			"text": fmt.Sprintf("Preparing repository %s (%s @ %s)…", repo.Name, repo.RemoteURL, repo.DefaultBranch),
		})
		mirror := filepath.Join(r.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repo.ID))
		if err := gitx.EnsureMirror(ctx, repo.RemoteURL, mirror); err != nil {
			return fmt.Errorf("mirror %s: %w", repo.Name, err)
		}
		wtDir := filepath.Join(run.Workdir, repo.Name)
		if _, err := os.Stat(wtDir); err == nil {
			continue
		}
		if err := gitx.AddWorktree(ctx, mirror, wtDir, run.Branch, repo.DefaultBranch); err != nil {
			return fmt.Errorf("worktree %s: %w", repo.Name, err)
		}
	}
	agentsMD := buildWorkspaceAGENTS(proj, issue)
	_ = os.WriteFile(filepath.Join(run.Workdir, "AGENTS.md"), []byte(agentsMD), 0o644)
	return nil
}

func (r *Runner) handlePermission(ctx context.Context, act *activeRun, req acp.PermissionRequest) string {
	title, _ := req.ToolCall["title"].(string)
	kind, _ := req.ToolCall["kind"].(string)
	if title == "" {
		title = "Tool execution permission"
	}
	reqID := fmt.Sprintf("perm-%d", act.reqSeq.Add(1))

	// Check policy for auto-approval.
	if opt := r.autoApproveOption(act.runID, kind, title, req.ToolCall, req.Options); opt != "" {
		r.emitEvent(act.runID, act.issueID, "permission_request", map[string]any{
			"requestId": reqID, "title": title, "toolCall": req.ToolCall, "options": req.Options,
		})
		r.emitEvent(act.runID, act.issueID, "permission_resolved", map[string]any{
			"requestId": reqID, "optionId": opt, "auto": true,
		})
		return opt
	}

	ch := make(chan string, 1)
	act.permMu.Lock()
	act.perms[reqID] = ch
	act.permMu.Unlock()
	defer func() {
		act.permMu.Lock()
		delete(act.perms, reqID)
		act.permMu.Unlock()
	}()

	r.emitEvent(act.runID, act.issueID, "permission_request", map[string]any{
		"requestId": reqID, "title": title, "toolCall": req.ToolCall, "options": req.Options,
	})
	r.setRunStatus(act.runID, act.issueID, domain.RunAwaiting, false, false)

	var chosen string
	select {
	case <-ctx.Done():
		return ""
	case chosen = <-ch:
	}

	for _, o := range req.Options {
		if o.OptionID == chosen && o.Kind == "allow_always" {
			r.mu.Lock()
			r.allowSet[act.runID] = true
			r.mu.Unlock()
		}
	}
	r.emitEvent(act.runID, act.issueID, "permission_resolved", map[string]any{
		"requestId": reqID, "optionId": chosen, "auto": false,
	})
	r.setRunStatus(act.runID, act.issueID, domain.RunRunning, false, false)
	return chosen
}

func (r *Runner) autoApproveOption(runID int64, kind, title string, tc map[string]any, opts []acp.PermissionOption) string {
	rawText := fmt.Sprintf("%s %v", title, tc["rawInput"])
	for _, pat := range r.cfg.Permissions.AlwaysAskPatterns {
		if pat != "" && strings.Contains(rawText, pat) {
			return ""
		}
	}
	r.mu.Lock()
	always := r.allowSet[runID]
	r.mu.Unlock()

	allowedKind := always
	if !allowedKind {
		for _, k := range r.cfg.Permissions.AutoAllowKinds {
			if strings.EqualFold(k, kind) {
				allowedKind = true
				break
			}
		}
	}
	if !allowedKind {
		return ""
	}
	for _, o := range opts {
		if o.Kind == "allow_once" || o.Kind == "allow_always" {
			return o.OptionID
		}
	}
	return ""
}

func (r *Runner) writeRunSummaryComment(ctx context.Context, proj *domain.Project, issueID int64, run *domain.Run, summary string) {
	var changed []string
	for _, repo := range proj.Repos {
		wt := filepath.Join(run.Workdir, repo.Name)
		if files, err := gitx.Changes(ctx, wt, repo.DefaultBranch); err == nil && len(files) > 0 {
			changed = append(changed, fmt.Sprintf("`%s` (%d files)", repo.Name, len(files)))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Run #%d** (`%s`, branch `%s`) completed.\n\n", run.ID, run.Agent, run.Branch)
	if len(changed) > 0 {
		fmt.Fprintf(&b, "- Modified repositories: %s\n\n", strings.Join(changed, ", "))
	} else {
		b.WriteString("- No file changes detected.\n\n")
	}
	if summary != "" {
		b.WriteString(summary)
	}
	_, _ = r.st.AddComment(ctx, issueID, "agent", b.String())
}

// ---------- Diff & Submit ----------

func (r *Runner) Changes(ctx context.Context, runID int64) ([]RepoChanges, error) {
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := make([]RepoChanges, 0, len(proj.Repos))
	for _, repo := range proj.Repos {
		wt := filepath.Join(run.Workdir, repo.Name)
		files, err := gitx.Changes(ctx, wt, repo.DefaultBranch)
		if err != nil || files == nil {
			files = []gitx.FileChange{}
		}
		out = append(out, RepoChanges{
			RepoID:   repo.ID,
			RepoName: repo.Name,
			Branch:   run.Branch,
			Files:    files,
		})
	}
	return out, nil
}

func (r *Runner) ReadFile(ctx context.Context, runID, repoID int64, relPath, side string) (string, error) {
	run, repo, wt, abs, err := r.resolveRepoFile(ctx, runID, repoID, relPath)
	if err != nil {
		return "", err
	}
	_ = run
	if side == "base" {
		return gitx.ShowBase(ctx, wt, repo.DefaultBranch, relPath)
	}
	b, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func (r *Runner) WriteFile(ctx context.Context, runID, repoID int64, relPath, content string) error {
	_, _, _, abs, err := r.resolveRepoFile(ctx, runID, repoID, relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0o644)
}

func (r *Runner) Submit(ctx context.Context, runID int64, req SubmitRequest) ([]SubmitResult, error) {
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return nil, err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return nil, err
	}
	commitMsg := strings.TrimSpace(req.CommitMessage)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("%s (%s)", issue.Title, issue.Key)
	}
	prTitle := strings.TrimSpace(req.Title)
	if prTitle == "" {
		prTitle = commitMsg
	}
	prBody := strings.TrimSpace(req.Body)
	if prBody == "" {
		prBody = fmt.Sprintf("Issue: %s — %s", issue.Key, issue.Title)
		if issue.RemoteURL != "" {
			prBody += "\n\nRef: " + issue.RemoteURL
		}
	}

	var results []SubmitResult
	var prLinks []string
	anyPushed := false

	for _, repo := range proj.Repos {
		wt := filepath.Join(run.Workdir, repo.Name)
		changed, err := gitx.HasChanges(ctx, wt, repo.DefaultBranch)
		if err != nil || !changed {
			continue
		}
		res := SubmitResult{RepoID: repo.ID, RepoName: repo.Name, Branch: run.Branch}
		sha, err := gitx.CommitAll(ctx, wt, commitMsg)
		if err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		res.Commit = sha
		if err := gitx.Push(ctx, wt, run.Branch); err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		anyPushed = true
		if req.Mode == "pr" {
			p := provider.For(repo.Provider)
			if p.Caps().PRs {
				url, err := p.CreatePR(ctx, repo, run.Branch, repo.DefaultBranch, prTitle, prBody)
				if err != nil {
					res.Error = err.Error()
				} else {
					res.PRURL = url
					prLinks = append(prLinks, fmt.Sprintf("- `%s`: %s", repo.Name, url))
				}
			}
		}
		results = append(results, res)
	}

	if anyPushed {
		st := domain.RunSubmitted
		if u, err := r.st.UpdateRun(ctx, run.ID, store.RunUpdate{Status: &st}); err == nil {
			r.notify.BroadcastRunUpdated(u)
		}
		ist := domain.IssueDone
		if len(prLinks) > 0 {
			ist = domain.IssuePROpen
		}
		if u, err := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &ist}); err == nil {
			r.notify.BroadcastIssueUpdated(u)
		}
		note := fmt.Sprintf("Submitted branch `%s`.", run.Branch)
		if len(prLinks) > 0 {
			note += "\n\nPull Requests:\n" + strings.Join(prLinks, "\n")
		}
		_, _ = r.st.AddComment(ctx, issue.ID, "system", note)
		if issue.Source == "remote" && issue.RepoID != nil && issue.RemoteNumber != nil {
			if repo, err := r.st.GetRepo(ctx, *issue.RepoID); err == nil {
				_ = provider.For(repo.Provider).Comment(ctx, *repo, *issue.RemoteNumber, note)
			}
		}
		r.ExportIssueMarkdown(ctx, issue.ID)
	}
	return results, nil
}

func (r *Runner) Discard(ctx context.Context, runID int64) error {
	_ = r.Cancel(runID)
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return err
	}
	for _, repo := range proj.Repos {
		mirror := filepath.Join(r.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repo.ID))
		wt := filepath.Join(run.Workdir, repo.Name)
		_ = gitx.RemoveWorktree(ctx, mirror, wt, run.Branch)
	}
	if run.Workdir != "" {
		_ = os.RemoveAll(run.Workdir)
	}
	st := domain.RunDiscarded
	if u, err := r.st.UpdateRun(ctx, run.ID, store.RunUpdate{Status: &st, Ended: true}); err == nil {
		r.notify.BroadcastRunUpdated(u)
	}
	return nil
}

// ---------- helpers ----------

func (r *Runner) runAndProject(ctx context.Context, runID int64) (*domain.Run, *domain.Project, error) {
	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return nil, nil, err
	}
	proj, err := r.st.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	return run, proj, nil
}

func (r *Runner) resolveRepoFile(ctx context.Context, runID, repoID int64, relPath string) (*domain.Run, *domain.Repo, string, string, error) {
	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, "", "", err
	}
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return nil, nil, "", "", err
	}
	wt := filepath.Clean(filepath.Join(run.Workdir, repo.Name))
	abs := filepath.Clean(filepath.Join(wt, relPath))
	if abs != wt && !strings.HasPrefix(abs, wt+string(filepath.Separator)) {
		return nil, nil, "", "", errors.New("invalid file path")
	}
	return run, repo, wt, abs, nil
}

func (r *Runner) emitEvent(runID, issueID int64, typ string, data any) {
	ev, err := r.st.AppendEvent(context.Background(), runID, typ, data)
	if err == nil {
		r.notify.BroadcastRunEvent(runID, issueID, ev)
	}
}

func (r *Runner) setRunStatus(runID, issueID int64, st domain.RunStatus, started, ended bool) {
	u, err := r.st.UpdateRun(context.Background(), runID, store.RunUpdate{Status: &st, Started: started, Ended: ended})
	if err == nil {
		r.emitEvent(runID, issueID, "status", map[string]any{"status": st})
		r.notify.BroadcastRunUpdated(u)
	}
	if issue, err := r.st.GetIssue(context.Background(), issueID); err == nil {
		r.notify.BroadcastIssueUpdated(issue)
	}
}

func (r *Runner) failRun(runID, issueID int64, msg string) {
	st := domain.RunFailed
	u, err := r.st.UpdateRun(context.Background(), runID, store.RunUpdate{Status: &st, Error: &msg, Ended: true})
	if err == nil {
		r.emitEvent(runID, issueID, "stderr", map[string]any{"text": msg})
		r.emitEvent(runID, issueID, "status", map[string]any{"status": st})
		r.notify.BroadcastRunUpdated(u)
	}
	ist := domain.IssueFailed
	if issue, err := r.st.UpdateIssue(context.Background(), issueID, store.IssuePatch{Status: &ist}); err == nil {
		r.notify.BroadcastIssueUpdated(issue)
	}
}

var nonBranchChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeBranch(s string) string {
	s = strings.ToLower(nonBranchChars.ReplaceAllString(s, "-"))
	return strings.Trim(s, "-")
}

func buildInitialPrompt(proj *domain.Project, issue *domain.Issue, extra string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Please resolve issue [%s]: %s\n\n", issue.Key, issue.Title)
	if strings.TrimSpace(issue.Body) != "" {
		b.WriteString("## Issue Description\n\n")
		b.WriteString(issue.Body + "\n\n")
	}
	b.WriteString("## Workspace Repositories\n\n")
	for _, repo := range proj.Repos {
		fmt.Fprintf(&b, "- `./%s/` (base branch `%s`", repo.Name, repo.DefaultBranch)
		if repo.TestCommand != "" {
			fmt.Fprintf(&b, ", test command: `%s`", repo.TestCommand)
		}
		b.WriteString(")\n")
	}
	if strings.TrimSpace(extra) != "" {
		b.WriteString("\n## Additional Instructions\n\n" + extra + "\n")
	}
	b.WriteString("\nMake the required changes inside the repository directory/directories above, run relevant tests, and summarize what you changed. Do not run `git push`.\n")
	return b.String()
}
