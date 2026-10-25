package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/automation"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/store"
)

func (r *Runner) Submit(ctx context.Context, runID int64, req SubmitRequest) ([]SubmitResult, error) {
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return nil, err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(run.RunnerID) != "" {
		switch run.Status {
		case domain.RunQueued, domain.RunStarting, domain.RunRunning, domain.RunAwaiting:
			return nil, errors.New("cannot submit while agent is still running")
		}
		if r.remote == nil || !r.remote.Online(run.RunnerID) {
			return nil, fmt.Errorf("%w: %s", ErrRemoteRunnerOffline, run.RunnerID)
		}
		return r.remote.Submit(ctx, run, proj, issue, req)
	}
	r.mu.Lock()
	busy := r.active[runID] != nil
	r.mu.Unlock()
	if busy {
		return nil, errors.New("cannot submit while agent is still running")
	}
	r.closeWarmClient(runID)
	commitMsg := strings.TrimSpace(req.CommitMessage)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("%s (%s)", issue.Title, issue.Key)
	}
	prTitle := strings.TrimSpace(req.Title)
	if prTitle == "" {
		prTitle = commitMsg
	}

	var results []SubmitResult
	var prLinks []string
	var firstPRURL string
	var firstTargetBranch string
	var lastChangeRecord string
	anyPushed := false

	for _, repo := range reposForIssue(proj, issue) {
		wt, pathErr := repoWorktreePath(run.Workdir, repo.Name)
		if pathErr != nil {
			results = append(results, SubmitResult{RepoName: repo.Name, Error: pathErr.Error()})
			continue
		}
		files, err := gitx.Changes(ctx, wt, repo.DefaultBranch)
		if err != nil || len(files) == 0 {
			continue
		}
		targetBranch := strings.TrimSpace(req.TargetBranch)
		if targetBranch == "" && strings.TrimSpace(run.TargetBranch) != "" {
			targetBranch = run.TargetBranch
		}
		if targetBranch == "" {
			targetBranch = proj.TargetBranch
		}
		if targetBranch == "" {
			targetBranch = repo.DefaultBranch
		}
		if targetBranch == "" {
			targetBranch = "dev"
		}
		res := SubmitResult{
			RepoID:       repo.ID,
			RepoName:     repo.Name,
			Branch:       run.Branch,
			TargetBranch: targetBranch,
		}
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
		if firstTargetBranch == "" {
			firstTargetBranch = targetBranch
		}

		items := cleanChangeItems(req.ChangeItems)
		if len(items) == 0 {
			items = deriveChangeItems(issue.Title, files)
		}

		if req.Mode == "merge_local" {
			mirror := filepath.Join(r.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repo.ID))
			msha, merr := gitx.MergeBranchToTarget(ctx, wt, mirror, repo.RemoteURL, run.Branch, targetBranch, commitMsg)
			if merr != nil {
				res.Error = fmt.Sprintf("直接合入 %s 失败: %v", targetBranch, merr)
			} else {
				if msha != "" {
					res.Commit = msha
				}
				res.PRURL = fmt.Sprintf("(已直接合入 %s)", targetBranch)
			}
		} else if req.Mode == "pr" {
			p := provider.For(repo.Provider)
			if p.Caps().PRs {
				preRecord := FormatChangeRecord(items, run.Branch, targetBranch, "(创建中)", req.Remark)
				prBody := strings.TrimSpace(req.Body)
				if prBody == "" {
					prBody = preRecord
					if issue.RemoteURL != "" {
						prBody += "\n\nRef: " + issue.RemoteURL
					}
				}
				url, err := p.CreatePR(ctx, repo, run.Branch, targetBranch, prTitle, prBody)
				if err != nil {
					res.Error = err.Error()
				} else {
					res.PRURL = url
					if firstPRURL == "" {
						firstPRURL = url
					}
					prLinks = append(prLinks, fmt.Sprintf("- `%s`: %s", repo.Name, url))
					r.dispatchPRCreated(proj, &repo, issue, &provider.PRDetailInfo{
						Number:      prNumberFromURL(url),
						Title:       prTitle,
						State:       "open",
						URL:         url,
						HeadRefName: run.Branch,
						BaseRefName: targetBranch,
					})
				}
			}
		}

		prDisplay := res.PRURL
		if prDisplay == "" && req.Mode == "push" {
			if run.PRURL != "" {
				prDisplay = run.PRURL
			} else {
				prDisplay = "(仅推送分支，未提 PR)"
			}
		}
		record := FormatChangeRecord(items, run.Branch, targetBranch, prDisplay, req.Remark)
		res.ChangeRecord = record
		lastChangeRecord = record
		results = append(results, res)
	}

	hasFailures := false
	succeededRepos := make(map[string]bool)
	for _, res := range results {
		if res.Error != "" {
			hasFailures = true
		} else if res.Commit != "" {
			succeededRepos[res.RepoName] = true
		}
	}

	if anyPushed {
		st := domain.RunSubmitted
		prState := ""
		if firstPRURL != "" {
			prState = "open"
		}
		upd := store.RunUpdate{
			Status:       &st,
			TargetBranch: &firstTargetBranch,
			ChangeRecord: &lastChangeRecord,
			Ended:        true,
		}
		// A plain push (including CI fixes on an existing PR branch) must not
		// clear the PR URL/state recorded by the earlier submission.
		if firstPRURL != "" {
			upd.PRURL = &firstPRURL
			upd.PRState = &prState
		}
		if u, err := r.st.UpdateRun(ctx, run.ID, upd); err == nil {
			r.notify.BroadcastRunUpdated(u)
		}
		ist := domain.IssueDone
		if len(prLinks) > 0 || run.PRURL != "" {
			ist = domain.IssuePROpen
		}
		if u, err := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &ist}); err == nil {
			r.notify.BroadcastIssueUpdated(u)
		}
		note := lastChangeRecord
		if note == "" {
			note = fmt.Sprintf("Submitted branch `%s`.", run.Branch)
		}
		_, _ = r.st.AddComment(ctx, issue.ID, "system", "```text\n"+note+"\n```")
		if issue.Source == "remote" && issue.RepoID != nil && issue.RemoteNumber != nil {
			if repo, err := r.st.GetRepo(ctx, *issue.RepoID); err == nil {
				_ = provider.For(repo.Provider).Comment(ctx, *repo, *issue.RemoteNumber, note)
			}
		}
		r.ExportIssueMarkdown(ctx, issue.ID)
		_, _ = r.ArchiveRun(ctx, run.ID)

		if !hasFailures {
			r.cleanupRunWorkdir(ctx, run, proj)
			empty := ""
			if u, err := r.st.UpdateRun(ctx, run.ID, store.RunUpdate{Workdir: &empty}); err == nil {
				r.notify.BroadcastRunUpdated(u)
			}
		} else {
			// 部分仓库失败：仅清理已成功的 worktree，保留未提交/未推送的仓库工作区
			r.cleanupSpecificRepoWorktrees(ctx, run, proj, succeededRepos)
			for i := range results {
				if results[i].Error != "" {
					results[i].Error += fmt.Sprintf(" (未推送修改已保留在工作区: %s/%s)", run.Workdir, results[i].RepoName)
				}
			}
		}

		if firstPRURL != "" {
			r.pushNotification("pr_status", fmt.Sprintf("[%s] PR 已创建 (Open)", issue.Key), firstPRURL, fmt.Sprintf("/issues/%d", issue.ID), "")
		}
	}
	return results, nil
}

