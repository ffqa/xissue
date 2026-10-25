package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/remote"
	"github.com/xpier/xissue/internal/runner"
	"github.com/xpier/xissue/internal/store"
)

// remoteManager owns outbound-runner WebSocket connections and routes run
// operations to them. Remote runners dial in, so execution hosts need no
// inbound port.
type remoteManager struct {
	cfg *config.Config
	st  *store.Store
	r   *runner.Runner
	hub *Hub

	mu        sync.Mutex
	runners   map[string]*remoteConn
	jobStates map[int64]*remoteJobState
	perms     map[string]*remotePermission
}

type remoteConn struct {
	id      string
	name    string
	version string
	conn    *websocket.Conn
	joined  time.Time

	writeMu sync.Mutex
	pendMu  sync.Mutex
	pend    map[string]chan remoteCallResult
}

type remoteCallResult struct {
	result json.RawMessage
	err    error
}

type remoteJobState struct {
	mu       sync.Mutex
	owner    *remoteConn
	runnerID string
	runID    int64
	issueID  int64
	autoPush bool
	summary  string
}

const remoteSummaryLimit = 4000

func (s *remoteJobState) resetSummary() {
	s.mu.Lock()
	s.summary = ""
	s.mu.Unlock()
}

func (s *remoteJobState) appendMessage(text string) {
	if text == "" {
		return
	}
	if len(text) > remoteSummaryLimit {
		text = text[len(text)-remoteSummaryLimit:]
	}
	s.mu.Lock()
	s.summary += text
	if len(s.summary) > remoteSummaryLimit {
		s.summary = s.summary[len(s.summary)-remoteSummaryLimit:]
	}
	s.mu.Unlock()
}

func (s *remoteJobState) summaryText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.summary)
}

// claim binds a job to the connection that started it. A second runner using
// the same name must never be able to report another node's run.
func (s *remoteJobState) claim(c *remoteConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owner != nil && s.owner != c {
		return false
	}
	s.owner = c
	return true
}

func (s *remoteJobState) ownedBy(c *remoteConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.owner == c
}

func (s *remoteJobState) setAutoPush(v bool) {
	s.mu.Lock()
	s.autoPush = v
	s.mu.Unlock()
}

func (s *remoteJobState) autoPushEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.autoPush
}

type remotePermission struct {
	conn        *remoteConn
	requestID   string
	runID       int64
	issueID     int64
	kind        string
	allowAlways map[string]bool
}

// RemoteRunnerInfo is returned by GET /api/runners.
type RemoteRunnerInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Online      bool   `json:"online"`
	ConnectedAt string `json:"connectedAt,omitempty"`
}

func newRemoteManager(cfg *config.Config, st *store.Store, r *runner.Runner, hub *Hub) *remoteManager {
	return &remoteManager{cfg: cfg, st: st, r: r, hub: hub,
		runners: map[string]*remoteConn{}, jobStates: map[int64]*remoteJobState{},
		perms: map[string]*remotePermission{}}
}

func (m *remoteManager) Close() {
	m.mu.Lock()
	conns := make([]*remoteConn, 0, len(m.runners))
	for _, c := range m.runners {
		conns = append(conns, c)
	}
	m.runners = map[string]*remoteConn{}
	m.mu.Unlock()
	for _, c := range conns {
		c.conn.Close(websocket.StatusGoingAway, "server shutting down")
	}
}

func (m *remoteManager) Online(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runners[strings.TrimSpace(id)] != nil
}

func (m *remoteManager) List() []RemoteRunnerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RemoteRunnerInfo, 0, len(m.runners))
	for _, c := range m.runners {
		out = append(out, RemoteRunnerInfo{
			ID: c.id, Name: c.name, Version: c.version, Online: true,
			ConnectedAt: c.joined.Format(time.RFC3339),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *remoteManager) conn(id string) *remoteConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runners[strings.TrimSpace(id)]
}

func (m *remoteManager) handleList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"runners": m.List()})
}

