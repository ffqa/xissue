package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/runner"
)

func (s *Server) handleSyncProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	proj, err := s.st.GetProject(r.Context(), projectID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.refreshProjectRepos(r.Context(), proj)
	imported, err := s.syncProjectIssues(r.Context(), proj)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
}

// syncProjectIssues imports/refreshes remote issues for one project and
// triggers auto-develop only for newly created issues.
func (s *Server) syncProjectIssues(ctx context.Context, proj *domain.Project) (int, error) {
	if proj == nil {
		return 0, nil
	}
	imported := 0
	var errs []error
	for _, repo := range proj.Repos {
		p := provider.For(repo.Provider)
		if !p.Caps().Issues {
			continue
		}
		list, err := p.ListOpenIssues(ctx, repo)
		if err != nil {
			// One unavailable repository must not stop the other repositories
			// in the project from syncing.
			errs = append(errs, fmt.Errorf("%s: %w", repo.Name, err))
			continue
		}
		repoID := repo.ID
		for _, ri := range list {
			num := ri.Number
			created, err := s.st.UpsertRemoteIssue(ctx, domain.Issue{
				ProjectID:    proj.ID,
				Key:          provider.IssueKey(repo, ri.Number),
				RepoID:       &repoID,
				RemoteNumber: &num,
				RemoteURL:    ri.URL,
				Title:        ri.Title,
				Body:         ri.Body,
				Labels:       ri.Labels,
			})
			if err != nil {
				errs = append(errs, fmt.Errorf("%s issue #%d: %w", repo.Name, ri.Number, err))
				continue
			}
			if created {
				imported++
				s.handleNewRemoteIssue(ctx, proj, repo, ri)
			} else {
				s.triageExistingRemoteIssue(ctx, proj, repo, ri)
			}
		}
		s.syncRemoteIssueComments(ctx, proj, repo)
	}
	return imported, errors.Join(errs...)
}

// syncRemoteIssueComments imports comments for all issues of one repository in
// a single paginated GitHub API call, then upserts them by remote ID. Failures
// are non-fatal: issue syncing still succeeds even if comments are unavailable.
func (s *Server) syncRemoteIssueComments(ctx context.Context, proj *domain.Project, repo domain.Repo) {
	comments, err := provider.For(repo.Provider).ListRepoIssueComments(ctx, repo)
	if err != nil {
		s.runner.LogMonitor("WARN", "sync", fmt.Sprintf("%s 评论同步失败: %v", repo.Name, err))
		return
	}
	if len(comments) == 0 {
		return
	}
	issues, err := s.st.ListIssues(ctx, proj.ID)
	if err != nil {
		return
	}
	byNumber := make(map[int64]*domain.Issue, len(issues))
	for i := range issues {
		issue := &issues[i]
		if issue.RepoID == nil || *issue.RepoID != repo.ID || issue.RemoteNumber == nil {
			continue
		}
		byNumber[*issue.RemoteNumber] = issue
	}
	changed := map[int64]bool{}
	added, updated := 0, 0
	for _, c := range comments {
		issue := byNumber[c.IssueNumber]
		if issue == nil || strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Body) == "" {
			continue
		}
		author := strings.TrimSpace(c.Author)
		if author == "" {
			author = "ghost"
		}
		wasAdded, wasUpdated, err := s.st.UpsertRemoteComment(ctx, issue.ID, c.ID, author, c.Body, c.URL, c.CreatedAt)
		if err != nil {
			s.runner.LogMonitor("WARN", "sync", fmt.Sprintf("%s 评论写入失败 (%s): %v", issue.Key, c.ID, err))
			continue
		}
		if wasAdded {
			added++
		}
		if wasUpdated {
			updated++
		}
		if wasAdded || wasUpdated {
			changed[issue.ID] = true
		}
	}
	if added == 0 && updated == 0 {
		return
	}
	for issueID := range changed {
		if issue, err := s.st.GetIssue(ctx, issueID); err == nil {
			s.hub.BroadcastIssueUpdated(issue)
			s.runner.ExportIssueMarkdown(ctx, issueID)
		}
	}
	s.runner.LogMonitor("EVENT", "sync", fmt.Sprintf("%s 评论同步完成: 新增 %d，更新 %d", repo.Name, added, updated))
}

