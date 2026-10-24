package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/automation"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/store"
)

// FormatChangeRecord formats the standardized change record requested by the user:
// 本次改动：
//
//	a. ...
//	b. ...
//
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

// PushNotification persists and broadcasts a user-visible notification.
func (r *Runner) PushNotification(kind, title, body, link, meta string) {
	r.pushNotification(kind, title, body, link, meta)
}

func (r *Runner) pushNotification(kind, title, body, link, meta string) {
	n, err := r.st.CreateNotification(context.Background(), kind, title, body, link, meta)
	if err == nil && n != nil {
		r.notify.BroadcastNotification(n)
	}
	r.sendExternalNotification(kind, title, body, link)
}

func (r *Runner) SendTestNotification(ctx context.Context) (map[string]any, error) {
	title := "🔔 xIssue 监控通知测试"
	body := "这是一条来自 xIssue 监控系统的测试通知，表明通知链路已正常连通！"
	link := "/projects"
	r.sendExternalNotification("test", title, body, link)
	return map[string]any{
		"ok":      true,
		"message": "已成功派发测试通知至外部平台（若已配置 Telegram 或 Webhook）",
	}, nil
}

func (r *Runner) sendExternalNotification(kind, title, body, link string) {
	mon := r.cfg.MonitorSnapshot()
	link = r.cfg.AbsoluteURL(link)
	tgToken := strings.TrimSpace(mon.TelegramBotToken)
	tgChatID := strings.TrimSpace(mon.TelegramChatID)
	webhookURL := strings.TrimSpace(mon.WebhookURL)
	allowTelegram := notificationRouteAllows(mon.NotificationRoutes, kind, "telegram")
	allowWebhook := notificationRouteAllows(mon.NotificationRoutes, kind, "webhook")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// 1. Telegram Bot 推送
		if allowTelegram && tgToken != "" && tgChatID != "" {
			tgMsg := fmt.Sprintf("🔔 *%s*\n\n%s", escapeTelegramMarkdown(title), escapeTelegramMarkdown(body))
			if link != "" {
				tgMsg += fmt.Sprintf("\n\n🔗 链接: %s", link)
			}
			payload, _ := json.Marshal(map[string]any{
				"chat_id":    tgChatID,
				"text":       tgMsg,
				"parse_mode": "Markdown",
			})
			tgURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tgToken)
			req, err := http.NewRequestWithContext(ctx, "POST", tgURL, bytes.NewReader(payload))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
				resp, rerr := http.DefaultClient.Do(req)
				if rerr == nil {
					resp.Body.Close()
					r.LogMonitor("EVENT", "notify", fmt.Sprintf("已成功通过 Telegram Bot 推送通知: %s", title))
				} else {
					r.LogMonitor("WARN", "notify", fmt.Sprintf("Telegram Bot 推送失败: %v", rerr))
				}
			}
		}

		// 2. 通用 Webhook 推送 (飞书/企微/钉钉/Slack/自定义)
		if allowWebhook && webhookURL != "" {
			payload, _ := json.Marshal(map[string]any{
				"event":     kind,
				"title":     title,
				"body":      body,
				"link":      link,
				"timestamp": time.Now().Format(time.RFC3339),
			})
			req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(payload))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
				resp, rerr := http.DefaultClient.Do(req)
				if rerr == nil {
					resp.Body.Close()
					r.LogMonitor("EVENT", "notify", fmt.Sprintf("已成功通过 Webhook 推送通知: %s", title))
				} else {
					r.LogMonitor("WARN", "notify", fmt.Sprintf("Webhook 推送失败: %v", rerr))
				}
			}
		}

		// 3. 自定义通知扩展钩子与插件 (on-notify / notification_dispatch)
		if r.automation != nil {
			notifyCtx := &automation.NotificationEventContext{
				Kind:      kind,
				Title:     title,
				Body:      body,
				Link:      link,
				Timestamp: time.Now().Format(time.RFC3339),
			}
			payload := automation.PREventContext{
				Event:        "notification_dispatch",
				Notification: notifyCtx,
			}
			_, _ = r.automation.ExecutePlugins(ctx, payload)
		}
	}()
}

// notificationRouteAllows reports whether an event kind may use a channel.
// An empty route map keeps the historical behavior (all configured channels).
func notificationRouteAllows(routes map[string][]string, kind, channel string) bool {
	if len(routes) == 0 {
		return true
	}
	channels, ok := routes[kind]
	if !ok {
		return true
	}
	for _, ch := range channels {
		if ch == channel {
			return true
		}
	}
	return false
}

func escapeTelegramMarkdown(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`_`, `\_`,
		`*`, `\*`,
		"`", "\\`",
		`[`, `\[`,
	)
	return replacer.Replace(s)
}