func (m *remoteManager) handleWS(w http.ResponseWriter, r *http.Request) {
	// The runner is a CLI, never a browser. Rejecting Origin prevents a
	// malicious page from opening a cross-site WebSocket to a local server.
	if strings.TrimSpace(r.Header.Get("Origin")) != "" {
		http.Error(w, "forbidden: browser origin not allowed on runner endpoint", http.StatusForbidden)
		return
	}
	// Without a server token, remote runners are only trusted on loopback.
	if strings.TrimSpace(m.cfg.Token) == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			http.Error(w, "forbidden: remote runner requires an access token", http.StatusForbidden)
			return
		}
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	conn.SetReadLimit(remote.MaxFrameBytes)
	defer conn.CloseNow()

	readCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	_, data, err := conn.Read(readCtx)
	cancel()
	if err != nil {
		return
	}
	var hello remote.Frame
	if err := json.Unmarshal(data, &hello); err != nil || hello.Type != "hello" {
		_ = writeFrame(r.Context(), conn, remote.Frame{Type: "error", Error: "expected hello"})
		return
	}
	name := strings.TrimSpace(hello.Name)
	if name == "" {
		_ = writeFrame(r.Context(), conn, remote.Frame{Type: "error", Error: "runner name is required"})
		return
	}
	c := &remoteConn{
		id: name, name: name, version: strings.TrimSpace(hello.Version),
		conn: conn, joined: time.Now(), pend: map[string]chan remoteCallResult{},
	}
	var old *remoteConn
	m.mu.Lock()
	if prev := m.runners[name]; prev != nil && prev.conn != conn {
		old = prev
	}
	m.runners[name] = c
	m.mu.Unlock()
	if old != nil {
		_ = old.conn.CloseNow()
	}
	defer m.unregister(c)

	if err := writeFrame(r.Context(), conn, remote.Frame{Type: "welcome", RunnerID: c.id, Version: strconv.Itoa(remote.Version)}); err != nil {
		return
	}

	// Detect a silently dead link (NAT timeout, sleeping laptop) without
	// waiting for the next job write.
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				pctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
				c.writeMu.Lock()
				err := conn.Ping(pctx)
				c.writeMu.Unlock()
				cancel()
				if err != nil {
					_ = conn.Close(websocket.StatusPolicyViolation, "ping timeout")
					return
				}
			}
		}
	}()

	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var f remote.Frame
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}
		m.handleFrame(r.Context(), c, f)
	}
}

func writeFrame(ctx context.Context, conn *websocket.Conn, f remote.Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

func (c *remoteConn) write(ctx context.Context, f remote.Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeFrame(ctx, c.conn, f)
}

func (c *remoteConn) call(ctx context.Context, method string, runID int64, params any) (json.RawMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	id := fmt.Sprintf("srv-%d-%d", runID, time.Now().UnixNano())
	ch := make(chan remoteCallResult, 1)
	c.pendMu.Lock()
	c.pend[id] = ch
	c.pendMu.Unlock()
	defer func() {
		c.pendMu.Lock()
		delete(c.pend, id)
		c.pendMu.Unlock()
	}()
	if err := c.write(ctx, remote.Frame{Type: "rpc", ID: id, Method: method, RunID: runID, Params: raw}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.result, res.err
	}
}

func (c *remoteConn) resolve(id string, res remoteCallResult) {
	c.pendMu.Lock()
	ch := c.pend[id]
	c.pendMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- res:
	default:
	}
}

func (c *remoteConn) failPending(err error) {
	c.pendMu.Lock()
	pend := c.pend
	c.pend = map[string]chan remoteCallResult{}
	c.pendMu.Unlock()
	for _, ch := range pend {
		select {
		case ch <- remoteCallResult{err: err}:
		default:
		}
	}
}

func (m *remoteManager) handleFrame(ctx context.Context, c *remoteConn, f remote.Frame) {
	switch f.Type {
	case "event":
		m.handleEvent(c, f)
	case "status":
		m.handleStatus(c, f)
	case "permission":
		m.handlePermission(c, f)
	case "rpc_result", "rpc_error":
		var err error
		if f.Type == "rpc_error" || f.Error != "" {
			err = errors.New(f.Error)
		}
		c.resolve(f.ID, remoteCallResult{result: f.Result, err: err})
	case "hello", "welcome", "pong":
	default:
	}
}