func (r *Runner) cleanupSpecificRepoWorktrees(ctx context.Context, run *domain.Run, proj *domain.Project, targetRepos map[string]bool) {
	if strings.TrimSpace(run.RunnerID) != "" {
		return
	}
	cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = ctx
	for _, repo := range proj.Repos {
		if targetRepos[repo.Name] {
			mirror := filepath.Join(r.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repo.ID))
			wt, err := repoWorktreePath(run.Workdir, repo.Name)
			if err != nil {
				r.LogMonitor("WARN", "cleanup", fmt.Sprintf("跳过非法仓库工作区 %q: %v", repo.Name, err))
				continue
			}
			_ = gitx.RemoveWorktree(cctx, mirror, wt, run.Branch)
			_ = os.RemoveAll(wt)
		}
	}
}

func (r *Runner) cleanupRunWorkdir(ctx context.Context, run *domain.Run, proj *domain.Project) {
	_ = r.removeRunWorkdir(ctx, run, proj)
}

// removeRunWorkdir removes a run worktree tree only when it is inside the
// configured runs directory. It returns an error instead of clearing the DB
// pointer when the filesystem still holds data.
func (r *Runner) removeRunWorkdir(ctx context.Context, run *domain.Run, proj *domain.Project) error {
	if run.Workdir == "" {
		return nil
	}
	if strings.TrimSpace(run.RunnerID) != "" {
		// Remote workdirs live on the runner host. Never let a runner-supplied
		// path make the server delete a local workspace.
		return nil
	}
	runsRoot := filepath.Clean(r.cfg.RunsDir())
	workdir := filepath.Clean(run.Workdir)
	if workdir == runsRoot || !strings.HasPrefix(workdir, runsRoot+string(filepath.Separator)) {
		return fmt.Errorf("workdir %q is not inside %q", run.Workdir, runsRoot)
	}
	cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = ctx
	if proj != nil {
		for _, repo := range proj.Repos {
			mirror := filepath.Join(r.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repo.ID))
			wt, err := repoWorktreePath(run.Workdir, repo.Name)
			if err != nil {
				r.LogMonitor("WARN", "cleanup", fmt.Sprintf("跳过非法仓库工作区 %q: %v", repo.Name, err))
				continue
			}
			_ = gitx.RemoveWorktree(cctx, mirror, wt, run.Branch)
		}
	}
	return os.RemoveAll(workdir)
}

