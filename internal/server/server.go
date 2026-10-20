// Package server exposes the HTTP API, WebSocket stream, and embedded Web UI.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/knowledge"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/runner"
	"github.com/xpier/xissue/internal/store"
)

const Version = "0.2.10"

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

func (s *Server) Shutdown() {
	if s.runner != nil {
		s.runner.Shutdown()
	}
}

func (s *Server) Handler() http.Handler {
	return s.authMiddleware(s.mux)
}

func isAllowedHost(hostHeader, listenAddr string) bool {
	h := hostHeader
	if host, _, err := net.SplitHostPort(hostHeader); err == nil {
		h = host
	}
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "127.0.0.1" || h == "localhost" || h == "::1" {
		return true
	}
	if listenAddr != "" {
		lh := listenAddr
		if host, _, err := net.SplitHostPort(listenAddr); err == nil {
			lh = host
		}
		lh = strings.ToLower(strings.TrimSpace(lh))
		if lh != "" && lh != "0.0.0.0" && lh != "::" && lh == h {
			return true
		}
	}
	return false
}

func isStateChanging(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete || method == http.MethodPatch
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Host header validation (DNS Rebinding protection)
		if !isAllowedHost(r.Host, s.cfg.Listen) {
			http.Error(w, "forbidden: invalid host header", http.StatusForbidden)
			return
		}

		// 2. CSRF & Cross-Site protection for state-changing endpoints
		if isStateChanging(r.Method) && strings.HasPrefix(r.URL.Path, "/api/") {
			// Check Origin header if present
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !isAllowedHost(u.Host, s.cfg.Listen) {
					http.Error(w, "forbidden: cross-origin request rejected", http.StatusForbidden)
					return
				}
			}

			// /api/mcp is a machine JSON-RPC endpoint used by CLI stdio bridge and IDE agents.
			// Cross-origin browser requests with application/json trigger mandatory CORS preflight and are blocked by Origin/Host checks.
			isMCPEndpoint := r.URL.Path == "/api/mcp"

			// Require custom header X-XIssue or Authorization to block cross-site simple requests (forms/scripts)
			authHeader := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			hasCustomHeader := r.Header.Get("X-XIssue") != ""
			hasValidBearer := s.cfg.Token != "" && subtle.ConstantTimeCompare([]byte(authHeader), []byte(s.cfg.Token)) == 1

			if !isMCPEndpoint && !hasCustomHeader && !hasValidBearer {
				http.Error(w, "forbidden: missing X-XIssue header for state-changing request", http.StatusForbidden)
				return
			}
		}

		// 3. Token-based authentication
		if s.cfg.Token == "" {
			next.ServeHTTP(w, r)
			return
		}

		cfgToken := []byte(s.cfg.Token)
		queryTokenValid := false
		if q := r.URL.Query().Get("token"); q != "" && subtle.ConstantTimeCompare([]byte(q), cfgToken) == 1 {
			queryTokenValid = true
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
			authValid := subtle.ConstantTimeCompare([]byte(auth), cfgToken) == 1
			cookieValid := cookie != nil && subtle.ConstantTimeCompare([]byte(cookie.Value), cfgToken) == 1
			if !authValid && !cookieValid && !queryTokenValid {
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
	m.HandleFunc("POST /api/restart", s.handleRestart)
	m.HandleFunc("GET /api/agents", s.handleAgents)
	m.HandleFunc("POST /api/agents/reload", s.handleReloadAgents)
	m.HandleFunc("PUT /api/agents/{name}", s.handleUpsertAgent)
	m.HandleFunc("DELETE /api/agents/{name}", s.handleDeleteAgent)
	m.HandleFunc("GET /api/agents/{name}/models", s.handleAgentModels)
	m.HandleFunc("GET /api/ws", s.hub.serveWS)
	m.HandleFunc("GET /api/fs/browse", s.handleBrowseDir)
	m.HandleFunc("GET /api/fs/inspect", s.handleInspectDir)

	m.HandleFunc("GET /api/projects", s.handleListProjects)
	m.HandleFunc("POST /api/projects", s.handleCreateProject)
	m.HandleFunc("GET /api/projects/{id}", s.handleGetProject)
	m.HandleFunc("PATCH /api/projects/{id}", s.handleUpdateProject)
	m.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)

	m.HandleFunc("POST /api/projects/{id}/repos", s.handleAddRepo)
	m.HandleFunc("POST /api/projects/{id}/repos/refresh", s.handleRefreshRepos)
	m.HandleFunc("DELETE /api/projects/{id}/repos/{repoId}", s.handleDeleteRepo)
	m.HandleFunc("POST /api/projects/{id}/sync", s.handleSyncProject)

	m.HandleFunc("GET /api/projects/{id}/issues", s.handleListIssues)
	m.HandleFunc("POST /api/projects/{id}/issues", s.handleCreateIssue)
	m.HandleFunc("GET /api/issues/{id}", s.handleGetIssue)
	m.HandleFunc("PATCH /api/issues/{id}", s.handleUpdateIssue)
	m.HandleFunc("DELETE /api/issues/{id}", s.handleDeleteIssue)
	m.HandleFunc("POST /api/issues/batch-delete", s.handleBatchDeleteIssues)
	m.HandleFunc("POST /api/issues/{id}/publish", s.handlePublishIssue)

	m.HandleFunc("GET /api/issues/{id}/comments", s.handleListComments)
	m.HandleFunc("POST /api/issues/{id}/comments", s.handleAddComment)

	m.HandleFunc("GET /api/issues/{id}/runs", s.handleListRuns)
	m.HandleFunc("POST /api/issues/{id}/runs", s.handleStartRun)
	m.HandleFunc("GET /api/runs/{id}", s.handleGetRun)
	m.HandleFunc("DELETE /api/runs/{id}", s.handleDeleteRun)
	m.HandleFunc("GET /api/runs/{id}/events", s.handleListEvents)
	m.HandleFunc("POST /api/runs/{id}/message", s.handleSendMessage)
	m.HandleFunc("POST /api/runs/{id}/permissions/{requestId}", s.handlePermission)
	m.HandleFunc("POST /api/runs/{id}/cancel", s.handleCancelRun)
	m.HandleFunc("GET /api/runs/{id}/changes", s.handleRunChanges)
	m.HandleFunc("GET /api/runs/{id}/archive", s.handleGetRunArchive)
	m.HandleFunc("GET /api/runs/{id}/file", s.handleReadFile)
	m.HandleFunc("PUT /api/runs/{id}/file", s.handleWriteFile)
	m.HandleFunc("POST /api/runs/{id}/submit", s.handleSubmitRun)
	m.HandleFunc("POST /api/runs/{id}/discard", s.handleDiscardRun)

	m.HandleFunc("POST /api/mcp", s.handleMCP)
	m.HandleFunc("GET /api/mcp/status", s.handleMCPStatus)
	m.HandleFunc("POST /api/mcp/install", s.handleMCPInstall)

	m.HandleFunc("GET /api/notifications", s.handleListNotifications)
	m.HandleFunc("POST /api/notifications/{id}/read", s.handleMarkNotificationRead)
	m.HandleFunc("POST /api/notifications/read-all", s.handleMarkAllNotificationsRead)

	m.HandleFunc("GET /api/projects/{id}/watches", s.handleListWatches)
	m.HandleFunc("POST /api/projects/{id}/watches", s.handleAddWatch)
	m.HandleFunc("DELETE /api/projects/{id}/watches/{watchId}", s.handleDeleteWatch)
	m.HandleFunc("POST /api/projects/{id}/check-watches", s.handleCheckWatches)
	m.HandleFunc("POST /api/projects/{id}/batch-run", s.handleBatchRunTodo)
	m.HandleFunc("POST /api/projects/{id}/cherry-pick", s.handleCherryPick)
	m.HandleFunc("GET /api/projects/{id}/prs", s.handleListProjectPRs)
	m.HandleFunc("POST /api/projects/{id}/prs/{number}/merge", s.handleMergeProjectPR)
	m.HandleFunc("POST /api/projects/{id}/prs/{number}/close", s.handleCloseProjectPR)
	m.HandleFunc("POST /api/projects/{id}/pull", s.handlePullProjectRepos)
	m.HandleFunc("GET /api/projects/{id}/hooks", s.handleDetectProjectHooks)
	m.HandleFunc("GET /api/plugins", s.handleListPlugins)

	m.HandleFunc("GET /api/projects/{id}/docs", s.handleListProjectDocs)
	m.HandleFunc("GET /api/projects/{id}/docs/detail", s.handleGetProjectDocDetail)
	m.HandleFunc("POST /api/projects/{id}/docs/sync-wiki", s.handleSyncProjectWiki)
	m.HandleFunc("POST /api/projects/{id}/docs/search", s.handleSearchProjectDocs)
	m.HandleFunc("PUT /api/projects/{id}/docs/save", s.handleSaveProjectDoc)

	m.HandleFunc("GET /api/monitor/status", s.handleMonitorStatus)
	m.HandleFunc("GET /api/monitor/config", s.handleGetMonitorConfig)
	m.HandleFunc("PUT /api/monitor/config", s.handleUpdateMonitorConfig)
	m.HandleFunc("POST /api/monitor/test-notify", s.handleTestNotify)
	m.HandleFunc("GET /api/monitor/logs", s.handleMonitorLogs)
	m.HandleFunc("POST /api/monitor/trigger", s.handleMonitorTrigger)
	m.HandleFunc("POST /api/monitor/prune", s.handleMonitorPrune)

	m.HandleFunc("GET /api/repos/{repoId}/branches", s.handleRepoBranches)
	m.HandleFunc("GET /api/repos/{repoId}/commits", s.handleRepoCommits)

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

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"restarting": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		s.Shutdown()
		_ = s.st.Close()
		// When running under launchd/systemd KeepAlive, exiting is enough,
		// while execSelf also seamlessly replaces the process in foreground terminals.
		if exe, err := os.Executable(); err == nil {
			execSelf(exe)
		}
		os.Exit(0)
	}()
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.runner.ListAgents())
}