func (m *remoteManager) state(runID, issueID int64, runnerID string) *remoteJobState {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.jobsLocked(runID)
	if state == nil {
		state = &remoteJobState{runID: runID, issueID: issueID, runnerID: runnerID}
		m.jobStates[runID] = state
	}
	if runnerID != "" {
		state.runnerID = runnerID
	}
	if issueID != 0 {
		state.issueID = issueID
	}
	return state
}

// jobs is stored on remoteManager but declared through these helpers to keep
// the locking in one place.
func (m *remoteManager) jobsLocked(runID int64) *remoteJobState {
	if m.jobStates == nil {
		m.jobStates = map[int64]*remoteJobState{}
	}
	return m.jobStates[runID]
}

func isTerminalRunStatus(s domain.RunStatus) bool {
	switch s {
	case domain.RunCompleted, domain.RunFailed, domain.RunCancelled, domain.RunSubmitted, domain.RunDiscarded:
		return true
	}
	return false
}

// ownedState returns the job state only when both the persisted run and the
// in-memory job belong to c. A runner sharing the server token must not be able
// to report another node's run.
func (m *remoteManager) ownedState(c *remoteConn, run *domain.Run) (*remoteJobState, bool) {
	if run == nil || strings.TrimSpace(run.RunnerID) != c.id {
		return nil, false
	}
	state := m.state(run.ID, run.IssueID, c.id)
	if !state.claim(c) {
		return nil, false
	}
	return state, true
}

func (m *remoteManager) forgetState(c *remoteConn, runID int64) {
	m.mu.Lock()
	if state := m.jobsLocked(runID); state != nil && state.ownedBy(c) {
		delete(m.jobStates, runID)
	}
	m.mu.Unlock()
	m.clearPermissions(runID)
	m.r.ForgetRemoteRun(runID)
}

func (m *remoteManager) handleEvent(c *remoteConn, f remote.Frame) {
	if f.Event == nil {
		return
	}
	run, err := m.st.GetRun(context.Background(), f.RunID)
	if err != nil || isTerminalRunStatus(run.Status) {
		return
	}
	state, ok := m.ownedState(c, run)
	if !ok {
		return
	}
	if f.Event.Type == "message" {
		if txt, ok := f.Event.Data["text"].(string); ok {
			state.appendMessage(txt)
		}
	}
	if f.Event.Type == "usage" {
		m.r.RecordRemoteUsage(f.RunID, run.IssueID, f.Event.Data)
	}
	m.r.EmitRemoteEvent(f.RunID, run.IssueID, f.Event.Type, f.Event.Data)
}

func (m *remoteManager) handleStatus(c *remoteConn, f remote.Frame) {
	run, err := m.st.GetRun(context.Background(), f.RunID)
	if err != nil || isTerminalRunStatus(run.Status) {
		return
	}
	state, ok := m.ownedState(c, run)
	if !ok {
		return
	}
	issueID := run.IssueID
	m.r.UpdateRemoteWorkspace(f.RunID, f.Workdir, f.SessionID)
	if f.Usage != nil {
		m.r.RecordRemoteTurnUsage(f.RunID, issueID, f.Usage)
	}
	switch domain.RunStatus(f.Status) {
	case domain.RunStarting:
		m.r.SetRemoteRunStatus(f.RunID, issueID, domain.RunStarting, f.Workdir, f.SessionID, "", "")
		m.markIssueRunning(issueID)
	case domain.RunRunning:
		m.r.SetRemoteRunStatus(f.RunID, issueID, domain.RunRunning, f.Workdir, f.SessionID, "", "")
		m.markIssueRunning(issueID)
	case domain.RunCompleted:
		summary := state.summaryText()
		m.r.FinishRemoteRun(f.RunID, issueID, domain.RunCompleted, summary, "")
		m.maybeAutoSubmit(run, state)
		m.forgetState(c, f.RunID)
	case domain.RunFailed:
		msg := strings.TrimSpace(f.Error)
		if msg == "" {
			msg = "remote runner reported failure"
		}
		m.forgetState(c, f.RunID)
		m.r.FinishRemoteRun(f.RunID, issueID, domain.RunFailed, "", msg)
	case domain.RunCancelled:
		m.forgetState(c, f.RunID)
		m.r.FinishRemoteRun(f.RunID, issueID, domain.RunCancelled, "", "")
	}
}

