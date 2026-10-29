// Package server exposes the HTTP API, WebSocket stream, and embedded Web UI.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/runner"
	"github.com/xpier/xissue/internal/store"
)

const Version = "0.5.11"

type Server struct {
	cfg    *config.Config
	st     *store.Store
	runner *runner.Runner
	remote *remoteManager
	hub    *Hub
	mux    *http.ServeMux

	syncMu           sync.Mutex
	lastIssueSync    time.Time
	lastSyncImported int
	lastSyncError    string
	stopCh           chan struct{}
	stopOnce         sync.Once
	jobCtx           context.Context
	jobCancel        context.CancelFunc
	scheduleMu       sync.Mutex
	scheduleRunning  map[string]bool
	startedAt        time.Time

	updateMu          sync.Mutex
	updateRunning     bool
	lastUpdateVersion string
	lastUpdateAt      time.Time
	latestUpdate      string

	statusMu       sync.Mutex
	lastCPUSeconds float64
	lastCPUAt      time.Time
	lastHeapAlloc  uint64
	lastHeapAt     time.Time

	usageMu    sync.Mutex
	dataSizeAt time.Time
	dataUsed   uint64
	runsUsed   uint64
	dbUsed     uint64

	terminalSem chan struct{}
}

func New(cfg *config.Config, st *store.Store) *Server {
	hub := newHub()
	r := runner.New(cfg, st, hub)
	jobCtx, jobCancel := context.WithCancel(context.Background())
	s := &Server{
		cfg:             cfg,
		st:              st,
		runner:          r,
		hub:             hub,
		mux:             http.NewServeMux(),
		stopCh:          make(chan struct{}),
		jobCtx:          jobCtx,
		jobCancel:       jobCancel,
		scheduleRunning: map[string]bool{},
		startedAt:       time.Now(),
		terminalSem:     make(chan struct{}, 8),
	}
	s.remote = newRemoteManager(cfg, st, r, hub)
	r.SetRemoteExecutor(s.remote)
	s.routes()
	go s.issueSyncLoop()
	go s.autoUpdateLoop()
	go s.statusLoop()
	go s.jobLoop()
	return s
}