func (s *Server) handleReloadAgents(w http.ResponseWriter, r *http.Request) {
	list, err := s.runner.ReloadAgents()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleUpsertAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body config.AgentConfig
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	st, err := s.runner.UpsertAgent(name, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.runner.DeleteAgent(name); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentModels(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	refresh := r.URL.Query().Get("refresh") == "1"
	res, err := s.runner.ListAgentModels(r.Context(), name, refresh)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
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
	s.refreshProjectRepos(r.Context(), p)
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
	s.refreshProjectRepos(r.Context(), p)
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

	// Double-check project name if provided in query or JSON body
	var reqBody struct {
		Name string `json:"name"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		_ = readJSON(r, &reqBody)
	}
	confirmName := strings.TrimSpace(reqBody.Name)
	if confirmName == "" {
		confirmName = strings.TrimSpace(r.URL.Query().Get("name"))
	}
	if confirmName != "" && confirmName != proj.Name {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("项目名称不匹配：请输入 %q 以确认删除", proj.Name))
		return
	}

	// 1. Terminate agent processes, clear warm pool, remove worktrees and git mirrors on disk
	_ = s.runner.CleanupProject(r.Context(), id)

	// 2. Cascade delete all project entities from SQLite database
	if err := s.st.DeleteProject(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
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
	p, err := s.st.GetProject(r.Context(), projectID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.refreshProjectRepos(r.Context(), p)
	repos := p.Repos
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
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	run, err := s.runner.StartRun(r.Context(), issueID, body.Agent, body.Model, body.Prompt)
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

func (s *Server) handleGetRunArchive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	dir, err := s.runner.ArchiveRun(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	analysisBytes, _ := os.ReadFile(filepath.Join(dir, "analysis.md"))
	eventsBytes, _ := os.ReadFile(filepath.Join(dir, "events.log"))
	writeJSON(w, http.StatusOK, map[string]any{
		"dir":      dir,
		"analysis": string(analysisBytes),
		"events":   string(eventsBytes),
	})
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

func (s *Server) handleDeleteRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.runner.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.st.ListNotifications(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.st.MarkNotificationRead(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if err := s.st.MarkAllNotificationsRead(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListWatches(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := s.st.ListBranchWatches(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAddWatch(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		RepoID int64  `json:"repoId"`
		Branch string `json:"branch"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	bw, err := s.runner.AddBranchWatch(r.Context(), pid, body.RepoID, body.Branch)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, bw)
}

func (s *Server) handleDeleteWatch(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	wid, ok := pathID(w, r, "watchId")
	if !ok {
		return
	}
	if err := s.st.DeleteBranchWatch(r.Context(), pid, wid); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCheckWatches(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	watchesTriggered, prsUpdated, err := s.runner.CheckWatchesAndPRs(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"watchesTriggered": watchesTriggered,
		"prsUpdated":       prsUpdated,
	})
}

func (s *Server) handleBatchRunTodo(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Agent string `json:"agent"`
		Model string `json:"model"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := readJSON(r, &body); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	runs, err := s.runner.BatchRunTodoIssues(r.Context(), pid, body.Agent, body.Model)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"started": len(runs),
		"runs":    runs,
	})
}

func (s *Server) handleRepoBranches(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r, "repoId")
	if !ok {
		return
	}
	branches, err := s.runner.ListRepoBranches(r.Context(), repoID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if branches == nil {
		branches = []string{}
	}
	writeJSON(w, http.StatusOK, branches)
}

func (s *Server) handleRepoCommits(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r, "repoId")
	if !ok {
		return
	}
	branch := r.URL.Query().Get("branch")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	commits, err := s.runner.ListRepoCommits(r.Context(), repoID, branch, limit)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if commits == nil {
		commits = []gitx.CommitInfo{}
	}
	writeJSON(w, http.StatusOK, commits)
}

func (s *Server) handleCherryPick(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req runner.CherryPickRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req.ProjectID = pid
	res, err := s.runner.CherryPick(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleDetectProjectHooks(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	hooks := s.runner.DetectProjectHooks(r.Context(), id)
	writeJSON(w, http.StatusOK, hooks)
}

func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	plugins := s.runner.ListPlugins()
	if plugins == nil {
		plugins = []domain.PluginInfo{}
	}
	writeJSON(w, http.StatusOK, plugins)
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
	return json.NewDecoder(io.LimitReader(r.Body, 10<<20)).Decode(out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"json marshal error"}` + "\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
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

func (s *Server) handleListProjectPRs(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	prs, err := s.runner.ListProjectPRs(r.Context(), id, state)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if prs == nil {
		prs = []provider.RemotePR{}
	}
	writeJSON(w, http.StatusOK, prs)
}

func (s *Server) handleMergeProjectPR(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	numStr := r.PathValue("number")
	number, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid PR number: %v", numStr))
		return
	}
	var body struct {
		RepoID int64  `json:"repoId"`
		Method string `json:"method"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	msg, err := s.runner.MergeProjectPR(r.Context(), projectID, body.RepoID, number, body.Method)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "merged": true, "message": msg})
}

func (s *Server) handleCloseProjectPR(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	numStr := r.PathValue("number")
	number, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid PR number: %v", numStr))
		return
	}
	var body struct {
		RepoID int64 `json:"repoId"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.runner.CloseProjectPR(r.Context(), projectID, body.RepoID, number); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "closed": true})
}

func (s *Server) handlePullProjectRepos(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	updated, err := s.runner.PullProjectRepos(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "pulledRepos": updated, "message": "已从远端成功拉取最新代码"})
}

