package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/store"
)

func (s *Server) refreshProjectRepos(ctx context.Context, p *domain.Project) {
	if p == nil {
		return
	}
	for i := range p.Repos {
		if gitx.DetectRepoRemote(ctx, &p.Repos[i]) {
			if updated, err := s.st.UpdateRepo(ctx, p.Repos[i]); err == nil {
				p.Repos[i] = *updated
			}
		}
	}
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListProjects(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []domain.Project{}
	}
	for i := range list {
		s.refreshProjectRepos(r.Context(), &list[i])
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		DevMode     *domain.DevMode `json:"devMode"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name is required"))
		return
	}
	var dm []domain.DevMode
	if body.DevMode != nil {
		dm = append(dm, *body.DevMode)
	}
	p, err := s.st.CreateProject(r.Context(), strings.TrimSpace(body.Name), body.Description, dm...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	p, err := s.st.GetProject(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if r.URL.Query().Get("refresh") != "0" {
		s.refreshProjectRepos(r.Context(), p)
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleRefreshRepos(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	p, err := s.st.GetProject(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if r.URL.Query().Get("refresh") != "0" {
		s.refreshProjectRepos(r.Context(), p)
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var patch store.ProjectPatch
	if err := readJSON(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.UpdateProject(r.Context(), id, patch)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	proj, err := s.st.GetProject(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}

	// The destructive confirmation is enforced server-side, not just in the UI.
	var reqBody struct {
		Name string `json:"name"`
	}
	if r.ContentLength != 0 {
		if err := readJSON(r, &reqBody); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	confirmName := strings.TrimSpace(reqBody.Name)
	if confirmName == "" {
		confirmName = strings.TrimSpace(r.URL.Query().Get("name"))
	}
	if confirmName != proj.Name {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("项目名称不匹配：请输入 %q 以确认删除", proj.Name))
		return
	}

	// Snapshot runs while rows still exist, then delete DB before touching disk.
	issues, _ := s.st.ListIssues(r.Context(), id)
	runsByIssue := make(map[int64][]domain.Run, len(issues))
	for _, issue := range issues {
		rs, _ := s.st.ListRuns(r.Context(), issue.ID)
		runsByIssue[issue.ID] = rs
	}
	if err := s.st.DeleteProject(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// 2. Terminate agent processes, clear warm pool, remove worktrees/mirrors.
	if err := s.runner.CleanupProjectData(r.Context(), proj, issues, runsByIssue); err != nil {
		s.runner.LogMonitor("WARN", "cleanup", fmt.Sprintf("项目 %s 数据库已删除，但磁盘清理不完整: %v", proj.Name, err))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": proj.Name})
}

func (s *Server) handleAddRepo(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body domain.Repo
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	body.ProjectID = projectID
	body.Name = strings.TrimSpace(body.Name)
	body.RemoteURL = strings.TrimSpace(body.RemoteURL)
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" || body.RemoteURL == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name and remoteUrl are required"))
		return
	}
	if err := domain.ValidateRepoName(body.Name); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	repo, err := s.st.CreateRepo(r.Context(), body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, repo)
}

func (s *Server) handleDeleteRepo(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	repoID, ok := pathID(w, r, "repoId")
	if !ok {
		return
	}
	if err := s.st.DeleteRepo(r.Context(), projectID, repoID); err != nil {
		if errors.Is(err, store.ErrRepoInUse) {
			writeErr(w, http.StatusConflict, err)
			return
		}
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