func IsWithinActiveHours(activeHours string, t time.Time) bool {
	raw := strings.TrimSpace(activeHours)
	if raw == "" || raw == "0-24" || raw == "00:00-24:00" {
		return true
	}
	parts := strings.Split(raw, "-")
	if len(parts) != 2 {
		return true
	}
	parseHourMin := func(s string) int {
		s = strings.TrimSpace(s)
		if strings.Contains(s, ":") {
			p := strings.Split(s, ":")
			h, _ := strconv.Atoi(p[0])
			m, _ := strconv.Atoi(p[1])
			return h*60 + m
		}
		h, _ := strconv.Atoi(s)
		return h * 60
	}
	startMin := parseHourMin(parts[0])
	endMin := parseHourMin(parts[1])
	nowMin := t.Hour()*60 + t.Minute()

	if startMin <= endMin {
		return nowMin >= startMin && nowMin < endMin
	}
	return nowMin >= startMin || nowMin < endMin
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

// ensureRepoMirrorCached creates the mirror if needed but does not fetch all
// branches; callers fetch exactly the refs they need.
func (r *Runner) ensureRepoMirrorCached(ctx context.Context, repo domain.Repo) (string, error) {
	mirror := r.mirrorPath(repo.ID)
	if err := gitx.EnsureMirrorCached(ctx, repo.RemoteURL, mirror); err != nil {
		return "", err
	}
	return mirror, nil
}

// ListRepoBranches returns branch names from the mirror. refresh forces a fetch.
func (r *Runner) ListRepoBranches(ctx context.Context, repoID int64, refresh bool) ([]string, error) {
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	mirror := r.mirrorPath(repo.ID)
	if refresh {
		mirror, err = r.ensureRepoMirror(ctx, *repo)
	} else {
		err = gitx.EnsureMirrorCached(ctx, repo.RemoteURL, mirror)
	}
	if err != nil {
		return nil, err
	}
	return gitx.ListBranches(ctx, mirror)
}

// ListRepoCommits returns recent commits on branch. Unless refresh is true it
// reads the existing mirror to keep UI navigation fast.
func (r *Runner) ListRepoCommits(ctx context.Context, repoID int64, branch string, limit int, refresh bool) ([]gitx.CommitInfo, error) {
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	mirror := r.mirrorPath(repo.ID)
	if refresh {
		mirror, err = r.ensureRepoMirror(ctx, *repo)
	} else {
		err = gitx.EnsureMirrorCached(ctx, repo.RemoteURL, mirror)
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(branch) == "" {
		branch = repo.DefaultBranch
	}
	return gitx.ListCommits(ctx, mirror, branch, limit)
}

// GetRepoCommitDetail fetches the repo mirror and returns one commit with files and patch.
func (r *Runner) GetRepoCommitDetail(ctx context.Context, repoID int64, sha string) (gitx.CommitDetail, error) {
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return gitx.CommitDetail{}, err
	}
	mirror := r.mirrorPath(repo.ID)
	if err := gitx.EnsureMirrorCached(ctx, repo.RemoteURL, mirror); err != nil {
		return gitx.CommitDetail{}, err
	}
	detail, err := gitx.GetCommitDetail(ctx, mirror, sha)
	if err == nil {
		return detail, nil
	}
	// The commit may be newer than the cached mirror; fetch once and retry.
	mirror, fetchErr := r.ensureRepoMirror(ctx, *repo)
	if fetchErr != nil {
		return gitx.CommitDetail{}, err
	}
	return gitx.GetCommitDetail(ctx, mirror, sha)
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
	r.LogMonitor("INFO", "watcher", "监控服务已启动，轮询间隔: 90秒 (保留最近8小时监控与执行日志)")
	ticker := time.NewTicker(90 * time.Second)
	pruneTicker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	defer pruneTicker.Stop()

	// 启动时立即执行一次检查与裁剪
	go func() {
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
		if mon := r.cfg.MonitorSnapshot(); mon.Enabled && IsWithinActiveHours(mon.ActiveHours, time.Now()) {
			ctx, cancel := context.WithTimeout(r.ctx, 60*time.Second)
			defer cancel()
			_, _, _ = r.CheckWatchesAndPRs(ctx, 0)
		}
		_, _ = r.PruneMonitorLogs(8 * time.Hour)
	}()

	for {
		select {
		case <-r.ctx.Done():
			r.LogMonitor("INFO", "watcher", "监控服务收到停止信号，已正常退出")
			return
		case <-ticker.C:
			mon := r.cfg.MonitorSnapshot()
			if !mon.Enabled {
				r.LogMonitor("INFO", "watcher", "监控当前处于暂停状态，已跳过轮询")
				continue
			}
			if !IsWithinActiveHours(mon.ActiveHours, time.Now()) {
				r.LogMonitor("INFO", "watcher", fmt.Sprintf("当前处于非活跃监控时段 (%s)，已跳过轮询以节省资源", mon.ActiveHours))
				continue
			}
			ctx, cancel := context.WithTimeout(r.ctx, 60*time.Second)
			_, _, _ = r.CheckWatchesAndPRs(ctx, 0)
			cancel()
		case <-pruneTicker.C:
			_, _ = r.PruneMonitorLogs(8 * time.Hour)
		}
	}
}

// CheckWatchesAndPRs checks branch watches and open PRs (for projectID, or all projects if projectID == 0).
func (r *Runner) CheckWatchesAndPRs(ctx context.Context, projectID int64) (watchesTriggered int, prsUpdated int, err error) {
	start := time.Now()
	r.watchMu.Lock()
	defer r.watchMu.Unlock()

	var watches []domain.BranchWatch
	if projectID > 0 {
		watches, err = r.st.ListBranchWatches(ctx, projectID)
	} else {
		watches, err = r.st.ListAllBranchWatches(ctx)
	}
	if err != nil {
		r.LogMonitor("ERROR", "watcher", fmt.Sprintf("获取监控分支失败: %v", err))
		return 0, 0, err
	}

	// Group watches by repoID so we fetch each mirror once, and only fetch the
	// branches that are actually watched instead of every remote branch.
	watchedBranches := map[int64][]string{}
	for _, w := range watches {
		watchedBranches[w.RepoID] = append(watchedBranches[w.RepoID], w.Branch)
	}
	fetchedRepo := map[int64]string{}
	for _, w := range watches {
		mirror, ok := fetchedRepo[w.RepoID]
		if !ok {
			repo, rerr := r.st.GetRepo(ctx, w.RepoID)
			if rerr != nil {
				continue
			}
			m, merr := r.ensureRepoMirrorCached(ctx, *repo)
			if merr != nil {
				continue
			}
			fetchBranches(ctx, m, validPullBranches(watchedBranches[w.RepoID]))
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
			r.pushNotification("branch_push", title, strings.Join(lines, "\n"), branchPushLink(w.ProjectID, w.RepoID, w.Branch, tip.SHA), string(metaBytes))

			// 触发自动化引擎 (Layer 1 规则、Layer 2 on-branch-push Hook、Layer 3 插件)
			repo, _ := r.st.GetRepo(ctx, w.RepoID)
			_ = r.processBranchPushAutomation(ctx, w, repo, commits, tip)
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

			// 尝试拉取 PR 详细信息（含 comments 与 reviews）
			prDetail, perr := provider.GetPRDetails(ctx, run.PRURL)
			if perr != nil || prDetail == nil {
				st, prTitle, err2 := provider.CheckPRStatus(ctx, run.PRURL)
				if err2 != nil || st == "" {
					continue
				}
				prDetail = &provider.PRDetailInfo{
					State: st,
					Title: prTitle,
					URL:   run.PRURL,
				}
			}

			// 如果 PR 当前还是 open 状态，触发自动化引擎（Layer 1 规则、Layer 2 项目 Hooks、Layer 3 插件）
			if prDetail.State == "open" {
				merged, aerr := r.processPRAutomation(ctx, issue, run, prDetail)
				if aerr == nil && merged {
					prDetail.State = "merged"
					prsUpdated++
				}
			}

			st := prDetail.State
			prTitle := prDetail.Title
			if st == "" || st == run.PRState {
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
				_ = r.processPRMergedAutomation(ctx, issue, run, prDetail)
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

	duration := time.Since(start)
	r.monitorMu.Lock()
	r.lastCheckTime = start
	r.nextCheckTime = start.Add(90 * time.Second)
	r.lastDurationMs = duration.Milliseconds()
	r.lastTrigWatches = watchesTriggered
	r.lastUpdatedPRs = prsUpdated
	r.monitorMu.Unlock()

	if watchesTriggered > 0 || prsUpdated > 0 {
		r.LogMonitor("EVENT", "watcher", fmt.Sprintf("检查完成: 触发 %d 个分支推送通知, 更新 %d 个 PR 状态 (耗时 %dms)", watchesTriggered, prsUpdated, duration.Milliseconds()))
	} else {
		r.LogMonitor("INFO", "watcher", fmt.Sprintf("检查完成: %d 个监控分支正常, 无新变更 (耗时 %dms)", len(watches), duration.Milliseconds()))
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
		agentName = r.AvailableAgentForRole(AgentRoleIssueDevelop)
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

// ListProjectPRs lists PRs across all repos of the given project.
func (r *Runner) ListProjectPRs(ctx context.Context, projectID int64, state string) ([]provider.RemotePR, error) {
	repos, err := r.st.ListRepos(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var all []provider.RemotePR
	for _, repo := range repos {
		p := provider.For(repo.Provider)
		if !p.Caps().PRs {
			continue
		}
		prs, err := p.ListPRs(ctx, repo, state)
		if err != nil {
			continue
		}
		all = append(all, prs...)
	}
	return all, nil
}

// PullProjectRepos pulls the latest remote commits into bare mirrors and into local repo directories.
func (r *Runner) PullProjectRepos(ctx context.Context, projectID int64) ([]string, error) {
	project, err := r.st.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	repos := project.Repos
	pullBranches := validPullBranches(project.PullBranches)
	var updated []string
	for _, repo := range repos {
		// 1. Update mirror. When pullBranches is configured, only fetch those refs
		// so stale/merged feature branches are not dragged back on every pull.
		mirror := filepath.Join(r.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repo.ID))
		if _, statErr := os.Stat(mirror); statErr == nil {
			fetchBranches(ctx, mirror, pullBranches)
		}

		// 2. If it's a local directory repository, pull latest into its working tree if clean.
		// Only the currently checked-out branch can be fast-forwarded; other selected
		// branches are fetched into origin/<branch> without touching the worktree.
		if _, statErr := os.Stat(repo.RemoteURL); statErr == nil {
			branch, _ := gitx.Run(ctx, repo.RemoteURL, "rev-parse", "--abbrev-ref", "HEAD")
			target := repo.DefaultBranch
			if target == "" {
				target = "main"
			}
			allowed := len(pullBranches) == 0 || containsString(pullBranches, branch)
			st, _ := gitx.Run(ctx, repo.RemoteURL, "status", "--porcelain")
			if strings.TrimSpace(st) == "" && allowed && (branch == target || branch == "dev") {
				_, _ = gitx.Run(ctx, repo.RemoteURL, "pull", "--ff-only")
			} else {
				fetchBranches(ctx, repo.RemoteURL, pullBranches)
			}
		}
		updated = append(updated, repo.Name)
	}
	// Also trigger watch & PR status check to keep issues in sync
	_, _, _ = r.CheckWatchesAndPRs(ctx, projectID)
	return updated, nil
}

// validPullBranches drops unsafe branch names from project settings instead of
// passing them to git as options or refspecs.
func validPullBranches(branches []string) []string {
	out := make([]string, 0, len(branches))
	seen := map[string]bool{}
	for _, b := range branches {
		b = strings.TrimSpace(b)
		if b == "" || seen[b] || gitx.ValidateBranchName(b) != nil {
			continue
		}
		seen[b] = true
		out = append(out, b)
	}
	return out
}

func containsString(list []string, value string) bool {
	for _, s := range list {
		if s == value {
			return true
		}
	}
	return false
}

// fetchBranches fetches either all remote branches (empty selection) or exactly
// the selected branches. Each selected branch is fetched separately so one
// deleted/renamed branch does not abort the whole pull.
func fetchBranches(ctx context.Context, dir string, branches []string) {
	if len(branches) == 0 {
		_, _ = gitx.Run(ctx, dir, "fetch", "--prune", "origin")
		return
	}
	for _, b := range branches {
		_, _ = gitx.Run(ctx, dir, "fetch", "--prune", "origin", "+refs/heads/"+b+":refs/remotes/origin/"+b)
	}
}

// MergeProjectPR merges a PR in the given repository, and immediately pulls changes locally.
func (r *Runner) MergeProjectPR(ctx context.Context, projectID, repoID, number int64, method string) (string, error) {
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return "", err
	}
	p := provider.For(repo.Provider)
	if !p.Caps().PRs {
		return "", fmt.Errorf("仓库 %s 不支持 PR 合并操作", repo.Name)
	}
	msg, err := p.MergePR(ctx, *repo, number, method)
	if err != nil {
		return "", err
	}

	// Immediately pull latest code into mirrors and local directories
	_, _ = r.PullProjectRepos(ctx, projectID)

	// Check and update any linked runs and issues
	_, _, _ = r.CheckWatchesAndPRs(ctx, projectID)

	r.pushNotification("pr_status", fmt.Sprintf("[%s] PR #%d 已合并", repo.Name, number), fmt.Sprintf("合并方式：%s\n%s", method, msg), fmt.Sprintf("/projects/%d", projectID), "")
	return msg, nil
}

// CloseProjectPR closes a PR in the given repository.
func (r *Runner) CloseProjectPR(ctx context.Context, projectID, repoID, number int64) error {
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return err
	}
	p := provider.For(repo.Provider)
	if !p.Caps().PRs {
		return fmt.Errorf("仓库 %s 不支持 PR 操作", repo.Name)
	}
	if err := p.ClosePR(ctx, *repo, number); err != nil {
		return err
	}
	_, _, _ = r.CheckWatchesAndPRs(ctx, projectID)
	r.pushNotification("pr_status", fmt.Sprintf("[%s] PR #%d 已关闭", repo.Name, number), "", fmt.Sprintf("/projects/%d", projectID), "")
	return nil
}

// MonitorLogEntry represents a structured log event in the monitoring subsystem.
type MonitorLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`  // INFO, WARN, EVENT, ERROR
	Source    string    `json:"source"` // watcher, pr_sync, service, prune
	Message   string    `json:"message"`
}

// MonitorStatus represents current runtime status of the monitoring daemon.
type MonitorStatus struct {
	Running              bool                 `json:"running"`
	Enabled              bool                 `json:"enabled"`
	ActiveHours          string               `json:"activeHours"`
	TelegramConfigured   bool                 `json:"telegramConfigured"`
	WebhookConfigured    bool                 `json:"webhookConfigured"`
	IntervalSeconds      int                  `json:"intervalSeconds"`
	LastCheckTime        *time.Time           `json:"lastCheckTime,omitempty"`
	NextCheckTime        *time.Time           `json:"nextCheckTime,omitempty"`
	LastDurationMs       int64                `json:"lastDurationMs"`
	WatchesCount         int                  `json:"watchesCount"`
	OpenPRsCount         int                  `json:"openPRsCount"`
	WatchedBranches      []domain.BranchWatch `json:"watchedBranches"`
	LastTriggeredWatches int                  `json:"lastTriggeredWatches"`
	LastUpdatedPRs       int                  `json:"lastUpdatedPRs"`
	IssueSyncEnabled     bool                 `json:"issueSyncEnabled"`
	IssueSyncIntervalMin int                  `json:"issueSyncIntervalMin"`
	LastIssueSyncTime    *time.Time           `json:"lastIssueSyncTime,omitempty"`
	LastIssueSyncCount   int                  `json:"lastIssueSyncCount"`
	LastIssueSyncError   string               `json:"lastIssueSyncError,omitempty"`
}

// LogMonitor appends an event to the in-memory ring buffer and appends to ~/.xissue/monitor.log.
func (r *Runner) LogMonitor(level, source, message string) {
	entry := MonitorLogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Source:    source,
		Message:   message,
	}

	r.monitorMu.Lock()
	r.monitorLogs = append(r.monitorLogs, entry)
	// Retain only entries within the last 8 hours, max 2000 items
	cutoff := time.Now().Add(-8 * time.Hour)
	if len(r.monitorLogs) > 100 {
		var pruned []MonitorLogEntry
		for _, e := range r.monitorLogs {
			if e.Timestamp.After(cutoff) {
				pruned = append(pruned, e)
			}
		}
		if len(pruned) > 2000 {
			pruned = pruned[len(pruned)-2000:]
		}
		r.monitorLogs = pruned
	}
	r.monitorMu.Unlock()

	// Asynchronously append to file
	go func() {
		logFile := filepath.Join(r.cfg.DataDir, "monitor.log")
		line := fmt.Sprintf("[%s] [%s] [%s] %s\n", entry.Timestamp.Format("2006-01-02 15:04:05"), entry.Level, entry.Source, entry.Message)
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(line)
			_ = f.Close()
		}
	}()
}

// GetMonitorStatus returns current status and watched branches.
func (r *Runner) GetMonitorStatus(ctx context.Context) (*MonitorStatus, error) {
	watches, err := r.st.ListAllBranchWatches(ctx)
	if err != nil {
		watches = []domain.BranchWatch{}
	}
	openRuns, err := r.st.ListOpenPRRuns(ctx)
	openPRs := 0
	if err == nil {
		openPRs = len(openRuns)
	}

	r.monitorMu.RLock()
	defer r.monitorMu.RUnlock()

	mon := r.cfg.MonitorSnapshot()
	res := &MonitorStatus{
		Running:              mon.Enabled && IsWithinActiveHours(mon.ActiveHours, time.Now()),
		Enabled:              mon.Enabled,
		ActiveHours:          mon.ActiveHours,
		TelegramConfigured:   strings.TrimSpace(mon.TelegramBotToken) != "" && strings.TrimSpace(mon.TelegramChatID) != "",
		WebhookConfigured:    strings.TrimSpace(mon.WebhookURL) != "",
		IntervalSeconds:      90,
		LastDurationMs:       r.lastDurationMs,
		WatchesCount:         len(watches),
		OpenPRsCount:         openPRs,
		WatchedBranches:      watches,
		LastTriggeredWatches: r.lastTrigWatches,
		LastUpdatedPRs:       r.lastUpdatedPRs,
	}
	if !r.lastCheckTime.IsZero() {
		t := r.lastCheckTime
		res.LastCheckTime = &t
	}
	if !r.nextCheckTime.IsZero() {
		t := r.nextCheckTime
		res.NextCheckTime = &t
	}
	return res, nil
}

// GetMonitorLogs returns the recent monitor logs within the specified hours (default 8 hours).
func (r *Runner) GetMonitorLogs(ctx context.Context, hours int) []MonitorLogEntry {
	if hours <= 0 {
		hours = 8
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)

	r.monitorMu.RLock()
	defer r.monitorMu.RUnlock()

	var res []MonitorLogEntry
	for i := len(r.monitorLogs) - 1; i >= 0; i-- {
		e := r.monitorLogs[i]
		if e.Timestamp.After(cutoff) {
			res = append(res, e)
		}
	}
	return res
}

// PruneMonitorLogs removes logs older than maxAge (default 8 hours) from memory and disk.
func (r *Runner) PruneMonitorLogs(maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		maxAge = 8 * time.Hour
	}
	cutoff := time.Now().Add(-maxAge)

	r.monitorMu.Lock()
	origCount := len(r.monitorLogs)
	var kept []MonitorLogEntry
	for _, e := range r.monitorLogs {
		if e.Timestamp.After(cutoff) {
			kept = append(kept, e)
		}
	}
	r.monitorLogs = kept
	prunedCount := origCount - len(kept)
	r.monitorMu.Unlock()

	// Prune disk file monitor.log
	logFile := filepath.Join(r.cfg.DataDir, "monitor.log")
	if fi, err := os.Stat(logFile); err == nil && fi.Size() > 0 {
		content, err := os.ReadFile(logFile)
		if err == nil {
			lines := strings.Split(string(content), "\n")
			var keptLines []string
			cutoffPrefix := cutoff.Format("2006-01-02 15:04:05")
			for _, l := range lines {
				if len(l) > 21 && l[0] == '[' {
					ts := l[1:20]
					if ts >= cutoffPrefix {
						keptLines = append(keptLines, l)
					}
				} else if strings.TrimSpace(l) != "" {
					keptLines = append(keptLines, l)
				}
			}
			// If still over 5MB, retain only the last 3000 lines
			if len(keptLines) > 3000 {
				keptLines = keptLines[len(keptLines)-3000:]
			}
			_ = os.WriteFile(logFile, []byte(strings.Join(keptLines, "\n")), 0o644)
		}
	}

	r.LogMonitor("INFO", "prune", fmt.Sprintf("已执行日志裁剪 (保留最近 %v 内日志，已清理 %d 条旧内存日志)", maxAge, prunedCount))
	return prunedCount, nil
}

func (r *Runner) processPRAutomation(ctx context.Context, issue *domain.Issue, run domain.Run, prDetail *provider.PRDetailInfo) (bool, error) {
	if r.automation == nil || issue == nil || prDetail == nil || prDetail.State != "open" {
		return false, nil
	}

	project, err := r.st.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return false, err
	}

	var repo *domain.Repo
	if issue.RepoID != nil {
		repo, _ = r.st.GetRepo(ctx, *issue.RepoID)
	}

	// Prefer the repo's own local working tree. For remote repos the run worktree
	// is used, but hooks stay gated by isTrustedAuthor below.
	workdir := ""
	if repo != nil && gitx.IsLocalDir(repo.RemoteURL) {
		workdir = repo.RemoteURL
	}
	if workdir == "" {
		workdir = run.Workdir
	}

	merged := false

	// 1. 处理评论事件 (pr_comment)
	for _, comment := range prDetail.Comments {
		if strings.TrimSpace(comment.Body) == "" {
			continue
		}
		eventKey := fmt.Sprintf("comment:%s", comment.ID)
		if comment.ID == "" {
			eventKey = fmt.Sprintf("comment:%s:%s", comment.Author, comment.CreatedAt.Format(time.RFC3339))
		}

		handled, err := r.st.IsPREventHandled(ctx, run.PRURL, eventKey)
		if err == nil && handled {
			continue
		}

		payload := automation.PREventContext{
			Event:   "pr_comment",
			Project: *project,
			Repo:    repo,
			Issue:   issue,
			PR:      prDetail,
			Comment: &comment,
		}

		// Layer 1: Rules
		rules := r.effectiveAutomationRules(project)
		actions := r.automation.EvaluateRules(rules, "pr_comment", &comment, nil)

		// Layer 2: Project Hooks (安全门控: 仅受信任人员允许触发本地外部脚本执行，防止恶意外部评论 RCE)
		if isTrustedAuthor(comment.Author, prDetail.Author, rules) {
			if hookActions, herr := r.automation.ExecuteProjectHooks(ctx, workdir, payload); herr == nil {
				actions = append(actions, hookActions...)
			}
		} else {
			r.LogMonitor("WARN", "automation", fmt.Sprintf("评论作者 @%s 非信任人员，根据安全规则阻断 Layer 2 本地 Hooks 执行", comment.Author))
		}

		// Layer 3: Plugins
		if plugActions, perr := r.automation.ExecutePlugins(ctx, payload); perr == nil {
			actions = append(actions, plugActions...)
		}

		if len(actions) > 0 {
			r.LogMonitor("EVENT", "automation", fmt.Sprintf("触发 %d 个自动化动作 (PR #%d, 评论作者: @%s)", len(actions), prDetail.Number, comment.Author))
			trig := prEventTrigger{Event: "pr_comment", Label: "评论/Report", Author: comment.Author, Body: comment.Body, Rules: rules}
			mergedNow, aerr := r.executeActions(ctx, actions, project, repo, issue, run, prDetail, trig)
			if aerr == nil && mergedNow {
				merged = true
			}
			_ = r.st.RecordPREventHandled(ctx, run.PRURL, eventKey, fmt.Sprintf("%d actions executed", len(actions)))
			if merged {
				return true, nil
			}
		} else {
			_ = r.st.RecordPREventHandled(ctx, run.PRURL, eventKey, "no-op")
		}
	}

	// 2. 处理审核事件 (pr_review)
	for _, review := range prDetail.LatestReviews {
		eventKey := fmt.Sprintf("review:%s", review.ID)
		if review.ID == "" {
			eventKey = fmt.Sprintf("review:%s:%s", review.Author, review.SubmittedAt.Format(time.RFC3339))
		}

		handled, err := r.st.IsPREventHandled(ctx, run.PRURL, eventKey)
		if err == nil && handled {
			continue
		}

		payload := automation.PREventContext{
			Event:   "pr_review",
			Project: *project,
			Repo:    repo,
			Issue:   issue,
			PR:      prDetail,
			Review:  &review,
		}

		// Layer 1: Rules
		rules := r.effectiveAutomationRules(project)
		actions := r.automation.EvaluateRules(rules, "pr_review", nil, &review)

		// Layer 2: Hooks (安全门控)
		if isTrustedAuthor(review.Author, prDetail.Author, rules) {
			if hookActions, herr := r.automation.ExecuteProjectHooks(ctx, workdir, payload); herr == nil {
				actions = append(actions, hookActions...)
			}
		} else {
			r.LogMonitor("WARN", "automation", fmt.Sprintf("Review 作者 @%s 非信任人员，根据安全规则阻断 Layer 2 本地 Hooks 执行", review.Author))
		}

		// Layer 3: Plugins
		if plugActions, perr := r.automation.ExecutePlugins(ctx, payload); perr == nil {
			actions = append(actions, plugActions...)
		}

		if len(actions) > 0 {
			r.LogMonitor("EVENT", "automation", fmt.Sprintf("触发 %d 个自动化动作 (PR #%d, 审核人: @%s, 状态: %s)", len(actions), prDetail.Number, review.Author, review.State))
			trig := prEventTrigger{Event: "pr_review", Label: "审核/Review", Author: review.Author, State: review.State, Body: review.Body, Rules: rules}
			mergedNow, aerr := r.executeActions(ctx, actions, project, repo, issue, run, prDetail, trig)
			if aerr == nil && mergedNow {
				merged = true
			}
			_ = r.st.RecordPREventHandled(ctx, run.PRURL, eventKey, fmt.Sprintf("%d actions executed", len(actions)))
			if merged {
				return true, nil
			}
		} else {
			_ = r.st.RecordPREventHandled(ctx, run.PRURL, eventKey, "no-op")
		}
	}

	return merged, nil
}

// prEventTrigger describes the remote PR event that fired a set of actions.
type prEventTrigger struct {
	Event  string                  // pr_comment | pr_review | pr_merged
	Label  string                  // human-readable label used in logs/comments
	Author string                  // triggering login
	State  string                  // pr_review state (approved / changes_requested / ...)
	Body   string                  // comment or review body
	Rules  []domain.AutomationRule // effective automation rules (author trust gate)
}

func (r *Runner) executeActions(ctx context.Context, actions []domain.RuleAction, project *domain.Project, repo *domain.Repo, issue *domain.Issue, run domain.Run, prDetail *provider.PRDetailInfo, trig prEventTrigger) (bool, error) {
	merged := false
	for _, act := range actions {
		switch act.Type {
		case "merge_pr":
			if repo == nil || prDetail.Number <= 0 {
				continue
			}
			strategy := act.Strategy
			if strategy == "" {
				strategy = "squash"
			}
			// Auto-merge identity: rule-level act.Agent first, then the global
			// autoMerge role.
			merger := strings.TrimSpace(act.Agent)
			if merger == "" {
				merger = r.AgentForRole(AgentRoleAutoMerge)
			}
			r.LogMonitor("INFO", "automation", fmt.Sprintf("正在执行自动合并 PR #%d (策略: %s)...", prDetail.Number, strategy))
			pvd := provider.For(repo.Provider)
			out, err := pvd.MergePR(ctx, *repo, prDetail.Number, strategy)
			if err != nil {
				r.LogMonitor("ERROR", "automation", fmt.Sprintf("自动合并 PR #%d 失败: %v", prDetail.Number, err))
				continue
			}
			r.LogMonitor("EVENT", "automation", fmt.Sprintf("PR #%d 自动合并成功: %s", prDetail.Number, out))
			merged = true

			// 添加系统说明评论
			mergerNote := ""
			if merger != "" {
				mergerNote = fmt.Sprintf("（自动合并身份: %s）", merger)
			}
			_, _ = r.st.AddComment(ctx, issue.ID, "system", fmt.Sprintf("🤖 [自动化与Hook] 检测到 @%s 提交的 %s 满足要求，PR #%d 已自动合并！%s", trig.Author, trig.Label, prDetail.Number, mergerNote))
			r.pushNotification("pr_status", fmt.Sprintf("🤖 [%s] PR 已自动合并", issue.Key), fmt.Sprintf("检测到 @%s 的 %s 通过，已自动合并 PR #%d\n%s", trig.Author, trig.Label, prDetail.Number, prDetail.URL), fmt.Sprintf("/issues/%d", issue.ID), "")

		case "trigger_run":
			// Only PR feedback events may dispatch agents. Lifecycle events such
			// as pr_merged stay notify-only to avoid recursive runs.
			if trig.Event != "pr_comment" && trig.Event != "pr_review" {
				r.LogMonitor("WARN", "automation", fmt.Sprintf("事件 %s 不支持 trigger_run 动作，已忽略", trig.Event))
				continue
			}
			if issue == nil {
				continue
			}
			// Trust gate: only the PR author or explicitly trusted rule authors
			// may start an agent, so external commenters cannot inject prompts.
			if !isTrustedAuthor(trig.Author, prDetail.Author, trig.Rules) {
				r.LogMonitor("WARN", "automation", fmt.Sprintf("%s 作者 @%s 非信任人员，已阻断 trigger_run 调度", trig.Label, trig.Author))
				continue
			}
			agentName := strings.TrimSpace(act.Agent)
			if agentName == "" {
				agentName = r.AgentForRoleOrDefault(AgentRolePRReview)
			}
			if agentName == "" {
				r.LogMonitor("WARN", "automation", "trigger_run 无可用 Agent，已跳过")
				continue
			}
			prompt := strings.TrimSpace(act.Prompt)
			if prompt == "" {
				prompt = prReviewRunPrompt(trig, prDetail, issue)
			}
			r.LogMonitor("INFO", "automation", fmt.Sprintf("正在为 Issue %s 自动调度 PR 评审 Agent [%s]...", issue.Key, agentName))
			if _, rerr := r.StartRun(ctx, issue.ID, agentName, "", prompt); rerr != nil {
				r.LogMonitor("ERROR", "automation", fmt.Sprintf("调度 PR 评审 Agent 失败: %v", rerr))
			} else {
				r.LogMonitor("EVENT", "automation", fmt.Sprintf("已启动 Agent [%s] 处理 Issue %s 的 PR %s", agentName, issue.Key, trig.Label))
			}

		case "create_issue":
			if trig.Event != "pr_comment" && trig.Event != "pr_review" {
				r.LogMonitor("WARN", "automation", fmt.Sprintf("事件 %s 不支持 create_issue 动作，已忽略", trig.Event))
				continue
			}
			title := strings.TrimSpace(act.Prompt)
			if title == "" {
				title = fmt.Sprintf("跟进 PR #%d 的%s反馈", prDetail.Number, trig.Label)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "## PR 反馈跟进\n\nPR #%d (%s)\n\n触发者: @%s (%s", prDetail.Number, prDetail.URL, trig.Author, trig.Label)
			if trig.State != "" {
				fmt.Fprintf(&b, ", 状态: %s", trig.State)
			}
			b.WriteString(")\n\n")
			if body := strings.TrimSpace(trig.Body); body != "" {
				fmt.Fprintf(&b, "### 内容\n\n%s\n", body)
			}
			newIssue, err := r.st.CreateLocalIssue(ctx, project.ID, title, b.String(), []string{"pr-feedback", "auto"})
			if err != nil {
				r.LogMonitor("ERROR", "automation", fmt.Sprintf("自动化创建 Issue 失败: %v", err))
				continue
			}
			if repo != nil {
				_ = r.st.BindIssueRepo(ctx, newIssue.ID, repo.ID)
			}
			r.notify.BroadcastIssueUpdated(newIssue)
			r.LogMonitor("INFO", "automation", fmt.Sprintf("已为 PR #%d 的%s创建跟进 Issue %s", prDetail.Number, trig.Label, newIssue.Key))

		case "add_labels":
			if issue == nil || len(act.Labels) == 0 {
				continue
			}
			updated, err := r.st.AddIssueLabels(ctx, issue.ID, act.Labels...)
			if err != nil {
				r.LogMonitor("WARN", "automation", fmt.Sprintf("添加标签失败: %v", err))
				continue
			}
			r.notify.BroadcastIssueUpdated(updated)
			r.LogMonitor("INFO", "automation", fmt.Sprintf("已为 Issue %s 添加标签 %v", issue.Key, act.Labels))

		case "set_issue_status":
			targetStatus := domain.IssueStatus(act.Status)
			if targetStatus == "" {
				targetStatus = domain.IssueDone
			}
			if domain.ValidIssueStatus(targetStatus) {
				if u, err := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &targetStatus}); err == nil {
					r.notify.BroadcastIssueUpdated(u)
					r.LogMonitor("INFO", "automation", fmt.Sprintf("已自动更新 Issue %s 状态 -> %s", issue.Key, targetStatus))
				}
			}

		case "pull_local":
			if repo != nil {
				targetBranch := project.TargetBranch
				if targetBranch == "" {
					targetBranch = repo.DefaultBranch
				}
				if mirror, merr := r.ensureRepoMirrorCached(ctx, *repo); merr == nil {
					branches := validPullBranches(project.PullBranches)
					if len(branches) == 0 {
						branches = []string{targetBranch}
					}
					fetchBranches(ctx, mirror, branches)
				}
				r.LogMonitor("INFO", "automation", fmt.Sprintf("已自动刷新本地镜像与代码缓存 (%s:%s)", repo.Name, targetBranch))
			}

		case "notify":
			msg := act.Message
			if msg == "" {
				msg = fmt.Sprintf("自动化规则已针对 PR #%d 触发执行", prDetail.Number)
			}
			r.pushNotification("automation", fmt.Sprintf("⚡ [%s] 自动化规则触发", issue.Key), msg, fmt.Sprintf("/issues/%d", issue.ID), "")
		}
	}
	return merged, nil
}

// DetectProjectHooks lists available lifecycle hooks in a project.
func (r *Runner) DetectProjectHooks(ctx context.Context, projectID int64) []domain.HookStatus {
	repos, err := r.st.ListRepos(ctx, projectID)
	if err != nil || len(repos) == 0 {
		return automation.DetectHooks("")
	}
	// 优先在本地工作树目录检测
	for _, repo := range repos {
		if gitx.IsLocalDir(repo.RemoteURL) {
			hooks := automation.DetectHooks(repo.RemoteURL)
			for _, h := range hooks {
				if h.Exists && h.Executable {
					return hooks
				}
			}
		}
	}
	for _, repo := range repos {
		if gitx.IsLocalDir(repo.RemoteURL) {
			return automation.DetectHooks(repo.RemoteURL)
		}
	}
	mirror := r.mirrorPath(repos[0].ID)
	return automation.DetectHooks(mirror)
}

// prReviewRunPrompt builds the default prompt for a run triggered by PR
// comment/review feedback when the rule does not supply its own prompt.
func prReviewRunPrompt(trig prEventTrigger, prDetail *provider.PRDetailInfo, issue *domain.Issue) string {
	var b strings.Builder
	if trig.Event == "pr_review" {
		fmt.Fprintf(&b, "PR #%d (%s) received a review", prDetail.Number, prDetail.URL)
		if trig.State != "" {
			fmt.Fprintf(&b, " (%s)", trig.State)
		}
		fmt.Fprintf(&b, " from @%s", trig.Author)
	} else {
		fmt.Fprintf(&b, "PR #%d (%s) received a new comment from @%s", prDetail.Number, prDetail.URL, trig.Author)
	}
	b.WriteString(":\n\n")
	if body := strings.TrimSpace(trig.Body); body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "Please review this feedback in the context of issue %s, update the code on the current branch when changes are needed, and summarize your assessment. Do NOT merge or close the PR yourself.", issue.Key)
	return b.String()
}

func isTrustedAuthor(author, prAuthor string, rules []domain.AutomationRule) bool {
	author = strings.TrimSpace(author)
	if author == "" {
		return false
	}
	if prAuthor != "" && strings.EqualFold(author, prAuthor) {
		return true
	}
	for _, rule := range rules {
		if rule.Conditions.Author != "" {
			clean := strings.TrimPrefix(strings.TrimSpace(rule.Conditions.Author), "@")
			if clean != "" && strings.EqualFold(author, clean) {
				return true
			}
		}
	}
	return false
}

// effectiveAutomationRules merges global rules (optionally scoped to selected
// projects) with this project's own rules.
func (r *Runner) effectiveAutomationRules(project *domain.Project) []domain.AutomationRule {
	var global []domain.AutomationRule
	if r.cfg != nil {
		global = r.cfg.GlobalAutomationRulesSnapshot()
	}
	out := make([]domain.AutomationRule, 0, len(global)+len(project.AutomationRules))
	for _, rule := range global {
		if len(rule.ProjectIDs) == 0 {
			out = append(out, rule)
			continue
		}
		for _, id := range rule.ProjectIDs {
			if id == project.ID {
				out = append(out, rule)
				break
			}
		}
	}
	out = append(out, project.AutomationRules...)
	return out
}

// shouldRunProjectHooks decides whether repo-supplied Layer-2 scripts may run.
// Only locally-added repositories are eligible; for any other source the push
// author must be explicitly trusted by a configured automation rule.
func shouldRunProjectHooks(repo *domain.Repo, workdir, author string, rules []domain.AutomationRule) bool {
	if repo == nil || workdir == "" {
		return false
	}
	if gitx.IsLocalDir(repo.RemoteURL) {
		return true
	}
	return isTrustedAuthor(author, "", rules)
}

// ListPlugins returns installed ecosystem plugins.
func (r *Runner) ListPlugins() []domain.PluginInfo {
	if r.automation == nil {
		return nil
	}
	return r.automation.ListPlugins()
}

// branchPushLink deep-links a branch-push notification to the commit page and
// highlights the branch tip (or the newest commit in the push).
func branchPushLink(projectID, repoID int64, branch, sha string) string {
	return fmt.Sprintf("/projects/%d?commits=1&repo=%d&branch=%s&sha=%s",
		projectID, repoID, url.QueryEscape(branch), url.QueryEscape(sha))
}

func (r *Runner) processBranchPushAutomation(ctx context.Context, w domain.BranchWatch, repo *domain.Repo, commits []gitx.CommitInfo, tip gitx.CommitInfo) error {
	if r.automation == nil {
		return nil
	}

	project, err := r.st.GetProject(ctx, w.ProjectID)
	if err != nil || project == nil {
		return err
	}

	// Layer 2 scripts come from the repository working tree, so they may only run
	// for locally-added repositories. Never fall back to another repo's workdir:
	// that would execute project A's hooks for a push to project B.
	workdir := ""
	if repo != nil && gitx.IsLocalDir(repo.RemoteURL) {
		workdir = repo.RemoteURL
	}

	payload := automation.PREventContext{
		Event:   "branch_push",
		Project: *project,
		Repo:    repo,
		Branch:  w.Branch,
		TipSHA:  tip.SHA,
		Commits: commits,
	}

	// Layer 1: Rules
	rules := r.effectiveAutomationRules(project)
	actions := r.automation.EvaluateBranchPushRules(rules, w.Branch, tip.Author, tip.Subject)

	// Layer 2: Project Hooks (on-branch-push), gated to local working copies.
	if shouldRunProjectHooks(repo, workdir, tip.Author, rules) {
		if hookActions, herr := r.automation.ExecuteProjectHooks(ctx, workdir, payload); herr == nil {
			actions = append(actions, hookActions...)
		} else {
			r.LogMonitor("WARN", "automation", fmt.Sprintf("Layer 2 Hook 执行失败: %v", herr))
		}
	} else if workdir != "" {
		r.LogMonitor("WARN", "automation", fmt.Sprintf("推送作者 @%s 未匹配任何自动化规则，已阻断 Layer 2 本地 Hooks 执行", tip.Author))
	}

	// Layer 3: Plugins
	if plugActions, perr := r.automation.ExecutePlugins(ctx, payload); perr == nil {
		actions = append(actions, plugActions...)
	}

	if len(actions) == 0 {
		return nil
	}

	r.LogMonitor("EVENT", "automation", fmt.Sprintf("分支 %s:%s 推送触发了 %d 个自动化动作", w.RepoName, w.Branch, len(actions)))

	for _, act := range actions {
		switch act.Type {
		case "create_issue", "trigger_run":
			title := act.Prompt
			if title == "" {
				title = fmt.Sprintf("跟踪分支推送: %s:%s (%d 个新提交)", w.RepoName, w.Branch, len(commits))
			}
			var lines []string
			lines = append(lines, fmt.Sprintf("## 分支监控通知: `%s:%s`\n", w.RepoName, w.Branch))
			lines = append(lines, fmt.Sprintf("最新提交: `%s` by **%s**", tip.ShortSHA, tip.Author))
			lines = append(lines, fmt.Sprintf("提交说明: %s\n", tip.Subject))
			lines = append(lines, "### 包含的新提交列表:")
			for _, c := range commits {
				lines = append(lines, fmt.Sprintf("- `%s` %s (%s)", c.ShortSHA, c.Subject, c.Author))
			}
			body := strings.Join(lines, "\n")

			newIssue, err := r.st.CreateLocalIssue(ctx, project.ID, title, body, []string{"branch-monitor", "auto"})
			if err != nil {
				r.LogMonitor("ERROR", "automation", fmt.Sprintf("自动化创建 Issue 失败: %v", err))
				continue
			}
			if repo != nil {
				_ = r.st.BindIssueRepo(ctx, newIssue.ID, repo.ID)
			}
			r.notify.BroadcastIssueUpdated(newIssue)
			r.LogMonitor("INFO", "automation", fmt.Sprintf("已自动创建 Issue %s 跟踪分支推送", newIssue.Key))

			if act.Type == "trigger_run" {
				agentName := strings.TrimSpace(act.Agent)
				if agentName == "" {
					agentName = r.AgentForRoleOrDefault(AgentRoleIssueReview)
				}
				if agentName != "" {
					prompt := act.Prompt
					if prompt == "" {
						prompt = fmt.Sprintf("请审查分支 %s 上最新的代码提交并进行分析与测试。", w.Branch)
					}
					r.LogMonitor("INFO", "automation", fmt.Sprintf("正在为 Issue %s 自动调度 Agent [%s]...", newIssue.Key, agentName))
					if _, rerr := r.StartRun(ctx, newIssue.ID, agentName, "", prompt); rerr != nil {
						r.LogMonitor("ERROR", "automation", fmt.Sprintf("自动化调度 Agent 失败: %v", rerr))
					} else {
						r.LogMonitor("EVENT", "automation", fmt.Sprintf("已成功启动 Agent [%s] 针对 Issue %s 开始执行任务", agentName, newIssue.Key))
					}
				}
			}

		case "pull_local":
			if repo != nil {
				if mirror, merr := r.ensureRepoMirrorCached(ctx, *repo); merr == nil {
					branches := validPullBranches(project.PullBranches)
					if len(branches) == 0 {
						branches = []string{w.Branch}
					}
					fetchBranches(ctx, mirror, branches)
				}
				r.LogMonitor("INFO", "automation", fmt.Sprintf("已自动刷新本地镜像与代码缓存 (%s:%s)", repo.Name, w.Branch))
			}

		case "notify":
			msg := act.Message
			if msg == "" {
				msg = fmt.Sprintf("分支 %s:%s 收到 %d 个新提交", w.RepoName, w.Branch, len(commits))
			}
			r.pushNotification("automation", fmt.Sprintf("⚡ [%s] 分支推送自动化", w.RepoName), msg, branchPushLink(w.ProjectID, w.RepoID, w.Branch, tip.SHA), "")
		}
	}

	return nil
}

func (r *Runner) processPRMergedAutomation(ctx context.Context, issue *domain.Issue, run domain.Run, prDetail *provider.PRDetailInfo) error {
	if r.automation == nil || issue == nil || prDetail == nil {
		return nil
	}
	project, err := r.st.GetProject(ctx, issue.ProjectID)
	if err != nil || project == nil {
		return err
	}
	var repo *domain.Repo
	if issue.RepoID != nil {
		repo, _ = r.st.GetRepo(ctx, *issue.RepoID)
	}
	// Layer 2 scripts must come from a user-owned working copy. A run worktree is
	// cloned from the remote repository, so executing its hooks on an externally
	// merged PR would be remote code execution. Cross-repo fallbacks are forbidden.
	workdir := ""
	if repo != nil && gitx.IsLocalDir(repo.RemoteURL) {
		workdir = repo.RemoteURL
	}

	payload := automation.PREventContext{
		Event:   "pr_merged",
		Project: *project,
		Repo:    repo,
		Issue:   issue,
		PR:      prDetail,
	}

	// Layer 2: Hooks (on-pr-merged)
	var actions []domain.RuleAction
	if hookActions, herr := r.automation.ExecuteProjectHooks(ctx, workdir, payload); herr == nil {
		actions = append(actions, hookActions...)
	}

	// Layer 3: Plugins
	if plugActions, perr := r.automation.ExecutePlugins(ctx, payload); perr == nil {
		actions = append(actions, plugActions...)
	}

	// The PR is already merged; never run merge_pr again from this event.
	filtered := actions[:0]
	for _, act := range actions {
		if act.Type != "merge_pr" {
			filtered = append(filtered, act)
		} else {
			r.LogMonitor("WARN", "automation", "pr_merged 事件忽略 merge_pr 动作（PR 已合并）")
		}
	}
	if len(filtered) > 0 {
		r.LogMonitor("EVENT", "automation", fmt.Sprintf("PR #%d 合并事件触发 %d 个生命周期动作", prDetail.Number, len(filtered)))
		trig := prEventTrigger{Event: "pr_merged", Label: "PR 合并", Author: prDetail.Author}
		_, _ = r.executeActions(ctx, filtered, project, repo, issue, run, prDetail, trig)
	}

	r.LogMonitor("EVENT", "automation", fmt.Sprintf("PR #%d 合并事件已成功派发至生命周期 Hooks 与插件 (pr_merged)", prDetail.Number))
	return nil
}