func (s *Server) handleMonitorStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.runner.GetMonitorStatus(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleGetMonitorConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.Monitor)
}

func (s *Server) handleUpdateMonitorConfig(w http.ResponseWriter, r *http.Request) {
	var body config.MonitorConfig
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.cfg.UpdateMonitor(body); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	status := "运行中"
	if !body.Enabled {
		status = "已暂停"
	}
	s.runner.LogMonitor("EVENT", "watcher", fmt.Sprintf("监控配置已更新: 状态=%s, 活跃时段=%s", status, body.ActiveHours))
	writeJSON(w, http.StatusOK, s.cfg.Monitor)
}

func (s *Server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	res, err := s.runner.SendTestNotification(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleMonitorLogs(w http.ResponseWriter, r *http.Request) {
	hoursStr := r.URL.Query().Get("hours")
	hours := 8
	if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
		hours = h
	}
	logs := s.runner.GetMonitorLogs(r.Context(), hours)
	if logs == nil {
		logs = []runner.MonitorLogEntry{}
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) handleMonitorTrigger(w http.ResponseWriter, r *http.Request) {
	watchesTriggered, prsUpdated, err := s.runner.CheckWatchesAndPRs(r.Context(), 0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"watchesTriggered": watchesTriggered,
		"prsUpdated":       prsUpdated,
		"message":          fmt.Sprintf("轮询检查完成: 触发 %d 个分支推送, %d 个 PR 状态流转", watchesTriggered, prsUpdated),
	})
}

func (s *Server) handleMonitorPrune(w http.ResponseWriter, r *http.Request) {
	hoursStr := r.URL.Query().Get("hours")
	hours := 8
	if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
		hours = h
	}
	pruned, err := s.runner.PruneMonitorLogs(time.Duration(hours) * time.Hour)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "prunedCount": pruned, "message": fmt.Sprintf("已成功清理并保留最近 %d 小时日志", hours)})
}