func (m *remoteManager) markIssueRunning(issueID int64) {
	running := domain.IssueRunning
	if u, err := m.st.UpdateIssue(context.Background(), issueID, store.IssuePatch{Status: &running}); err == nil {
		m.hub.BroadcastIssueUpdated(u)
	}
}

func (m *remoteManager) handlePermission(c *remoteConn, f remote.Frame) {
	var req remote.PermissionRequest
	if err := json.Unmarshal(f.Params, &req); err != nil {
		_ = c.write(context.Background(), remote.Frame{Type: "permission_result", RequestID: f.RequestID, OptionID: ""})
		return
	}
	run, err := m.st.GetRun(context.Background(), f.RunID)
	if err != nil || isTerminalRunStatus(run.Status) {
		return
	}
	if _, ok := m.ownedState(c, run); !ok {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Tool execution permission"
	}
	kind := strings.TrimSpace(req.Kind)
	opts := make([]acp.PermissionOption, 0, len(req.Options))
	for _, o := range req.Options {
		opts = append(opts, acp.PermissionOption{OptionID: o.OptionID, Name: o.Name, Kind: o.Kind})
	}
	// Emit the same event shape as the local runner so the existing UI works.
	m.r.EmitRemoteEvent(f.RunID, run.IssueID, "permission_request", map[string]any{
		"requestId": f.RequestID, "title": title, "toolCall": req.ToolCall, "options": req.Options,
	})
	if option, auto := m.r.RemotePermissionDecision(f.RunID, title, kind, req.ToolCall, opts); auto {
		for _, o := range opts {
			if o.OptionID == option && o.Kind == "allow_always" {
				m.r.RememberRemoteAllow(f.RunID, kind)
			}
		}
		m.r.EmitRemoteEvent(f.RunID, run.IssueID, "permission_resolved", map[string]any{
			"requestId": f.RequestID, "optionId": option, "auto": true,
		})
		_ = c.write(context.Background(), remote.Frame{Type: "permission_result", RunID: f.RunID, RequestID: f.RequestID, OptionID: option})
		return
	}

	allowAlways := map[string]bool{}
	for _, o := range opts {
		if o.Kind == "allow_always" {
			allowAlways[o.OptionID] = true
		}
	}
	key := permissionKey(f.RunID, f.RequestID)
	m.mu.Lock()
	m.perms[key] = &remotePermission{conn: c, requestID: f.RequestID, runID: f.RunID, issueID: run.IssueID, kind: kind, allowAlways: allowAlways}
	m.mu.Unlock()
	m.r.SetRemoteRunStatus(f.RunID, run.IssueID, domain.RunAwaiting, run.Workdir, run.SessionID, "", "")
	m.r.NotifyRemoteRun("permission", fmt.Sprintf("Run #%d 等待授权审批", f.RunID), title, fmt.Sprintf("/issues/%d", run.IssueID))
}

func permissionKey(runID int64, requestID string) string {
	return fmt.Sprintf("%d|%s", runID, requestID)
}

func (m *remoteManager) clearPermissions(runID int64) {
	prefix := fmt.Sprintf("%d|", runID)
	m.mu.Lock()
	for key := range m.perms {
		if strings.HasPrefix(key, prefix) {
			delete(m.perms, key)
		}
	}
	m.mu.Unlock()
}

func (m *remoteManager) autoSubmit(runID int64, mode, message string) {
	run, err := m.st.GetRun(context.Background(), runID)
	if err != nil {
		return
	}
	proj, issue, err := m.runContext(context.Background(), run)
	if err != nil {
		return
	}
	req := runner.SubmitRequest{Mode: mode, CommitMessage: message}
	if _, err := m.Submit(context.Background(), run, proj, issue, req); err != nil {
		m.r.EmitRemoteEvent(runID, issue.ID, "system", map[string]any{"text": "远程自动提交失败: " + err.Error()})
		m.r.NotifyRemoteRun("run_failed", fmt.Sprintf("[%s] 远程自动提交失败", issue.Key), err.Error(), fmt.Sprintf("/issues/%d", issue.ID))
	}
}

