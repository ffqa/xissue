package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/automation"
	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/remote"
	"github.com/xpier/xissue/internal/store"
)

// ErrRemoteRunnerOffline is returned when a project points at a runner that is
// not currently connected.
var ErrRemoteRunnerOffline = errors.New("remote runner is offline")

// RemoteExecutor is implemented by the server's remote-runner manager. Runner
// uses it to route operations for projects that selected a remote runner.
type RemoteExecutor interface {
	Online(runnerID string) bool
	// Agents returns the runner's cached agent capability list. known=false
	// means probing has not finished, so callers must not reject an agent.
	Agents(runnerID string) ([]remote.AgentCapability, bool)
	Start(ctx context.Context, job remote.Job) error
	Send(ctx context.Context, job remote.Job) error
	Cancel(runID int64) error
	RespondPermission(runID int64, requestID, optionID string) error
	Submit(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue, req SubmitRequest) ([]SubmitResult, error)
	Changes(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue) ([]RepoChanges, error)
	ReadFile(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue, repoID int64, relPath, side string) (string, error)
	WriteFile(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue, repoID int64, relPath, content string) error
	Discard(ctx context.Context, run *domain.Run, proj *domain.Project) error
	Delete(ctx context.Context, run *domain.Run, proj *domain.Project) error
}

// SetRemoteExecutor wires the remote manager after construction.
func (r *Runner) SetRemoteExecutor(e RemoteExecutor) { r.remote = e }

// remoteContext returns the run/project/issue only when the run is remote.
// A nil run means "use the local implementation".
func (r *Runner) remoteContext(ctx context.Context, runID int64) (*domain.Run, *domain.Project, *domain.Issue, error) {
	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, nil, err
	}
	if strings.TrimSpace(run.RunnerID) == "" {
		return nil, nil, nil, nil
	}
	proj, issue, err := r.runContext(ctx, run)
	if err != nil {
		return nil, nil, nil, err
	}
	return run, proj, issue, nil
}

func (r *Runner) runContext(ctx context.Context, run *domain.Run) (*domain.Project, *domain.Issue, error) {
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return nil, nil, err
	}
	proj, err := r.st.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	return proj, issue, nil
}

func (r *Runner) remoteFor(proj *domain.Project) RemoteExecutor {
	if proj == nil || strings.TrimSpace(proj.RunnerID) == "" {
		return nil
	}
	if r.remote == nil || !r.remote.Online(proj.RunnerID) {
		return nil
	}
	return r.remote
}

func (r *Runner) remoteJob(proj *domain.Project, issue *domain.Issue, run *domain.Run, ac config.AgentConfig, prompt string, isResume, autoPush bool) remote.Job {
	repos := reposForIssue(proj, issue)
	runnerID := proj.RunnerID
	if strings.TrimSpace(run.RunnerID) != "" {
		runnerID = run.RunnerID
	}
	return remote.Job{
		RunID:           run.ID,
		RunnerID:        runnerID,
		IssueID:         issue.ID,
		ProjectID:       proj.ID,
		IssueKey:        issue.Key,
		IssueTitle:      issue.Title,
		Prompt:          prompt,
		AgentName:       run.Agent,
		Agent:           ac,
		Model:           run.Model,
		Branch:          run.Branch,
		TargetBranch:    run.TargetBranch,
		SessionID:       run.SessionID,
		Workdir:         run.Workdir,
		Repos:           repos,
		WorkspaceAGENTS: buildWorkspaceAGENTS(proj, issue),
		AutoPR:          run.AutoPR,
		IsResume:        isResume,
		AutoPush:        autoPush,
	}
}

// issueHasActiveRun reports whether the issue already has a local or remote
// run in progress. Remote runs are not kept in the in-memory active map.
func (r *Runner) issueHasActiveRunAny(ctx context.Context, issueID int64) bool {
	r.mu.Lock()
	for _, a := range r.active {
		if a.issueID == issueID {
			r.mu.Unlock()
			return true
		}
	}
	r.mu.Unlock()
	runs, err := r.st.ListRuns(ctx, issueID)
	if err != nil {
		return false
	}
	for _, run := range runs {
		switch run.Status {
		case domain.RunQueued, domain.RunStarting, domain.RunRunning, domain.RunAwaiting:
			return true
		}
	}
	return false
}

