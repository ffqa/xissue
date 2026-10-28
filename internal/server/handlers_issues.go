package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/store"
)

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := s.st.ListIssues(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Title      string               `json:"title"`
		Body       string               `json:"body"`
		Labels     []string             `json:"labels"`
		Priority   domain.IssuePriority `json:"priority"`
		RepoID     *int64               `json:"repoId"`
		ParentID   *int64               `json:"parentId"`
		SyncRemote bool                 `json:"syncRemote"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("title is required"))
		return
	}
	priority := body.Priority
	if priority == "" {
		priority = domain.IssuePriorityNormal
	}
	if !domain.ValidIssuePriority(priority) {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid priority %q", priority))
		return
	}
	var err error
	var parent *domain.Issue
	if body.ParentID != nil && *body.ParentID > 0 {
		parent, err = s.st.GetIssue(r.Context(), *body.ParentID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if parent.ProjectID != projectID {
			writeErr(w, http.StatusBadRequest, errors.New("parent issue does not belong to this project"))
			return
		}
		if body.RepoID == nil {
			body.RepoID = parent.RepoID
		}
	}
	var repoID *int64
	var targetRepo *domain.Repo
	if body.RepoID != nil && *body.RepoID > 0 {
		repo, err := s.st.GetRepo(r.Context(), *body.RepoID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if repo.ProjectID != projectID {
			writeErr(w, http.StatusBadRequest, errors.New("repo does not belong to this project"))
			return
		}
		repoID = &repo.ID
		targetRepo = repo
	}
	if body.SyncRemote {
		if targetRepo == nil {
			writeErr(w, http.StatusBadRequest, errors.New("syncRemote requires a repository"))
			return
		}
		if !provider.For(targetRepo.Provider).Caps().Issues {
			writeErr(w, http.StatusBadRequest, errors.New("this repository provider does not support remote issues"))
			return
		}
	}
	var issue *domain.Issue
	if parent != nil {
		issue, err = s.st.CreateSubIssue(r.Context(), projectID, parent.ID, repoID, strings.TrimSpace(body.Title), body.Body, body.Labels, priority)
	} else {
		issue, err = s.st.CreateLocalIssueInRepo(r.Context(), projectID, repoID, strings.TrimSpace(body.Title), body.Body, body.Labels, priority)
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if parent != nil {
		_ = s.st.AddIssueEvent(r.Context(), parent.ID, "subissue", "me", "创建子任务 "+issue.Key)
	}
	if body.SyncRemote && targetRepo != nil {
		issue = s.syncNewIssueToRemote(r.Context(), issue, *targetRepo, body.Labels)
	}
	s.runner.ExportIssueMarkdown(r.Context(), issue.ID)
	s.hub.BroadcastIssueUpdated(issue)
	writeJSON(w, http.StatusCreated, issue)
}

// syncNewIssueToRemote creates the platform issue and binds its number/key to
// the local row. The local issue is kept on failure so the user does not lose
// their text; the transient SyncError is shown by the create dialog.
func (s *Server) syncNewIssueToRemote(ctx context.Context, issue *domain.Issue, repo domain.Repo, labels []string) *domain.Issue {
	p := provider.For(repo.Provider)
	ri, err := p.CreateIssue(ctx, repo, issue.Title, issue.Body)
	if err != nil {
		issue.SyncError = err.Error()
		_ = s.st.AddIssueEvent(ctx, issue.ID, "remote_sync_failed", "system", "远程 Issue 创建失败: "+err.Error())
		return issue
	}
	if len(labels) > 0 {
		if err := p.AddLabels(ctx, repo, ri.Number, labels); err != nil {
			_ = s.st.AddIssueEvent(ctx, issue.ID, "remote_sync_warning", "system", "远程标签写入失败: "+err.Error())
		}
	}
	updated, err := s.st.MarkIssuePublished(ctx, issue.ID, repo.ID, ri.Number, provider.IssueKey(repo, ri.Number), ri.URL)
	if err != nil {
		issue.SyncError = err.Error()
		_ = s.st.AddIssueEvent(ctx, issue.ID, "remote_sync_failed", "system", "远程 Issue 已创建但本地绑定失败: "+err.Error())
		return issue
	}
	return updated
}

func (s *Server) handleListSubIssues(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := s.st.ListSubIssues(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleListIssueEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := s.st.ListIssueEvents(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	issue, err := s.st.GetIssue(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issue)
}

func (s *Server) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var patch store.IssuePatch
	if err := readJSON(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	issue, err := s.st.UpdateIssue(r.Context(), id, patch)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.runner.ExportIssueMarkdown(r.Context(), issue.ID)
	s.hub.BroadcastIssueUpdated(issue)
	writeJSON(w, http.StatusOK, issue)
}

func (s *Server) cancelIssueRuns(ctx context.Context, issueID int64) {
	if runs, err := s.st.ListRuns(ctx, issueID); err == nil {
		for _, run := range runs {
			_ = s.runner.Cancel(run.ID)
		}
	}
}

func (s *Server) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	s.cancelIssueRuns(r.Context(), id)
	if err := s.st.DeleteIssue(r.Context(), id); err != nil {
		writeStoreErr(w, err)
		return
	}
	_ = os.RemoveAll(filepath.Join(s.cfg.AttachmentsDir(), strconv.FormatInt(id, 10)))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleBatchDeleteIssues(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	for _, id := range body.IDs {
		s.cancelIssueRuns(r.Context(), id)
	}
	n, err := s.st.BatchDeleteIssues(r.Context(), body.IDs)
	for _, id := range body.IDs {
		_ = os.RemoveAll(filepath.Join(s.cfg.AttachmentsDir(), strconv.FormatInt(id, 10)))
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": n})
}

func (s *Server) handlePublishIssue(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		RepoID int64 `json:"repoId"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	issue, err := s.st.GetIssue(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	repo, err := s.st.GetRepo(r.Context(), body.RepoID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if repo.ProjectID != issue.ProjectID {
		writeErr(w, http.StatusBadRequest, errors.New("repository does not belong to this issue's project"))
		return
	}
	p := provider.For(repo.Provider)
	if !p.Caps().Issues {
		writeErr(w, http.StatusBadRequest, provider.ErrUnsupported)
		return
	}
	ri, err := p.CreateIssue(r.Context(), *repo, issue.Title, issue.Body)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	updated, err := s.st.MarkIssuePublished(r.Context(), issue.ID, repo.ID, ri.Number, provider.IssueKey(*repo, ri.Number), ri.URL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.runner.ExportIssueMarkdown(r.Context(), updated.ID)
	s.hub.BroadcastIssueUpdated(updated)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := s.st.ListComments(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Body) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("body is required"))
		return
	}
	c, err := s.st.AddComment(r.Context(), id, "me", body.Body)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.runner.ExportIssueMarkdown(r.Context(), id)
	writeJSON(w, http.StatusCreated, c)
}