// handleNewRemoteIssue classifies a newly synced issue, applies a canonical
// label locally and remotely, and only then considers auto-development.
func (s *Server) handleNewRemoteIssue(ctx context.Context, proj *domain.Project, repo domain.Repo, ri provider.RemoteIssue) {
	issue, err := s.st.FindIssueByKey(ctx, proj.ID, provider.IssueKey(repo, ri.Number))
	if err != nil {
		return
	}
	triage := runner.ClassifyIssue(*issue)
	if triage.Kind == runner.IssueKindUnknown {
		s.runner.LogMonitor("INFO", "triage", fmt.Sprintf("%s 未识别为 Bug/需求/建议，跳过自动处理", issue.Key))
		return
	}
	if updated, err := s.st.AddIssueLabels(ctx, issue.ID, triage.Label); err == nil {
		issue = updated
		s.hub.BroadcastIssueUpdated(issue)
	}
	if err := provider.For(repo.Provider).AddLabels(ctx, repo, ri.Number, []string{triage.Label}); err != nil {
		s.runner.LogMonitor("WARN", "triage", fmt.Sprintf("%s 远程打标失败 (%s): %v", issue.Key, triage.Label, err))
	}
	s.runner.LogMonitor("EVENT", "triage", fmt.Sprintf("%s 判定为 %s (%s)", issue.Key, triage.Kind, triage.Reason))

	if !triage.Actionable {
		_, _ = s.st.AddComment(ctx, issue.ID, "system", "💡 已识别为建议，未自动开发；等待作者补充或维护者处理。")
		return
	}
	s.maybeAutoDevelop(ctx, proj, issue.Key, ri.Author, triage)
}

// triageExistingRemoteIssue backfills canonical labels for issues imported
// before triage existed. It never starts an agent run.
func (s *Server) triageExistingRemoteIssue(ctx context.Context, proj *domain.Project, repo domain.Repo, ri provider.RemoteIssue) {
	issue, err := s.st.FindIssueByKey(ctx, proj.ID, provider.IssueKey(repo, ri.Number))
	if err != nil || issue.Status == domain.IssueDone {
		return
	}
	triage := runner.ClassifyIssue(*issue)
	if triage.Kind == runner.IssueKindUnknown {
		return
	}
	hasCanonicalLabel := false
	for _, label := range issue.Labels {
		switch strings.ToLower(strings.TrimSpace(label)) {
		case "bug", "enhancement", "suggestion":
			hasCanonicalLabel = true
		}
	}
	if !hasCanonicalLabel {
		if updated, err := s.st.AddIssueLabels(ctx, issue.ID, triage.Label); err == nil {
			issue = updated
			s.hub.BroadcastIssueUpdated(issue)
		}
		if err := provider.For(repo.Provider).AddLabels(ctx, repo, ri.Number, []string{triage.Label}); err != nil {
			s.runner.LogMonitor("WARN", "triage", fmt.Sprintf("%s 远程补标失败 (%s): %v", issue.Key, triage.Label, err))
		}
		s.runner.LogMonitor("EVENT", "triage", fmt.Sprintf("%s 历史 Issue 补判为 %s (%s)", issue.Key, triage.Kind, triage.Reason))
	}
	// A previously imported issue may have been triaged before the classifier
	// understood its wording, or auto-develop may have been enabled after it
	// was labeled. If it has never been developed, give it one chance now.
	if runs, err := s.st.ListRuns(ctx, issue.ID); err == nil && len(runs) == 0 {
		s.maybeAutoDevelop(ctx, proj, issue.Key, ri.Author, triage)
	}
}

// syncAllProjectIssues imports issues for every project. It never aborts the
// whole run because one repository failed.
func (s *Server) syncAllProjectIssues(ctx context.Context) issueSyncResult {
	projects, err := s.st.ListProjects(ctx)
	if err != nil {
		return issueSyncResult{Errors: []string{err.Error()}}
	}
	res := issueSyncResult{Projects: len(projects)}
	for i := range projects {
		proj := &projects[i]
		imported, err := s.syncProjectIssues(ctx, proj)
		res.Imported += imported
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", proj.Name, err))
		}
	}
	return res
}