// RecordRemoteUsage applies an ACP usage_update event received from a remote runner.
func (r *Runner) RecordRemoteUsage(runID, issueID int64, data map[string]any) {
	r.recordUsage(&activeRun{}, runID, issueID, data)
}

// RecordRemoteTurnUsage applies the end-of-turn usage from a remote runner.
func (r *Runner) RecordRemoteTurnUsage(runID, issueID int64, usage *acp.Usage) {
	if usage == nil {
		return
	}
	r.recordTurnUsage(&activeRun{}, runID, issueID, usage)
}

// RemotePermissionDecision evaluates the server-side policy for a remote
// permission request. An empty option means the user must decide.
func (r *Runner) RemotePermissionDecision(runID int64, title, kind string, toolCall map[string]any, opts []acp.PermissionOption) (string, bool) {
	if opt := r.autoApproveOption(runID, kind, title, toolCall, opts); opt != "" {
		return opt, true
	}
	run, err := r.st.GetRun(context.Background(), runID)
	if err == nil && run.AutoPR {
		for _, o := range opts {
			if strings.Contains(strings.ToLower(o.Kind), "reject") || strings.Contains(strings.ToLower(o.OptionID), "reject") {
				return o.OptionID, true
			}
		}
		// No reject option: resolving with an empty option id cancels/denies
		// the request, matching the local auto-develop behaviour.
		return "", true
	}
	return "", false
}

// ForgetRemoteRun clears per-run server state after a remote run is terminal.
func (r *Runner) ForgetRemoteRun(runID int64) {
	r.mu.Lock()
	delete(r.allowSet, runID)
	r.mu.Unlock()
}

// RememberRemoteAllow records an allow_always choice made for a remote run.
func (r *Runner) RememberRemoteAllow(runID int64, kind string) {
	if kind == "" {
		kind = "read"
	}
	r.mu.Lock()
	if r.allowSet[runID] == nil {
		r.allowSet[runID] = map[string]bool{}
	}
	r.allowSet[runID][strings.ToLower(kind)] = true
	r.mu.Unlock()
}

// EmitRemoteEvent appends and broadcasts an event originating from a runner.
func (r *Runner) EmitRemoteEvent(runID, issueID int64, typ string, data any) {
	r.emitEvent(runID, issueID, typ, data)
}

// NotifyRemoteRun pushes a notification for a remote run lifecycle change.
func (r *Runner) NotifyRemoteRun(kind, title, body, link string) {
	r.pushNotification(kind, title, body, link, "")
}

// DispatchRemoteRunEvent triggers lifecycle automation for a remote run.
func (r *Runner) DispatchRemoteRunEvent(runID int64, event string) {
	go r.dispatchRunEvent(runID, event)
}

// ArchiveRemoteRun exports the run archive after completion.
func (r *Runner) ArchiveRemoteRun(runID int64) {
	r.archiveMu.Lock()
	if r.archiveClosed {
		r.archiveMu.Unlock()
		return
	}
	r.archiveWG.Add(1)
	r.archiveMu.Unlock()
	go func() {
		defer r.archiveWG.Done()
		_, _ = r.ArchiveRun(context.Background(), runID)
	}()
}

// SetRemoteRunStatus updates a run and mirrors the local status semantics.
func (r *Runner) SetRemoteRunStatus(runID, issueID int64, status domain.RunStatus, workdir, sessionID, summary, errText string) {
	upd := store.RunUpdate{Status: &status}
	if strings.TrimSpace(workdir) != "" {
		upd.Workdir = &workdir
	}
	if strings.TrimSpace(sessionID) != "" {
		upd.SessionID = &sessionID
	}
	if summary != "" {
		upd.Summary = &summary
	}
	if errText != "" {
		upd.Error = &errText
	}
	switch status {
	case domain.RunRunning:
		upd.Started = true
	case domain.RunCompleted, domain.RunFailed, domain.RunCancelled:
		upd.Ended = true
	}
	run, err := r.st.UpdateRun(context.Background(), runID, upd)
	if err == nil {
		r.emitEvent(runID, issueID, "status", map[string]any{"status": status})
		if r.notify != nil {
			r.notify.BroadcastRunUpdated(run)
		}
	}
	if issue, ierr := r.st.GetIssue(context.Background(), issueID); ierr == nil {
		if r.notify != nil {
			r.notify.BroadcastIssueUpdated(issue)
		}
	}
}

