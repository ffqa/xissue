// Package server exposes the HTTP API, WebSocket stream, and embedded Web UI.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/runner"
	"github.com/xpier/xissue/internal/store"
)

const Version = "0.1.0"

type Server struct {
	cfg    *config.Config
	st     *store.Store
	runner *runner.Runner
	hub    *Hub
	mux    *http.ServeMux
}

func New(cfg *config.Config, st *store.Store) *Server {
	hub := newHub()
	r := runner.New(cfg, st, hub)
	s := &Server{
		cfg:    cfg,
		st:     st,
		runner: r,
		hub:    hub,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.authMiddleware(s.mux)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token == "" {
			next.ServeHTTP(w, r)
			return
		}
		if q := r.URL.Query().Get("token"); q != "" && q == s.cfg.Token {
			http.SetCookie(w, &http.Cookie{
				Name:     "xissue_token",
				Value:    s.cfg.Token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			if r.URL.Path == "/" {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			cookie, _ := r.Cookie("xissue_token")
			if auth != s.cfg.Token && (cookie == nil || cookie.Value != s.cfg.Token) {
				writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /api/health", s.handleHealth)
	m.HandleFunc("GET /api/agents", s.handleAgents)
	m.HandleFunc("GET /api/ws", s.hub.serveWS)
	m.HandleFunc("GET /api/fs/browse", s.handleBrowseDir)
	m.HandleFunc("GET /api/fs/inspect", s.handleInspectDir)

	m.HandleFunc("GET /api/projects", s.handleListProjects)
	m.HandleFunc("POST /api/projects", s.handleCreateProject)
	m.HandleFunc("GET /api/projects/{id}", s.handleGetProject)
	m.HandleFunc("PATCH /api/projects/{id}", s.handleUpdateProject)
	m.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)

	m.HandleFunc("POST /api/projects/{id}/repos", s.handleAddRepo)
	m.HandleFunc("DELETE /api/projects/{id}/repos/{repoId}", s.handleDeleteRepo)
	m.HandleFunc("POST /api/projects/{id}/sync", s.handleSyncProject)

	m.HandleFunc("GET /api/projects/{id}/issues", s.handleListIssues)
	m.HandleFunc("POST /api/projects/{id}/issues", s.handleCreateIssue)
	m.HandleFunc("GET /api/issues/{id}", s.handleGetIssue)
	m.HandleFunc("PATCH /api/issues/{id}", s.handleUpdateIssue)
	m.HandleFunc("POST /api/issues/{id}/publish", s.handlePublishIssue)

	m.HandleFunc("GET /api/issues/{id}/comments", s.handleListComments)
	m.HandleFunc("POST /api/issues/{id}/comments", s.handleAddComment)

	m.HandleFunc("GET /api/issues/{id}/runs", s.handleListRuns)
	m.HandleFunc("POST /api/issues/{id}/runs", s.handleStartRun)
	m.HandleFunc("GET /api/runs/{id}", s.handleGetRun)
	m.HandleFunc("GET /api/runs/{id}/events", s.handleListEvents)
	m.HandleFunc("POST /api/runs/{id}/message", s.handleSendMessage)
	m.HandleFunc("POST /api/runs/{id}/permissions/{requestId}", s.handlePermission)
	m.HandleFunc("POST /api/runs/{id}/cancel", s.handleCancelRun)
	m.HandleFunc("GET /api/runs/{id}/changes", s.handleRunChanges)
	m.HandleFunc("GET /api/runs/{id}/file", s.handleReadFile)
	m.HandleFunc("PUT /api/runs/{id}/file", s.handleWriteFile)
	m.HandleFunc("POST /api/runs/{id}/submit", s.handleSubmitRun)
	m.HandleFunc("POST /api/runs/{id}/discard", s.handleDiscardRun)

	m.Handle("/", spaHandler())
}

// ---------- handlers ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"version":           Version,
		"editorUrlTemplate": s.cfg.EditorURLTemplate,
	})
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.runner.ListAgents())
}

func (s *Server) handleBrowseDir(w http.ResponseWriter, r *http.Request) {
	res, err := gitx.BrowseDir(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleInspectDir(w http.ResponseWriter, r *http.Request) {
	res, err := gitx.InspectDir(r.Context(), r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
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
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name is required"))
		return
	}
	p, err := s.st.CreateProject(r.Context(), strings.TrimSpace(body.Name), body.Description)
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
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.UpdateProject(r.Context(), id, body.Name, body.Description)
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
	if err := s.st.DeleteProject(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	if body.Name == "" || body.RemoteURL == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name and remoteUrl are required"))
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
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSyncProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	repos, err := s.st.ListRepos(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	imported := 0
	for _, repo := range repos {
		p := provider.For(repo.Provider)
		if !p.Caps().Issues {
			continue
		}
		list, err := p.ListOpenIssues(r.Context(), repo)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		repoID := repo.ID
		for _, ri := range list {
			num := ri.Number
			created, err := s.st.UpsertRemoteIssue(r.Context(), domain.Issue{
				ProjectID:    projectID,
				Key:          provider.IssueKey(repo, ri.Number),
				RepoID:       &repoID,
				RemoteNumber: &num,
				RemoteURL:    ri.URL,
				Title:        ri.Title,
				Body:         ri.Body,
				Labels:       ri.Labels,
			})
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			if created {
				imported++
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported})
}

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
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("title is required"))
		return
	}
	issue, err := s.st.CreateLocalIssue(r.Context(), projectID, strings.TrimSpace(body.Title), body.Body, body.Labels)
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

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := s.st.ListRuns(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	issueID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Agent  string `json:"agent"`
		Prompt string `json:"prompt"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	run, err := s.runner.StartRun(r.Context(), issueID, body.Agent, body.Prompt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	run, err := s.st.GetRun(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	list, err := s.st.ListEvents(r.Context(), id, after)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.runner.SendMessage(r.Context(), id, body.Text); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePermission(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	reqID := r.PathValue("requestId")
	var body struct {
		OptionID string `json:"optionId"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.runner.RespondPermission(id, reqID, body.OptionID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.runner.Cancel(id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunChanges(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ch, err := s.runner.Changes(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	repoID, _ := strconv.ParseInt(r.URL.Query().Get("repoId"), 10, 64)
	path := r.URL.Query().Get("path")
	side := r.URL.Query().Get("side")
	content, err := s.runner.ReadFile(r.Context(), id, repoID, path, side)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": content})
}

func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	repoID, _ := strconv.ParseInt(r.URL.Query().Get("repoId"), 10, 64)
	path := r.URL.Query().Get("path")
	var body struct {
		Content string `json:"content"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.runner.WriteFile(r.Context(), id, repoID, path, body.Content); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSubmitRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req runner.SubmitRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.runner.Submit(r.Context(), id, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if res == nil {
		res = []runner.SubmitResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": res})
}

func (s *Server) handleDiscardRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.runner.Discard(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- helpers ----------

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("invalid id"))
		return 0, false
	}
	return id, true
}

func readJSON(r *http.Request, out any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeErr(w, http.StatusInternalServerError, err)
}