// runIssueSync serializes manual and background syncs and records the result
// for the monitor status API.
func (s *Server) runIssueSync(ctx context.Context) issueSyncResult {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	res := s.syncAllProjectIssues(ctx)
	s.lastIssueSync = time.Now()
	s.lastSyncImported = res.Imported
	s.lastSyncError = ""
	if len(res.Errors) > 0 {
		s.lastSyncError = strings.Join(res.Errors, "; ")
		s.runner.LogMonitor("WARN", "sync", fmt.Sprintf("Issue 同步完成但部分失败: 新增 %d，错误: %s", res.Imported, s.lastSyncError))
	} else {
		s.runner.LogMonitor("INFO", "sync", fmt.Sprintf("Issue 同步完成: %d 个项目，新增 %d 个 Issue", res.Projects, res.Imported))
	}
	return res
}

// triggerIssueSyncNow runs one sync in the background (e.g. right after the
// user enables automatic sync) without blocking the HTTP request.
func (s *Server) triggerIssueSyncNow() {
	mon := s.cfg.MonitorSnapshot()
	if !mon.Enabled || !mon.SyncIssues {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		s.runIssueSync(ctx)
	}()
}

func (s *Server) issueSyncLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
		}
		mon := s.cfg.MonitorSnapshot()
		if !mon.Enabled || !mon.SyncIssues {
			continue
		}
		interval := mon.SyncIntervalMin
		if interval <= 0 {
			interval = 5
		}
		s.syncMu.Lock()
		last := s.lastIssueSync
		s.syncMu.Unlock()
		if !last.IsZero() && time.Since(last) < time.Duration(interval)*time.Minute {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		s.runIssueSync(ctx)
		cancel()
	}
}

func (s *Server) handleSyncAllIssues(w http.ResponseWriter, r *http.Request) {
	res := s.runIssueSync(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       len(res.Errors) == 0,
		"projects": res.Projects,
		"imported": res.Imported,
		"errors":   res.Errors,
		"message":  fmt.Sprintf("已同步 %d 个项目，新增 %d 个 Issue", res.Projects, res.Imported),
	})
}

func (s *Server) maybeAutoDevelop(ctx context.Context, p *domain.Project, key, author string, triage runner.TriageResult) {
	if !triage.Actionable {
		return
	}
	globalAuto := s.cfg.MonitorSnapshot().AutoDevelopIssues
	if !p.AutoDevelop && !globalAuto {
		return
	}
	// Auto-develop can start an agent on code from an arbitrary issue author.
	// Require an explicit author allowlist even for project-level opt-in.
	if len(p.AutoDevelopAuthors) == 0 {
		s.runner.LogMonitor("WARN", "auto-develop", fmt.Sprintf("跳过 %s：自动开发要求项目配置作者白名单（autoDevelopAuthors）", key))
		return
	}
	allowed := false
	for _, name := range p.AutoDevelopAuthors {
		if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(name), "@"), strings.TrimSpace(author)) {
			allowed = true
			break
		}
	}
	if !allowed {
		return
	}
	issue, err := s.st.FindIssueByKey(ctx, p.ID, key)
	if err != nil {
		return
	}
	if !globalAuto && !runner.IsDevelopableIssue(*issue, p.AutoDevelopLabels, p.AutoDevelopText) {
		return
	}
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	count, err := s.st.CountAutoDevelopRunsSince(ctx, p.ID, startOfDay)
	if err != nil {
		return
	}
	limit := p.AutoDevelopDailyLimit
	if limit <= 0 {
		limit = 5
	}
	if count >= limit {
		_, _ = s.st.AddComment(ctx, issue.ID, "system", fmt.Sprintf("🤖 自动预开发已达到今日上限 (%d)，未启动新的开发任务。", limit))
		return
	}
	agent := strings.TrimSpace(p.AutoDevelopAgent)
	if agent == "" {
		agent = s.runner.AgentForRole(runner.AgentRoleIssueDevelop)
	}
	if agent == "" {
		agent = s.runner.DefaultAgentName()
	}
	if agent == "" {
		_, _ = s.st.AddComment(ctx, issue.ID, "system", "🤖 自动预开发已开启，但未配置任何 Agent，无法启动。请在设置中配置 Agent 或指定预开发 Agent。")
		return
	}
	if _, err := s.runner.StartAutoDevelopRun(ctx, issue.ID, agent, runner.AutoDevelopPrompt(issue)); err != nil {
		_, _ = s.st.AddComment(ctx, issue.ID, "system", "🤖 自动预开发启动失败: "+err.Error())
	}
}