// FinishRemoteRun applies the same issue transitions and notifications as a
// local completion/failure, without pretending the worktree is local.
func (r *Runner) FinishRemoteRun(runID, issueID int64, status domain.RunStatus, summary, errText string) {
	run, _ := r.st.GetRun(context.Background(), runID)
	issue, _ := r.st.GetIssue(context.Background(), issueID)
	workdir, sessionID := "", ""
	if run != nil {
		workdir, sessionID = run.Workdir, run.SessionID
	}
	r.SetRemoteRunStatus(runID, issueID, status, workdir, sessionID, summary, errText)
	if status == domain.RunCompleted {
		review := domain.IssueReview
		if u, err := r.st.UpdateIssue(context.Background(), issueID, store.IssuePatch{Status: &review}); err == nil && r.notify != nil {
			r.notify.BroadcastIssueUpdated(u)
		}
		if issue != nil {
			body := fmt.Sprintf("**Remote run #%d** completed.\n\n", runID)
			if run != nil {
				body = fmt.Sprintf("**Remote run #%d** (`%s`, branch `%s`) completed.\n\n", runID, run.Agent, run.Branch)
			}
			if summary != "" {
				body += summary
			}
			_, _ = r.st.AddComment(context.Background(), issueID, "agent", body)
		}
		r.ExportIssueMarkdown(context.Background(), issueID)
		r.ArchiveRemoteRun(runID)
		if issue != nil {
			r.pushNotification("run_done", fmt.Sprintf("[%s] 执行完成，待验收", issue.Key), issue.Title, fmt.Sprintf("/issues/%d", issueID), "")
			r.dispatchRunEvent(runID, automation.EventRunFinished)
		}
	} else if status == domain.RunFailed {
		ist := domain.IssueFailed
		if u, err := r.st.UpdateIssue(context.Background(), issueID, store.IssuePatch{Status: &ist}); err == nil && r.notify != nil {
			r.notify.BroadcastIssueUpdated(u)
		}
		if issue != nil {
			r.pushNotification("run_failed", fmt.Sprintf("[%s] 远程执行失败", issue.Key), errText, fmt.Sprintf("/issues/%d", issueID), "")
			r.dispatchRunEvent(runID, automation.EventRunFinished)
		}
	} else if status == domain.RunCancelled {
		if issue != nil {
			r.dispatchRunEvent(runID, automation.EventRunFinished)
		}
	}
}