func (m *remoteManager) maybeAutoSubmit(run *domain.Run, state *remoteJobState) {
	switch {
	case state != nil && state.autoPushEnabled():
		go m.autoSubmit(run.ID, "push", "chore: apply remote CI fix")
	case run.AutoPR && strings.TrimSpace(run.PRURL) == "":
		go m.autoSubmit(run.ID, "pr", "")
	}
}

func (m *remoteManager) runContext(ctx context.Context, run *domain.Run) (*domain.Project, *domain.Issue, error) {
	issue, err := m.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return nil, nil, err
	}
	proj, err := m.st.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	return proj, issue, nil
}

func (m *remoteManager) Start(ctx context.Context, job remote.Job) error {
	c := m.conn(job.RunnerID)
	if c == nil {
		return fmt.Errorf("%w: %s", runner.ErrRemoteRunnerOffline, job.RunnerID)
	}
	state := m.state(job.RunID, job.IssueID, job.RunnerID)
	if !state.claim(c) {
		return fmt.Errorf("run %d is owned by another runner", job.RunID)
	}
	state.setAutoPush(job.AutoPush)
	state.resetSummary()
	if err := c.write(ctx, remote.Frame{Type: "job_start", Job: &job}); err != nil {
		m.mu.Lock()
		delete(m.jobStates, job.RunID)
		m.mu.Unlock()
		return err
	}
	return nil
}

func (m *remoteManager) Send(ctx context.Context, job remote.Job) error {
	c := m.conn(job.RunnerID)
	if c == nil {
		return fmt.Errorf("%w: %s", runner.ErrRemoteRunnerOffline, job.RunnerID)
	}
	state := m.state(job.RunID, job.IssueID, job.RunnerID)
	if !state.claim(c) {
		return fmt.Errorf("run %d is owned by another runner", job.RunID)
	}
	state.setAutoPush(job.AutoPush)
	state.resetSummary()
	if err := c.write(ctx, remote.Frame{Type: "job_message", Job: &job}); err != nil {
		return err
	}
	return nil
}

func (m *remoteManager) Cancel(runID int64) error {
	m.mu.Lock()
	state := m.jobsLocked(runID)
	runnerID := ""
	if state != nil {
		runnerID = state.runnerID
	}
	m.mu.Unlock()
	if runnerID == "" {
		if run, err := m.st.GetRun(context.Background(), runID); err == nil {
			runnerID = run.RunnerID
		}
	}
	c := m.conn(runnerID)
	if c == nil {
		return fmt.Errorf("%w: %s", runner.ErrRemoteRunnerOffline, runnerID)
	}
	return c.write(context.Background(), remote.Frame{Type: "cancel", RunID: runID})
}

func (m *remoteManager) RespondPermission(runID int64, requestID, optionID string) error {
	key := permissionKey(runID, requestID)
	m.mu.Lock()
	p := m.perms[key]
	delete(m.perms, key)
	m.mu.Unlock()
	if p == nil {
		return errors.New("permission request not found or already resolved")
	}
	if err := p.conn.write(context.Background(), remote.Frame{Type: "permission_result", RunID: runID, RequestID: requestID, OptionID: optionID}); err != nil {
		return err
	}
	if optionID == "" {
		optionID = "cancelled"
	}
	if p.allowAlways[optionID] {
		m.r.RememberRemoteAllow(runID, p.kind)
	}
	m.r.EmitRemoteEvent(runID, p.issueID, "permission_resolved", map[string]any{
		"requestId": requestID, "optionId": optionID, "auto": false,
	})
	run, _ := m.st.GetRun(context.Background(), runID)
	workdir, session := "", ""
	if run != nil {
		workdir, session = run.Workdir, run.SessionID
	}
	m.r.SetRemoteRunStatus(runID, p.issueID, domain.RunRunning, workdir, session, "", "")
	return nil
}

