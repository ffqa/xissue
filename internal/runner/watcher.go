package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/store"
)

// FormatChangeRecord formats the standardized change record requested by the user:
// 本次改动：
//  a. ...
//  b. ...
// 分支：xxx : xxxx
// PR：
// 备注：
func FormatChangeRecord(items []string, sourceBranch, targetBranch, prURL, remark string) string {
	cleaned := cleanChangeItems(items)
	if len(cleaned) == 0 {
		cleaned = []string{"代码更新与调整"}
	}
	var b strings.Builder
	b.WriteString("本次改动：\n")
	for i, item := range cleaned {
		letter := string(rune('a' + (i % 26)))
		if i >= 26 {
			letter = fmt.Sprintf("a%d", i-25)
		}
		// Strip existing leading "a. " / "1. " / "- " if user typed it manually.
		item = trimLeadingBullet(item)
		fmt.Fprintf(&b, " %s. %s\n", letter, item)
	}
	fmt.Fprintf(&b, "分支：%s : %s\n", strings.TrimSpace(sourceBranch), strings.TrimSpace(targetBranch))
	fmt.Fprintf(&b, "PR：%s\n", strings.TrimSpace(prURL))
	fmt.Fprintf(&b, "备注：%s", strings.TrimSpace(remark))
	return b.String()
}

func trimLeadingBullet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 3 && s[0] >= 'a' && s[0] <= 'z' && (s[1] == '.' || s[1] == ')') && s[2] == ' ' {
		return strings.TrimSpace(s[3:])
	}
	if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") {
		return strings.TrimSpace(s[2:])
	}
	return s
}

func cleanChangeItems(items []string) []string {
	var out []string
	for _, it := range items {
		for _, line := range strings.Split(it, "\n") {
			line = trimLeadingBullet(line)
			if line != "" {
				out = append(out, line)
			}
		}
	}
	return out
}

func deriveChangeItems(issueTitle string, files []gitx.FileChange) []string {
	var out []string
	if strings.TrimSpace(issueTitle) != "" {
		out = append(out, strings.TrimSpace(issueTitle))
	}
	limit := len(files)
	if limit > 6 {
		limit = 6
	}
	for i := 0; i < limit; i++ {
		f := files[i]
		action := "修改"
		switch f.Status {
		case "A":
			action = "新增"
		case "D":
			action = "删除"
		case "R":
			action = "重命名"
		}
		out = append(out, fmt.Sprintf("%s `%s` (+%d/-%d)", action, f.Path, f.Additions, f.Deletions))
	}
	if len(files) > limit {
		out = append(out, fmt.Sprintf("其余 %d 个文件变更", len(files)-limit))
	}
	return out
}

func (r *Runner) pushNotification(kind, title, body, link, meta string) {
	n, err := r.st.CreateNotification(context.Background(), kind, title, body, link, meta)
	if err == nil && n != nil {
		r.notify.BroadcastNotification(n)
	}
}

func (r *Runner) mirrorPath(repoID int64) string {
	return filepath.Join(r.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repoID))
}

func (r *Runner) ensureRepoMirror(ctx context.Context, repo domain.Repo) (string, error) {
	mirror := r.mirrorPath(repo.ID)
	if err := gitx.EnsureMirror(ctx, repo.RemoteURL, mirror); err != nil {
		return "", err
	}
	return mirror, nil
}

// ListRepoBranches fetches the repo mirror and returns available branch names.
func (r *Runner) ListRepoBranches(ctx context.Context, repoID int64) ([]string, error) {
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	mirror, err := r.ensureRepoMirror(ctx, *repo)
	if err != nil {
		return nil, err
	}
	return gitx.ListBranches(ctx, mirror)
}

// ListRepoCommits fetches the repo mirror and returns recent commits on branch.
func (r *Runner) ListRepoCommits(ctx context.Context, repoID int64, branch string, limit int) ([]gitx.CommitInfo, error) {
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	mirror, err := r.ensureRepoMirror(ctx, *repo)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(branch) == "" {
		branch = repo.DefaultBranch
	}
	return gitx.ListCommits(ctx, mirror, branch, limit)
}