func (r *Runner) Discard(ctx context.Context, runID int64) error {
	_ = r.Cancel(runID)
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(run.RunnerID) != "" {
		if r.remote == nil || !r.remote.Online(run.RunnerID) {
			return fmt.Errorf("%w: %s", ErrRemoteRunnerOffline, run.RunnerID)
		}
		if err := r.remote.Discard(ctx, run, proj); err != nil {
			return err
		}
		r.ArchiveRemoteRun(run.ID)
		empty := ""
		st := domain.RunDiscarded
		if u, err := r.st.UpdateRun(ctx, run.ID, store.RunUpdate{Status: &st, Workdir: &empty, Ended: true}); err == nil {
			r.notify.BroadcastRunUpdated(u)
		}
		if run.EndedAt == nil {
			r.DispatchRemoteRunEvent(run.ID, automation.EventRunFinished)
		}
		return nil
	}
	r.mu.Lock()
	delete(r.allowSet, runID)
	r.mu.Unlock()
	_, _ = r.ArchiveRun(ctx, run.ID)
	r.cleanupRunWorkdir(ctx, run, proj)
	empty := ""
	st := domain.RunDiscarded
	if u, err := r.st.UpdateRun(ctx, run.ID, store.RunUpdate{Status: &st, Workdir: &empty, Ended: true}); err == nil {
		r.notify.BroadcastRunUpdated(u)
	}
	if run.EndedAt == nil {
		go r.dispatchRunEvent(run.ID, automation.EventRunFinished)
	}
	return nil
}

// Delete removes the run's worktree/branch on disk and deletes the run record.
func (r *Runner) Delete(ctx context.Context, runID int64) error {
	_ = r.Cancel(runID)
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(run.RunnerID) != "" {
		if r.remote == nil || !r.remote.Online(run.RunnerID) {
			return fmt.Errorf("%w: %s", ErrRemoteRunnerOffline, run.RunnerID)
		}
		if err := r.remote.Delete(ctx, run, proj); err != nil {
			return err
		}
		if err := r.st.DeleteRun(ctx, run.ID); err != nil {
			return err
		}
		if issue, err := r.st.GetIssue(ctx, run.IssueID); err == nil {
			hasActive := false
			if runs, rerr := r.st.ListRuns(ctx, issue.ID); rerr == nil {
				for _, rem := range runs {
					if rem.Status == domain.RunQueued || rem.Status == domain.RunStarting || rem.Status == domain.RunRunning || rem.Status == domain.RunAwaiting {
						hasActive = true
						break
					}
				}
			}
			if !hasActive && (issue.Status == domain.IssueFailed || issue.Status == domain.IssueRunning) {
				todo := domain.IssueTodo
				if u, uerr := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &todo}); uerr == nil {
					issue = u
				}
			}
			r.notify.BroadcastIssueUpdated(issue)
			r.ExportIssueMarkdown(ctx, issue.ID)
		}
		return nil
	}
	r.mu.Lock()
	delete(r.allowSet, runID)
	r.mu.Unlock()
	_, _ = r.ArchiveRun(ctx, run.ID)
	r.cleanupRunWorkdir(ctx, run, proj)
	if err := r.st.DeleteRun(ctx, run.ID); err != nil {
		return err
	}
	// If the issue was stuck in failed/running and has no active run remaining, reset it to todo.
	if issue, err := r.st.GetIssue(ctx, run.IssueID); err == nil {
		hasActive := false
		if runs, rerr := r.st.ListRuns(ctx, issue.ID); rerr == nil {
			for _, rem := range runs {
				if rem.Status == domain.RunQueued || rem.Status == domain.RunStarting || rem.Status == domain.RunRunning || rem.Status == domain.RunAwaiting {
					hasActive = true
					break
				}
			}
		}
		if !hasActive && (issue.Status == domain.IssueFailed || issue.Status == domain.IssueRunning) {
			todo := domain.IssueTodo
			if u, err := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &todo}); err == nil {
				issue = u
			}
		}
		r.notify.BroadcastIssueUpdated(issue)
		r.ExportIssueMarkdown(ctx, issue.ID)
	}
	return nil
}