func (m *remoteManager) Changes(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue) ([]runner.RepoChanges, error) {
	c := m.conn(run.RunnerID)
	if c == nil {
		return nil, fmt.Errorf("%w: %s", runner.ErrRemoteRunnerOffline, run.RunnerID)
	}
	raw, err := c.call(ctx, "changes", run.ID, struct{}{})
	if err != nil {
		return nil, err
	}
	var res remote.ChangesResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	out := make([]runner.RepoChanges, 0, len(res.Changes))
	for _, rc := range res.Changes {
		item := runner.RepoChanges{RepoID: rc.RepoID, RepoName: rc.RepoName, Branch: rc.Branch}
		for _, f := range rc.Files {
			item.Files = append(item.Files, gitx.FileChange{Path: f.Path, Status: f.Status, Additions: f.Additions, Deletions: f.Deletions})
		}
		out = append(out, item)
	}
	return out, nil
}

func (m *remoteManager) ReadFile(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue, repoID int64, relPath, side string) (string, error) {
	c := m.conn(run.RunnerID)
	if c == nil {
		return "", fmt.Errorf("%w: %s", runner.ErrRemoteRunnerOffline, run.RunnerID)
	}
	raw, err := c.call(ctx, "read_file", run.ID, map[string]any{"repoId": repoID, "path": relPath, "side": side})
	if err != nil {
		return "", err
	}
	var res struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	return res.Content, nil
}

func (m *remoteManager) WriteFile(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue, repoID int64, relPath, content string) error {
	c := m.conn(run.RunnerID)
	if c == nil {
		return fmt.Errorf("%w: %s", runner.ErrRemoteRunnerOffline, run.RunnerID)
	}
	_, err := c.call(ctx, "write_file", run.ID, map[string]any{"repoId": repoID, "path": relPath, "content": content})
	return err
}