// AddBranchWatch registers a branch to be monitored for new pushes.
func (r *Runner) AddBranchWatch(ctx context.Context, projectID, repoID int64, branch string) (*domain.BranchWatch, error) {
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	if projectID > 0 && repo.ProjectID != projectID {
		return nil, fmt.Errorf("仓库不属于当前项目")
	}
	branch = strings.TrimSpace(strings.TrimPrefix(branch, "origin/"))
	if branch == "" {
		branch = repo.DefaultBranch
	}
	mirror, err := r.ensureRepoMirror(ctx, *repo)
	if err != nil {
		return nil, err
	}
	tip, err := gitx.BranchTip(ctx, mirror, branch)
	if err != nil {
		return nil, err
	}
	return r.st.UpsertBranchWatch(ctx, repo.ProjectID, repoID, branch, tip.SHA, tip.Subject, tip.Author)
}

// startWatcherLoop periodically checks watched branches and open PR statuses.
func (r *Runner) startWatcherLoop() {
	ticker := time.NewTicker(90 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		_, _, _ = r.CheckWatchesAndPRs(ctx, 0)
		cancel()
	}
}

// CheckWatchesAndPRs checks branch watches and open PRs (for projectID, or all projects if projectID == 0).
func (r *Runner) CheckWatchesAndPRs(ctx context.Context, projectID int64) (watchesTriggered int, prsUpdated int, err error) {
	r.watchMu.Lock()
	defer r.watchMu.Unlock()

	var watches []domain.BranchWatch
	if projectID > 0 {
		watches, err = r.st.ListBranchWatches(ctx, projectID)
	} else {
		watches, err = r.st.ListAllBranchWatches(ctx)
	}
	if err != nil {
		return 0, 0, err
	}

	// Group watches by repoID so we only fetch each mirror once.
	fetchedRepo := map[int64]string{}
	for _, w := range watches {
		mirror, ok := fetchedRepo[w.RepoID]
		if !ok {
			repo, rerr := r.st.GetRepo(ctx, w.RepoID)
			if rerr != nil {
				continue
			}
			m, merr := r.ensureRepoMirror(ctx, *repo)
			if merr != nil {
				continue
			}
			mirror = m
			fetchedRepo[w.RepoID] = mirror
		}
		tip, terr := gitx.BranchTip(ctx, mirror, w.Branch)
		if terr != nil || tip.SHA == "" {
			continue
		}
		if w.LastSHA == "" {
			_, _ = r.st.UpdateBranchWatchTip(ctx, w.ID, tip.SHA, tip.Subject, tip.Author)
			continue
		}
		if tip.SHA != w.LastSHA {
			commits, _ := gitx.ListCommitsRange(ctx, mirror, w.LastSHA, tip.SHA, 10)
			if len(commits) == 0 {
				commits = []gitx.CommitInfo{tip}
			}
			_, _ = r.st.UpdateBranchWatchTip(ctx, w.ID, tip.SHA, tip.Subject, tip.Author)
			watchesTriggered++

			var lines []string
			var shas []string
			// Reverse commits so oldest is first for cherry-picking
			for i := len(commits) - 1; i >= 0; i-- {
				shas = append(shas, commits[i].SHA)
			}
			for _, c := range commits {
				lines = append(lines, fmt.Sprintf("• %s %s (%s)", c.ShortSHA, c.Subject, c.Author))
			}
			metaBytes, _ := json.Marshal(map[string]any{
				"projectId": w.ProjectID,
				"repoId":    w.RepoID,
				"repoName":  w.RepoName,
				"branch":    w.Branch,
				"shas":      shas,
			})
			title := fmt.Sprintf("分支推送：%s:%s (%d 个新提交)", w.RepoName, w.Branch, len(commits))
			r.pushNotification("branch_push", title, strings.Join(lines, "\n"), fmt.Sprintf("/projects/%d?watch=%d", w.ProjectID, w.ID), string(metaBytes))
		}
	}

	// Check open PRs
	openRuns, err := r.st.ListOpenPRRuns(ctx)
	if err == nil {
		for _, run := range openRuns {
			issue, ierr := r.st.GetIssue(ctx, run.IssueID)
			if ierr != nil {
				continue
			}
			if projectID > 0 && issue.ProjectID != projectID {
				continue
			}
			st, prTitle, perr := provider.CheckPRStatus(ctx, run.PRURL)
			if perr != nil || st == "" || st == run.PRState {
				continue
			}
			// If it was "" and is still "open", just normalize without noisy notification.
			if run.PRState == "" && st == "open" {
				_, _ = r.st.UpdateRun(ctx, run.ID, store.RunUpdate{PRState: &st})
				continue
			}
			if u, uerr := r.st.UpdateRun(ctx, run.ID, store.RunUpdate{PRState: &st}); uerr == nil {
				r.notify.BroadcastRunUpdated(u)
			}
			prsUpdated++
			if st == "merged" {
				done := domain.IssueDone
				if u, uerr := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &done}); uerr == nil {
					r.notify.BroadcastIssueUpdated(u)
				}
				_, _ = r.st.AddComment(ctx, issue.ID, "system", fmt.Sprintf("🎉 PR 已合并 (Merged): %s", run.PRURL))
				r.ExportIssueMarkdown(ctx, issue.ID)
				r.pushNotification("pr_status", fmt.Sprintf("🎉 [%s] PR 已合并 (Merged)", issue.Key), fmt.Sprintf("%s\n%s", prTitle, run.PRURL), fmt.Sprintf("/issues/%d", issue.ID), "")
			} else if st == "closed" {
				if u, uerr := r.st.GetIssue(ctx, issue.ID); uerr == nil {
					r.notify.BroadcastIssueUpdated(u)
				}
				_, _ = r.st.AddComment(ctx, issue.ID, "system", fmt.Sprintf("⚠️ PR 已关闭 (Closed): %s", run.PRURL))
				r.ExportIssueMarkdown(ctx, issue.ID)
				r.pushNotification("pr_status", fmt.Sprintf("⚠️ [%s] PR 已关闭 (Closed)", issue.Key), fmt.Sprintf("%s\n%s", prTitle, run.PRURL), fmt.Sprintf("/issues/%d", issue.ID), "")
			}
		}
	}

	return watchesTriggered, prsUpdated, nil
}