// ---------- Knowledge Base & Wiki Handlers ----------

func (s *Server) handleListProjectDocs(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	docs, err := knowledge.ScanProjectDocs(r.Context(), s.cfg.DataDir, p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if docs == nil {
		docs = []domain.DocItem{}
	}
	writeJSON(w, http.StatusOK, docs)
}

func (s *Server) handleGetProjectDocDetail(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	docID := r.URL.Query().Get("docId")
	if docID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("缺少 docId 参数"))
		return
	}
	p, err := s.st.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	detail, err := knowledge.GetDocDetail(r.Context(), s.cfg.DataDir, p, docID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleSyncProjectWiki(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	var req struct {
		RepoID int64 `json:"repoId"`
	}
	_ = readJSON(r, &req)

	synced := 0
	var syncErrors []string

	for _, repo := range p.Repos {
		if req.RepoID > 0 && repo.ID != req.RepoID {
			continue
		}
		if repo.Provider != domain.ProviderGitHub || repo.OwnerRepo == "" {
			continue
		}
		if err := knowledge.SyncGitHubWiki(r.Context(), s.cfg.DataDir, repo); err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", repo.Name, err))
		} else {
			synced++
		}
	}

	msg := fmt.Sprintf("已完成同步 %d 个仓库的 GitHub Wiki", synced)
	if len(syncErrors) > 0 {
		msg += fmt.Sprintf(" (部分提示: %s)", strings.Join(syncErrors, "; "))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"syncedCount": synced,
		"syncErrors":  syncErrors,
		"message":     msg,
	})
}

func (s *Server) handleSearchProjectDocs(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	results, err := knowledge.SearchDocs(r.Context(), s.cfg.DataDir, p, req.Query, req.Limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if results == nil {
		results = []domain.DocSearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleSaveProjectDoc(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		DocID   string `json:"docId"`
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err := knowledge.SaveDoc(r.Context(), s.cfg.DataDir, p, req.DocID, req.Content); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "文档已成功保存",
	})
}