func (s *Server) Shutdown() {
	s.stopOnce.Do(func() {
		if s.stopCh != nil {
			close(s.stopCh)
		}
	})
	if s.jobCancel != nil {
		s.jobCancel()
	}
	if s.remote != nil {
		s.remote.Close()
	}
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
		// 1. Host header validation (DNS Rebinding protection). Remote runners
		// authenticate with a bearer token and may connect through a LAN IP.
		if r.URL.Path != "/api/runners/ws" && !isAllowedHost(r.Host, s.cfg.Listen) {
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
	m.HandleFunc("GET /api/status", s.handleSystemStatus)
	m.HandleFunc("POST /api/restart", s.handleRestart)
	m.HandleFunc("GET /api/agents", s.handleAgents)
	m.HandleFunc("POST /api/agents/reload", s.handleReloadAgents)
	m.HandleFunc("PUT /api/agents/{name}", s.handleUpsertAgent)
	m.HandleFunc("DELETE /api/agents/{name}", s.handleDeleteAgent)
	m.HandleFunc("GET /api/agents/{name}/models", s.handleAgentModels)
	m.HandleFunc("GET /api/ws", s.hub.serveWS)
	m.HandleFunc("GET /api/runners", s.remote.handleList)
	m.HandleFunc("GET /api/runners/ws", s.remote.handleWS)
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
	m.HandleFunc("POST /api/projects/{id}/backlog/reorder", s.handleReorderBacklog)
	m.HandleFunc("GET /api/issues/{id}", s.handleGetIssue)
	m.HandleFunc("PATCH /api/issues/{id}", s.handleUpdateIssue)
	m.HandleFunc("DELETE /api/issues/{id}", s.handleDeleteIssue)
	m.HandleFunc("POST /api/issues/batch-delete", s.handleBatchDeleteIssues)
	m.HandleFunc("POST /api/issues/{id}/publish", s.handlePublishIssue)

	m.HandleFunc("GET /api/issues/{id}/comments", s.handleListComments)
	m.HandleFunc("POST /api/issues/{id}/comments", s.handleAddComment)
	m.HandleFunc("GET /api/issues/{id}/events", s.handleListIssueEvents)
	m.HandleFunc("GET /api/issues/{id}/subissues", s.handleListSubIssues)
	m.HandleFunc("GET /api/issues/{id}/pr-links", s.handleListIssuePRLinks)
	m.HandleFunc("POST /api/issues/{id}/pr-links", s.handleAddIssuePRLink)
	m.HandleFunc("DELETE /api/issues/{id}/pr-links/{linkID}", s.handleDeleteIssuePRLink)
	m.HandleFunc("GET /api/issues/{id}/attachments", s.handleListAttachments)
	m.HandleFunc("POST /api/issues/{id}/attachments", s.handleUploadAttachment)
	m.HandleFunc("GET /api/issues/{id}/attachments/{attachmentID}", s.handleGetAttachment)
	m.HandleFunc("DELETE /api/issues/{id}/attachments/{attachmentID}", s.handleDeleteAttachment)

	m.HandleFunc("GET /api/issues/{id}/runs", s.handleListRuns)
	m.HandleFunc("POST /api/issues/{id}/runs", s.handleStartRun)
	m.HandleFunc("GET /api/runs/{id}", s.handleGetRun)
	m.HandleFunc("DELETE /api/runs/{id}", s.handleDeleteRun)
	m.HandleFunc("GET /api/runs/{id}/events", s.handleListEvents)
	m.HandleFunc("GET /api/runs/{id}/terminal", s.handleRunTerminal)
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
	m.HandleFunc("DELETE /api/mcp", s.handleMCP)
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
	m.HandleFunc("GET /api/projects/{id}/prs/page", s.handleListProjectPRPage)
	m.HandleFunc("POST /api/projects/{id}/prs/{number}/merge", s.handleMergeProjectPR)
	m.HandleFunc("POST /api/projects/{id}/prs/{number}/close", s.handleCloseProjectPR)
	m.HandleFunc("POST /api/projects/{id}/pull", s.handlePullProjectRepos)
	m.HandleFunc("GET /api/projects/{id}/pull-status", s.handlePullProjectStatus)
	m.HandleFunc("GET /api/projects/{id}/hooks", s.handleDetectProjectHooks)
	m.HandleFunc("GET /api/plugins", s.handleListPlugins)
	m.HandleFunc("GET /api/plugins/ai", s.handlePluginAIInfo)
	m.HandleFunc("POST /api/plugins/ai", s.handlePluginAI)
	m.HandleFunc("PATCH /api/plugins/{id}", s.handlePatchPlugin)
	m.HandleFunc("GET /api/plugins/{id}/ui/{path...}", s.handlePluginUI)
	m.HandleFunc("GET /api/plugins/{id}/readme", s.handlePluginReadme)
	m.HandleFunc("GET /api/plugins/{id}/config", s.handlePluginConfig)
	m.HandleFunc("PUT /api/plugins/{id}/config", s.handlePluginConfig)

	m.HandleFunc("GET /api/projects/{id}/docs", s.handleListProjectDocs)
	m.HandleFunc("GET /api/projects/{id}/readmes", s.handleListProjectReadmes)
	m.HandleFunc("GET /api/projects/{id}/docs/detail", s.handleGetProjectDocDetail)
	m.HandleFunc("POST /api/projects/{id}/docs/sync-wiki", s.handleSyncProjectWiki)
	m.HandleFunc("POST /api/projects/{id}/docs/search", s.handleSearchProjectDocs)
	m.HandleFunc("PUT /api/projects/{id}/docs/save", s.handleSaveProjectDoc)

	m.HandleFunc("GET /api/monitor/status", s.handleMonitorStatus)
	m.HandleFunc("GET /api/schedules", s.handleListSchedules)
	m.HandleFunc("POST /api/schedules/{id}/run", s.handleRunSchedule)
	m.HandleFunc("GET /api/usage", s.handleUsage)
	m.HandleFunc("GET /api/settings", s.handleGetSettings)
	m.HandleFunc("PATCH /api/settings", s.handlePatchSettings)

	m.HandleFunc("GET /api/monitor/config", s.handleGetMonitorConfig)
	m.HandleFunc("PUT /api/monitor/config", s.handleUpdateMonitorConfig)
	m.HandleFunc("PATCH /api/monitor/config", s.handlePatchMonitorConfig)
	m.HandleFunc("POST /api/monitor/test-notify", s.handleTestNotify)
	m.HandleFunc("GET /api/monitor/logs", s.handleMonitorLogs)
	m.HandleFunc("POST /api/monitor/trigger", s.handleMonitorTrigger)
	m.HandleFunc("POST /api/monitor/sync", s.handleSyncAllIssues)
	m.HandleFunc("POST /api/monitor/prune", s.handleMonitorPrune)
	m.HandleFunc("POST /api/maintenance/prune", s.handlePruneRunStorage)

	m.HandleFunc("GET /api/repos/{repoId}/branches", s.handleRepoBranches)
	m.HandleFunc("GET /api/repos/{repoId}/merged-branches", s.handleListMergedBranches)
	m.HandleFunc("POST /api/repos/{repoId}/branches/delete", s.handleDeleteMergedBranches)
	m.HandleFunc("GET /api/jobs", s.handleListJobs)
	m.HandleFunc("POST /api/jobs/{id}/retry", s.handleRetryJob)
	m.HandleFunc("POST /api/jobs/{id}/cancel", s.handleCancelJob)
	m.HandleFunc("GET /api/repos/{repoId}/commits", s.handleRepoCommits)
	m.HandleFunc("GET /api/repos/{repoId}/commits/{sha}", s.handleRepoCommitDetail)

	m.Handle("/", spaHandler())
}

// ---------- handlers ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"version":           Version,
		"editorUrlTemplate": s.cfg.EditorTemplate(),
	})
}

// restartProcess replaces the running program after a short delay. When the
// process is supervised (launchd/systemd/brew services), exit and let the
// supervisor create a fresh process. syscall.Exec keeps the original PID and
// OS process start time, which makes ps/lstart and uptime disagree; reserve it
// for foreground terminals that have no supervisor to restart us.
func (s *Server) restartProcess(delay time.Duration) {
	time.Sleep(delay)
	s.Shutdown()
	_ = s.st.Close()
	if serviceManaged() {
		if requestSupervisorRestart() {
			os.Exit(0)
		}
		// A supervised unit normally has Restart=on-failure (or always);
		// a non-zero exit makes legacy units start a fresh process too.
		os.Exit(1)
	}
	if exe, err := os.Executable(); err == nil {
		execSelf(exe)
	}
	os.Exit(0)
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"restarting": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go s.restartProcess(150 * time.Millisecond)
}

type issueSyncResult struct {
	Projects int      `json:"projects"`
	Imported int      `json:"imported"`
	Errors   []string `json:"errors,omitempty"`
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

// ---------- Knowledge Base & Wiki Handlers ----------