// CherryPickRequest cherry-picks selected commits onto a target branch, pushes a new branch,
// optionally creates a PR, and generates the standardized ChangeRecord.
type CherryPickRequest struct {
	ProjectID    int64    `json:"projectId"`
	RepoID       int64    `json:"repoId"`
	IssueID      int64    `json:"issueId"`      // optional; if 0, a new local issue is created
	SourceBranch string   `json:"sourceBranch"` // informational source branch name
	TargetBranch string   `json:"targetBranch"` // base branch to cherry-pick onto (e.g. main)
	NewBranch    string   `json:"newBranch"`    // optional custom branch name
	SHAs         []string `json:"shas"`         // commit SHAs in order to cherry-pick
	Mode         string   `json:"mode"`         // "pr" | "push"
	Title        string   `json:"title"`
	ChangeItems  []string `json:"changeItems"`
	Remark       string   `json:"remark"`
}

type CherryPickResult struct {
	IssueID      int64  `json:"issueId"`
	IssueKey     string `json:"issueKey"`
	RunID        int64  `json:"runId"`
	Branch       string `json:"branch"`
	TargetBranch string `json:"targetBranch"`
	Commit       string `json:"commit"`
	PRURL        string `json:"prUrl"`
	ChangeRecord string `json:"changeRecord"`
}

