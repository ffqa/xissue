package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

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
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Labels []string `json:"labels"`
		RepoID *int64   `json:"repoId"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("title is required"))
		return
	}
	var repoID *int64
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
	}
	issue, err := s.st.CreateLocalIssueInRepo(r.Context(), projectID, repoID, strings.TrimSpace(body.Title), body.Body, body.Labels)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.runner.ExportIssueMarkdown(r.Context(), issue.ID)
	s.hub.BroadcastIssueUpdated(issue)
	writeJSON(w, http.StatusCreated, issue)
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