// FinalizeRemoteSubmit updates run/issue state after a remote submit. The
// manager fills PR URLs and change records first.
func (r *Runner) FinalizeRemoteSubmit(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue, req SubmitRequest, results []SubmitResult) (bool, error) {
	if run == nil || proj == nil || issue == nil {
		return false, errors.New("missing run context")
	}
	anyPushed := false
	allOK := true
	firstPRURL := ""
	firstTarget := ""
	lastRecord := ""
	var prLinks []string
	for _, res := range results {
		if res.Error != "" {
			allOK = false
			continue
		}
		if res.Commit == "" {
			continue
		}
		anyPushed = true
		if firstTarget == "" {
			firstTarget = res.TargetBranch
		}
		if res.PRURL != "" {
			if firstPRURL == "" {
				firstPRURL = res.PRURL
			}
			prLinks = append(prLinks, fmt.Sprintf("- `%s`: %s", res.RepoName, res.PRURL))
		}
		if res.ChangeRecord != "" {
			lastRecord = res.ChangeRecord
		}
	}
	if !anyPushed {
		return allOK, nil
	}
	st := domain.RunSubmitted
	prState := ""
	if firstPRURL != "" {
		prState = "open"
	}
	upd := store.RunUpdate{Status: &st, Ended: true, ChangeRecord: &lastRecord}
	if firstTarget != "" {
		upd.TargetBranch = &firstTarget
	}
	if firstPRURL != "" {
		upd.PRURL = &firstPRURL
		upd.PRState = &prState
	}
	if u, err := r.st.UpdateRun(ctx, run.ID, upd); err == nil && r.notify != nil {
		r.notify.BroadcastRunUpdated(u)
	}
	ist := domain.IssueDone
	if len(prLinks) > 0 || run.PRURL != "" {
		ist = domain.IssuePROpen
	}
	if u, err := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &ist}); err == nil && r.notify != nil {
		r.notify.BroadcastIssueUpdated(u)
	}
	note := lastRecord
	if note == "" {
		note = fmt.Sprintf("Submitted branch `%s` from remote runner.", run.Branch)
	}
	_, _ = r.st.AddComment(ctx, issue.ID, "system", "```text\n"+note+"\n```")
	if issue.Source == "remote" && issue.RepoID != nil && issue.RemoteNumber != nil {
		if repo, rerr := r.st.GetRepo(ctx, *issue.RepoID); rerr == nil {
			_ = provider.For(repo.Provider).Comment(ctx, *repo, *issue.RemoteNumber, note)
		}
	}
	r.ExportIssueMarkdown(ctx, issue.ID)
	r.ArchiveRemoteRun(run.ID)
	if firstPRURL != "" {
		r.pushNotification("pr_status", fmt.Sprintf("[%s] PR 已创建 (Open)", issue.Key), firstPRURL, fmt.Sprintf("/issues/%d", issue.ID), "")
	}
	return allOK, nil
}

// DispatchRemotePRCreated fires lifecycle hooks for a PR opened by a remote
// submission, matching the local submit path.
func (r *Runner) DispatchRemotePRCreated(proj *domain.Project, repo *domain.Repo, issue *domain.Issue, url, title, head, base string) {
	if proj == nil || repo == nil || issue == nil || strings.TrimSpace(url) == "" {
		return
	}
	r.dispatchPRCreated(proj, repo, issue, &provider.PRDetailInfo{
		Number:      prNumberFromURL(url),
		Title:       title,
		State:       "open",
		URL:         url,
		HeadRefName: head,
		BaseRefName: base,
	})
}

// UpdateRemoteWorkspace persists workdir/session metadata that arrives with a
// remote status frame, even if the frame is terminal and no running status was
// observed first.
func (r *Runner) UpdateRemoteWorkspace(runID int64, workdir, sessionID string) {
	if strings.TrimSpace(workdir) == "" && strings.TrimSpace(sessionID) == "" {
		return
	}
	upd := store.RunUpdate{}
	if strings.TrimSpace(workdir) != "" {
		upd.Workdir = &workdir
	}
	if strings.TrimSpace(sessionID) != "" {
		upd.SessionID = &sessionID
		if run, err := r.st.GetRun(context.Background(), runID); err == nil &&
			run.SessionID != "" && run.SessionID != sessionID {
			// A resume that fell back to session/new starts a new cumulative
			// cost baseline; keep the already-attributed total.
			zero := 0.0
			upd.UsageCumulativeCost = &zero
		}
	}
	if u, err := r.st.UpdateRun(context.Background(), runID, upd); err == nil && r.notify != nil {
		r.notify.BroadcastRunUpdated(u)
	}
}

// remoteAgentAvailable reports whether an agent was found on a remote runner.
func remoteAgentAvailable(agents []remote.AgentCapability, name string) bool {
	name = strings.TrimSpace(name)
	for _, a := range agents {
		if a.Name == name && a.Available {
			return true
		}
	}
	return false
}

// remoteAgentList renders the available agents for an actionable error.
func remoteAgentList(agents []remote.AgentCapability) string {
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		if a.Available {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		return " (no configured agent binary was found there)"
	}
	return " (available: " + strings.Join(names, ", ") + ")"
}