func (m *remoteManager) Submit(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue, req runner.SubmitRequest) ([]runner.SubmitResult, error) {
	if req.Mode == "merge_local" {
		return nil, errors.New("远程 Runner 不支持直接合并目标分支；请使用 PR 或 push")
	}
	c := m.conn(run.RunnerID)
	if c == nil {
		return nil, fmt.Errorf("%w: %s", runner.ErrRemoteRunnerOffline, run.RunnerID)
	}
	target := strings.TrimSpace(req.TargetBranch)
	if target == "" {
		target = strings.TrimSpace(run.TargetBranch)
	}
	if target == "" {
		target = strings.TrimSpace(proj.TargetBranch)
	}
	raw, err := c.call(ctx, "submit", run.ID, map[string]any{"commitMessage": req.CommitMessage, "targetBranch": target})
	if err != nil {
		return nil, err
	}
	var remoteResults []remote.SubmitResult
	if err := json.Unmarshal(raw, &remoteResults); err != nil {
		return nil, err
	}
	commitMsg := strings.TrimSpace(req.CommitMessage)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("%s (%s)", issue.Title, issue.Key)
	}
	prTitle := strings.TrimSpace(req.Title)
	if prTitle == "" {
		prTitle = commitMsg
	}
	results := make([]runner.SubmitResult, 0, len(remoteResults))
	for _, rr := range remoteResults {
		repo, ok := repoByID(proj, rr.RepoID)
		res := runner.SubmitResult{
			RepoID: rr.RepoID, RepoName: rr.RepoName, Branch: rr.Branch,
			TargetBranch: rr.TargetBranch, Commit: rr.Commit, Error: rr.Error,
		}
		if res.TargetBranch == "" {
			res.TargetBranch = target
		}
		if res.TargetBranch == "" && ok {
			res.TargetBranch = repo.DefaultBranch
		}
		items := cleanItems(req.ChangeItems)
		if len(items) == 0 {
			items = []string{issue.Title}
			for i, f := range rr.Files {
				if i >= 6 {
					break
				}
				items = append(items, f.Path)
			}
		}
		if res.Error == "" && ok && req.Mode == "pr" {
			p := provider.For(repo.Provider)
			if p.Caps().PRs {
				pre := runner.FormatChangeRecord(items, run.Branch, res.TargetBranch, "(创建中)", req.Remark)
				body := strings.TrimSpace(req.Body)
				if body == "" {
					body = pre
					if issue.RemoteURL != "" {
						body += "\n\nRef: " + issue.RemoteURL
					}
				}
				url, perr := p.CreatePR(ctx, repo, run.Branch, res.TargetBranch, prTitle, body)
				if perr != nil {
					res.Error = perr.Error()
				} else {
					res.PRURL = url
					m.r.DispatchRemotePRCreated(proj, &repo, issue, url, prTitle, run.Branch, res.TargetBranch)
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
		res.ChangeRecord = runner.FormatChangeRecord(items, run.Branch, res.TargetBranch, prDisplay, req.Remark)
		results = append(results, res)
	}
	allOK, err := m.r.FinalizeRemoteSubmit(ctx, run, proj, issue, req, results)
	if err != nil {
		return results, err
	}
	if allOK && len(results) > 0 {
		if _, cerr := c.call(ctx, "discard", run.ID, struct{}{}); cerr == nil {
			empty := ""
			if u, uerr := m.st.UpdateRun(ctx, run.ID, store.RunUpdate{Workdir: &empty}); uerr == nil {
				m.hub.BroadcastRunUpdated(u)
			}
		}
	} else if !allOK && run.Workdir != "" {
		for i := range results {
			if results[i].Error != "" {
				results[i].Error += fmt.Sprintf(" (未推送修改保留在远端工作区: %s/%s)", run.Workdir, results[i].RepoName)
			}
		}
	}
	return results, nil
}

func repoByID(proj *domain.Project, id int64) (domain.Repo, bool) {
	for _, repo := range proj.Repos {
		if repo.ID == id {
			return repo, true
		}
	}
	return domain.Repo{}, false
}

func cleanItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s := strings.TrimSpace(it); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (m *remoteManager) Discard(ctx context.Context, run *domain.Run, proj *domain.Project) error {
	return m.cleanupRemote(ctx, run, "discard")
}

func (m *remoteManager) Delete(ctx context.Context, run *domain.Run, proj *domain.Project) error {
	return m.cleanupRemote(ctx, run, "delete")
}

// cleanupRemote removes the in-memory job and workspace. After a runner
// restart the job map is empty, so fall back to a path-scoped cleanup RPC.
func (m *remoteManager) cleanupRemote(ctx context.Context, run *domain.Run, method string) error {
	c := m.conn(run.RunnerID)
	if c == nil {
		return fmt.Errorf("%w: %s", runner.ErrRemoteRunnerOffline, run.RunnerID)
	}
	_, err := c.call(ctx, method, run.ID, struct{}{})
	if err == nil {
		m.forgetState(c, run.ID)
		return nil
	}
	if !strings.Contains(err.Error(), "not known") {
		return err
	}
	_, err = c.call(ctx, "cleanup", run.ID, map[string]string{"workdir": run.Workdir})
	if err == nil {
		m.forgetState(c, run.ID)
	}
	return err
}

func (m *remoteManager) unregister(c *remoteConn) {
	m.mu.Lock()
	if m.runners[c.id] == c {
		delete(m.runners, c.id)
	}
	var runs []*domain.Run
	for runID, state := range m.jobsLockedAll() {
		if !state.ownedBy(c) {
			continue
		}
		delete(m.jobStates, runID)
		if run, err := m.st.GetRun(context.Background(), runID); err == nil && isActiveRun(run.Status) {
			runs = append(runs, run)
		}
	}
	m.mu.Unlock()
	c.failPending(errors.New("remote runner disconnected"))
	m.mu.Lock()
	for key, p := range m.perms {
		if p.conn == c {
			delete(m.perms, key)
		}
	}
	m.mu.Unlock()
	for _, run := range runs {
		m.r.FinishRemoteRun(run.ID, run.IssueID, domain.RunFailed, "", "remote runner disconnected")
	}
}

func (m *remoteManager) jobsLockedAll() map[int64]*remoteJobState {
	if m.jobStates == nil {
		m.jobStates = map[int64]*remoteJobState{}
	}
	return m.jobStates
}

func isActiveRun(status domain.RunStatus) bool {
	switch status {
	case domain.RunQueued, domain.RunStarting, domain.RunRunning, domain.RunAwaiting:
		return true
	}
	return false
}

var _ runner.RemoteExecutor = (*remoteManager)(nil)