func (r *Runner) CherryPick(ctx context.Context, req CherryPickRequest) (*CherryPickResult, error) {
	if len(req.SHAs) == 0 {
		return nil, fmt.Errorf("请至少选择一个要 Cherry-Pick 的 Commit")
	}
	repo, err := r.st.GetRepo(ctx, req.RepoID)
	if err != nil {
		return nil, err
	}
	targetBranch := strings.TrimSpace(req.TargetBranch)
	if targetBranch == "" {
		targetBranch = repo.DefaultBranch
	}
	if targetBranch == "" {
		targetBranch = "main"
	}
	newBranch := strings.TrimSpace(req.NewBranch)
	if newBranch == "" {
		newBranch = fmt.Sprintf("cp/%s-%d", sanitizeBranch(targetBranch), time.Now().Unix())
	}

	mirror, err := r.ensureRepoMirror(ctx, *repo)
	if err != nil {
		return nil, err
	}
	tempDir := filepath.Join(r.cfg.RunsDir(), fmt.Sprintf("cp-%d", time.Now().UnixNano()))
	defer os.RemoveAll(tempDir)

	headSHA, subjects, err := gitx.CherryPickAndPush(ctx, mirror, tempDir, newBranch, targetBranch, req.SHAs)
	if err != nil {
		return nil, err
	}

	items := cleanChangeItems(req.ChangeItems)
	if len(items) == 0 {
		items = subjects
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		if len(items) > 0 {
			title = fmt.Sprintf("chore(%s): %s", targetBranch, items[0])
		} else {
			title = fmt.Sprintf("Cherry-pick %d commits to %s", len(req.SHAs), targetBranch)
		}
	}

	// Ensure or create an Issue to track this PR / change record on the Kanban board.
	var issue *domain.Issue
	if req.IssueID > 0 {
		issue, err = r.st.GetIssue(ctx, req.IssueID)
		if err != nil {
			return nil, err
		}
	} else {
		issue, err = r.st.CreateLocalIssue(ctx, repo.ProjectID, title, fmt.Sprintf("Cherry-pick `%s` -> `%s` (%d commits)", req.SourceBranch, targetBranch, len(req.SHAs)), []string{"cherry-pick"})
		if err != nil {
			return nil, err
		}
	}

	var prURL string
	if req.Mode == "pr" || req.Mode == "" {
		p := provider.For(repo.Provider)
		if p.Caps().PRs {
			preRecord := FormatChangeRecord(items, newBranch, targetBranch, "(创建中)", req.Remark)
			url, perr := p.CreatePR(ctx, *repo, newBranch, targetBranch, title, preRecord)
			if perr != nil {
				return nil, fmt.Errorf("分支 %s 已推送，但创建 PR 失败: %w", newBranch, perr)
			}
			prURL = url
		}
	}

	record := FormatChangeRecord(items, newBranch, targetBranch, prURL, req.Remark)

	// Record a synthetic submitted Run so PR status tracking works identically.
	run, err := r.st.CreateRun(ctx, issue.ID, "cherry-pick", "")
	if err != nil {
		return nil, err
	}
	st := domain.RunSubmitted
	prState := ""
	if prURL != "" {
		prState = "open"
	}
	run, _ = r.st.UpdateRun(ctx, run.ID, store.RunUpdate{
		Status:       &st,
		Branch:       &newBranch,
		TargetBranch: &targetBranch,
		Summary:      &record,
		PRURL:        &prURL,
		PRState:      &prState,
		ChangeRecord: &record,
		Started:      true,
		Ended:        true,
	})
	if run != nil {
		r.notify.BroadcastRunUpdated(run)
	}

	ist := domain.IssueReview
	if prURL != "" {
		ist = domain.IssuePROpen
	}
	if u, uerr := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &ist}); uerr == nil {
		issue = u
		r.notify.BroadcastIssueUpdated(u)
	}
	_, _ = r.st.AddComment(ctx, issue.ID, "system", "```text\n"+record+"\n```")
	r.ExportIssueMarkdown(ctx, issue.ID)

	if prURL != "" {
		r.pushNotification("pr_status", fmt.Sprintf("🍒 [%s] Cherry-Pick PR 已创建", issue.Key), prURL, fmt.Sprintf("/issues/%d", issue.ID), "")
	}

	return &CherryPickResult{
		IssueID:      issue.ID,
		IssueKey:     issue.Key,
		RunID:        run.ID,
		Branch:       newBranch,
		TargetBranch: targetBranch,
		Commit:       headSHA,
		PRURL:        prURL,
		ChangeRecord: record,
	}, nil
}

// BatchRunTodoIssues starts runs for all backlog/todo issues in a project that don't have an active run.
func (r *Runner) BatchRunTodoIssues(ctx context.Context, projectID int64, agentName, modelName string) ([]*domain.Run, error) {
	if strings.TrimSpace(agentName) == "" {
		for _, a := range r.ListAgents() {
			if a.Available {
				agentName = a.Name
				break
			}
		}
	}
	if agentName == "" {
		return nil, fmt.Errorf("未找到可用的 Agent")
	}
	issues, err := r.st.ListIssues(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var started []*domain.Run
	for _, iss := range issues {
		if iss.Status != domain.IssueTodo && iss.Status != domain.IssueBacklog {
			continue
		}
		run, err := r.StartRun(ctx, iss.ID, agentName, modelName, "")
		if err != nil {
			continue
		}
		started = append(started, run)
	}
	return started, nil
}
