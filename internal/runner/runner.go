// Package runner orchestrates worktrees, agent ACP processes, permission prompts, and review submissions.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/store"
)

// Notifier broadcasts state updates to connected WebSocket clients.
type Notifier interface {
	BroadcastRunEvent(runID, issueID int64, ev *domain.RunEvent)
	BroadcastRunUpdated(run *domain.Run)
	BroadcastIssueUpdated(issue *domain.Issue)
	BroadcastNotification(n *domain.Notification)
}

// AgentStatus describes a configured agent CLI and whether its binary is in PATH.
type AgentStatus struct {
	Name         string            `json:"name"`
	Command      string            `json:"command"`
	Args         []string          `json:"args"`
	Env          map[string]string `json:"env,omitempty"`
	MemoryMB     int               `json:"memoryMB"`
	DefaultModel string            `json:"defaultModel"`
	Models       []string          `json:"models"`
	Available    bool              `json:"available"`
}

// AgentModels describes the default model, providers, and available models for an agent.
type AgentModels struct {
	Agent        string            `json:"agent"`
	CurrentModel string            `json:"currentModel"`
	DefaultModel string            `json:"defaultModel"`
	CustomModels []string          `json:"customModels"`
	Providers    []string          `json:"providers"`
	Models       []acp.ModelOption `json:"models"`
}

// RepoChanges groups file diffs per repository for the review screen.
type RepoChanges struct {
	RepoID   int64             `json:"repoId"`
	RepoName string            `json:"repoName"`
	Branch   string            `json:"branch"`
	Files    []gitx.FileChange `json:"files"`
}

// SubmitRequest commits and pushes (and optionally opens PRs) across modified repos.
type SubmitRequest struct {
	Mode          string   `json:"mode"` // "pr" | "push"
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	CommitMessage string   `json:"commitMessage"`
	TargetBranch  string   `json:"targetBranch"`
	ChangeItems   []string `json:"changeItems"`
	Remark        string   `json:"remark"`
}

// SubmitResult is the outcome of submitting one repository.
type SubmitResult struct {
	RepoID       int64  `json:"repoId"`
	RepoName     string `json:"repoName"`
	Branch       string `json:"branch"`
	TargetBranch string `json:"targetBranch"`
	Commit       string `json:"commit"`
	PRURL        string `json:"prUrl"`
	ChangeRecord string `json:"changeRecord"`
	Error        string `json:"error"`
}

type activeRun struct {
	runID     int64
	issueID   int64
	clientMu  sync.Mutex
	client    *acp.Client
	cancel    context.CancelFunc
	done      chan struct{}
	permMu    sync.Mutex
	perms     map[string]chan string // requestID -> optionID
	toolMu    sync.Mutex
	openTools map[string]string // toolCallId -> last non-terminal status
	reqSeq    atomic.Int64
	msgMu     sync.Mutex
	msgBuf    strings.Builder
}

type warmSession struct {
	client *acp.Client
	timer  *time.Timer
}

type cachedAgentModels struct {
	data      *AgentModels
	fetchedAt time.Time
}

// Runner manages local execution of runs.
type Runner struct {
	cfg        *config.Config
	st         *store.Store
	notify     Notifier
	budget     *budget
	mu         sync.Mutex
	active     map[int64]*activeRun   // runID -> activeRun
	warm       map[int64]*warmSession // runID -> idle warm ACP process
	allowSet   map[int64]bool         // runID -> allow_always chosen
	watchMu    sync.Mutex
	modelMu    sync.Mutex
	modelCache map[string]cachedAgentModels
}

func New(cfg *config.Config, st *store.Store, notify Notifier) *Runner {
	r := &Runner{
		cfg:        cfg,
		st:         st,
		notify:     notify,
		budget:     newBudget(cfg.Runner.MaxConcurrent, cfg.Runner.MemoryBudgetMB),
		active:     map[int64]*activeRun{},
		warm:       map[int64]*warmSession{},
		allowSet:   map[int64]bool{},
		modelCache: map[string]cachedAgentModels{},
	}
	// Mark any runs left active by a previous server process as failed and sync their parent issues.
	if runs, err := st.ListActiveRuns(context.Background()); err == nil {
		failed := domain.RunFailed
		issFailed := domain.IssueFailed
		errMsg := "server restarted while run was active"
		for _, run := range runs {
			_, _ = st.UpdateRun(context.Background(), run.ID, store.RunUpdate{
				Status: &failed, Error: &errMsg, Ended: true,
			})
			_, _ = st.AppendEvent(context.Background(), run.ID, "status", map[string]any{"status": failed})
			if iss, ierr := st.GetIssue(context.Background(), run.IssueID); ierr == nil && iss.Status == domain.IssueRunning {
				_, _ = st.UpdateIssue(context.Background(), iss.ID, store.IssuePatch{Status: &issFailed})
			}
		}
	}
	go r.startWatcherLoop()
	return r
}

// ListAgents returns configured agents and whether their command is installed.
func (r *Runner) ListAgents() []AgentStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AgentStatus, 0, len(r.cfg.Agents))
	for name, ac := range r.cfg.Agents {
		_, err := exec.LookPath(ac.Command)
		args := ac.Args
		if args == nil {
			args = []string{}
		}
		models := ac.Models
		if models == nil {
			models = []string{}
		}
		out = append(out, AgentStatus{
			Name:         name,
			Command:      ac.Command,
			Args:         args,
			Env:          ac.Env,
			MemoryMB:     ac.MemoryMB,
			DefaultModel: ac.DefaultModel,
			Models:       models,
			Available:    err == nil,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// UpsertAgent creates or updates an agent configuration and persists it to ~/.xissue/config.yaml.
func (r *Runner) UpsertAgent(name string, ac config.AgentConfig) (*AgentStatus, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("agent name is required")
	}
	ac.Command = strings.TrimSpace(ac.Command)
	if ac.Command == "" {
		return nil, errors.New("command is required")
	}
	ac.DefaultModel = strings.TrimSpace(ac.DefaultModel)
	cleanedModels := make([]string, 0, len(ac.Models))
	seen := map[string]bool{}
	for _, m := range ac.Models {
		m = strings.TrimSpace(m)
		if m != "" && !seen[m] {
			seen[m] = true
			cleanedModels = append(cleanedModels, m)
		}
	}
	ac.Models = cleanedModels
	if ac.MemoryMB <= 0 {
		ac.MemoryMB = 500
	}

	r.mu.Lock()
	if r.cfg.Agents == nil {
		r.cfg.Agents = map[string]config.AgentConfig{}
	}
	r.cfg.Agents[name] = ac
	err := r.cfg.Save()
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}

	r.modelMu.Lock()
	delete(r.modelCache, name)
	r.modelMu.Unlock()

	_, lookErr := exec.LookPath(ac.Command)
	return &AgentStatus{
		Name:         name,
		Command:      ac.Command,
		Args:         ac.Args,
		Env:          ac.Env,
		MemoryMB:     ac.MemoryMB,
		DefaultModel: ac.DefaultModel,
		Models:       ac.Models,
		Available:    lookErr == nil,
	}, nil
}

// DeleteAgent removes an agent from ~/.xissue/config.yaml.
func (r *Runner) DeleteAgent(name string) error {
	r.mu.Lock()
	delete(r.cfg.Agents, name)
	err := r.cfg.Save()
	r.mu.Unlock()
	if err != nil {
		return err
	}
	r.modelMu.Lock()
	delete(r.modelCache, name)
	r.modelMu.Unlock()
	return nil
}

// ReloadAgents reloads ~/.xissue/config.yaml from disk and clears the model discovery cache.
func (r *Runner) ReloadAgents() ([]AgentStatus, error) {
	r.mu.Lock()
	err := r.cfg.Reload()
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	r.modelMu.Lock()
	r.modelCache = map[string]cachedAgentModels{}
	r.modelMu.Unlock()
	return r.ListAgents(), nil
}

// ListAgentModels probes an agent via ACP (and local config files + bound custom models) for available providers and models.
func (r *Runner) ListAgentModels(ctx context.Context, agentName string, refresh bool) (*AgentModels, error) {
	r.mu.Lock()
	ac, ok := r.cfg.Agents[agentName]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", agentName)
	}

	if !refresh {
		r.modelMu.Lock()
		cached, ok := r.modelCache[agentName]
		r.modelMu.Unlock()
		if ok && time.Since(cached.fetchedAt) < 10*time.Minute && len(cached.data.Models) > 0 {
			return cached.data, nil
		}
	}

	pctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	var currentModel string
	byVal := map[string]acp.ModelOption{}

	// 0. Merge user-bound custom models from xIssue AgentConfig
	customModels := ac.Models
	if customModels == nil {
		customModels = []string{}
	}
	for _, raw := range customModels {
		val := strings.TrimSpace(raw)
		if val == "" {
			continue
		}
		prov := "custom"
		if idx := strings.IndexByte(val, '/'); idx > 0 {
			prov = val[:idx]
		}
		byVal[val] = acp.ModelOption{
			Value:    val,
			Name:     val,
			Provider: prov,
		}
	}
	if ac.DefaultModel != "" {
		val := strings.TrimSpace(ac.DefaultModel)
		prov := "custom"
		if idx := strings.IndexByte(val, '/'); idx > 0 {
			prov = val[:idx]
		}
		if _, exists := byVal[val]; !exists {
			byVal[val] = acp.ModelOption{
				Value:    val,
				Name:     val,
				Provider: prov,
			}
		}
	}

	// 1. If opencode, also merge any custom providers/models declared in ~/.config/opencode/opencode.json
	if agentName == "opencode" || filepath.Base(ac.Command) == "opencode" {
		if home, err := os.UserHomeDir(); err == nil {
			for _, cfgName := range []string{"opencode.json", "config.json"} {
				p := filepath.Join(home, ".config", "opencode", cfgName)
				if b, err := os.ReadFile(p); err == nil {
					var oc struct {
						Model     any `json:"model"`
						Providers map[string]struct {
							Models map[string]struct {
								Name string `json:"name"`
							} `json:"models"`
						} `json:"providers"`
						Provider map[string]struct {
							Models map[string]struct {
								Name string `json:"name"`
							} `json:"models"`
						} `json:"provider"`
					}
					if json.Unmarshal(b, &oc) == nil {
						if s, ok := oc.Model.(string); ok && s != "" && currentModel == "" {
							currentModel = s
						}
						mergeProvs := func(pm map[string]struct {
							Models map[string]struct {
								Name string `json:"name"`
							} `json:"models"`
						}) {
							for prov, pdef := range pm {
								for mid, mdef := range pdef.Models {
									val := prov + "/" + mid
									name := mdef.Name
									if name == "" {
										name = mid
									}
									byVal[val] = acp.ModelOption{
										Value:    val,
										Name:     prov + "/" + name,
										Provider: prov,
									}
								}
							}
						}
						mergeProvs(oc.Providers)
						mergeProvs(oc.Provider)
					}
				}
			}
		}
	}

	// 2. Probe via ACP initialize + session/new if binary is installed
	if _, err := exec.LookPath(ac.Command); err == nil {
		probeDir := r.cfg.DataDir
		if err := os.MkdirAll(probeDir, 0o755); err != nil {
			probeDir = os.TempDir()
		}
		client, err := acp.Start(pctx, acp.Options{
			Command: ac.Command,
			Args:    ac.Args,
			Env:     ac.Env,
			Dir:     probeDir,
		}, acp.Handlers{})
		if err == nil {
			defer client.Close()
			if err := client.NewSession(pctx, probeDir); err == nil {
				// For opencode, if background service just booted and returned few models, retry once after a short pause
				if (agentName == "opencode" || filepath.Base(ac.Command) == "opencode") && len(client.ModelOptions) <= 40 {
					time.Sleep(800 * time.Millisecond)
					_ = client.NewSession(pctx, probeDir)
				}
				if client.CurrentModel != "" {
					currentModel = client.CurrentModel
				}
				for _, m := range client.ModelOptions {
					byVal[m.Value] = m
				}
			}
		}
	}

	if ac.DefaultModel != "" {
		currentModel = ac.DefaultModel
	}

	models := make([]acp.ModelOption, 0, len(byVal))
	provSet := map[string]bool{}
	for _, m := range byVal {
		models = append(models, m)
		if m.Provider != "" {
			provSet[m.Provider] = true
		}
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return models[i].Value < models[j].Value
	})
	providers := make([]string, 0, len(provSet))
	for p := range provSet {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	res := &AgentModels{
		Agent:        agentName,
		CurrentModel: currentModel,
		DefaultModel: ac.DefaultModel,
		CustomModels: customModels,
		Providers:    providers,
		Models:       models,
	}
	if len(models) > 0 {
		r.modelMu.Lock()
		r.modelCache[agentName] = cachedAgentModels{data: res, fetchedAt: time.Now()}
		r.modelMu.Unlock()
	}
	return res, nil
}

// StartRun creates a new Run record and launches the agent asynchronously.
func (r *Runner) StartRun(ctx context.Context, issueID int64, agentName, modelName, extraPrompt string) (*domain.Run, error) {
	r.mu.Lock()
	ac, ok := r.cfg.Agents[agentName]
	for _, a := range r.active {
		if a.issueID == issueID {
			r.mu.Unlock()
			return nil, errors.New("this issue already has an active run in progress")
		}
	}
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", agentName)
	}
	if _, err := exec.LookPath(ac.Command); err != nil {
		return nil, fmt.Errorf("agent binary %q not found in PATH", ac.Command)
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" && ac.DefaultModel != "" {
		modelName = strings.TrimSpace(ac.DefaultModel)
	}
	issue, err := r.st.GetIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	proj, err := r.st.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return nil, err
	}
	run, err := r.st.CreateRun(ctx, issueID, agentName, modelName)
	if err != nil {
		return nil, err
	}
	branch := fmt.Sprintf("agent/%s-run-%d", sanitizeBranch(issue.Key), run.ID)
	workdir := filepath.Join(r.cfg.RunsDir(), fmt.Sprintf("run-%d", run.ID))
	run, err = r.st.UpdateRun(ctx, run.ID, store.RunUpdate{Branch: &branch, Workdir: &workdir})
	if err != nil {
		return nil, err
	}

	act, ok := r.reserveActiveRun(run.ID, issue.ID)
	if !ok {
		return nil, errors.New("run is already active")
	}

	running := domain.IssueRunning
	if u, err := r.st.UpdateIssue(ctx, issueID, store.IssuePatch{Status: &running}); err == nil {
		r.notify.BroadcastIssueUpdated(u)
	}
	r.notify.BroadcastRunUpdated(run)

	prompt := buildInitialPrompt(proj, issue, extraPrompt)
	if strings.TrimSpace(extraPrompt) != "" {
		r.emitEvent(run.ID, issue.ID, "user", map[string]any{"text": extraPrompt})
	}
	go r.executeTurn(act, proj, issue, ac, prompt, false)
	return run, nil
}

// SendMessage sends a follow-up instruction to a running or completed run (resuming in the same workdir).
func (r *Runner) SendMessage(ctx context.Context, runID int64, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("empty message")
	}
	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return err
	}
	proj, err := r.st.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	ac, ok := r.cfg.Agents[run.Agent]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown agent %q", run.Agent)
	}

	act, reserved := r.reserveActiveRun(run.ID, issue.ID)
	if !reserved {
		return errors.New("agent is currently busy; cancel or wait for the current turn to finish")
	}

	r.emitEvent(run.ID, issue.ID, "user", map[string]any{"text": text})
	running := domain.IssueRunning
	if u, err := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &running}); err == nil {
		r.notify.BroadcastIssueUpdated(u)
	}
	go r.executeTurn(act, proj, issue, ac, text, true)
	return nil
}

func (r *Runner) reserveActiveRun(runID, issueID int64) (*activeRun, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active[runID] != nil {
		return nil, false
	}
	act := &activeRun{
		runID:     runID,
		issueID:   issueID,
		done:      make(chan struct{}),
		perms:     map[string]chan string{},
		openTools: map[string]string{},
	}
	r.active[runID] = act
	return act, true
}

func (r *Runner) takeWarmClient(runID int64) *acp.Client {
	r.mu.Lock()
	w := r.warm[runID]
	delete(r.warm, runID)
	r.mu.Unlock()
	if w == nil {
		return nil
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	if w.client != nil && w.client.Alive() {
		return w.client
	}
	if w.client != nil {
		w.client.Close()
	}
	return nil
}

func (r *Runner) putWarmClient(runID int64, client *acp.Client, ttl time.Duration) {
	if client == nil || !client.Alive() {
		if client != nil {
			client.Close()
		}
		return
	}
	client.SetHandlers(acp.Handlers{})
	r.mu.Lock()
	if old := r.warm[runID]; old != nil {
		if old.timer != nil {
			old.timer.Stop()
		}
		if old.client != nil && old.client != client {
			go old.client.Close()
		}
	}
	r.warm[runID] = &warmSession{
		client: client,
		timer: time.AfterFunc(ttl, func() {
			r.closeWarmClient(runID)
		}),
	}
	r.mu.Unlock()
}

func (r *Runner) closeWarmClient(runID int64) {
	if c := r.takeWarmClient(runID); c != nil {
		c.Close()
	}
}

// Shutdown terminates all active and warm agent processes gracefully before server exit/restart.
func (r *Runner) Shutdown() {
	r.mu.Lock()
	activeIDs := make([]int64, 0, len(r.active))
	for id := range r.active {
		activeIDs = append(activeIDs, id)
	}
	warmIDs := make([]int64, 0, len(r.warm))
	for id := range r.warm {
		warmIDs = append(warmIDs, id)
	}
	r.mu.Unlock()

	for _, id := range activeIDs {
		_ = r.Cancel(id)
	}
	for _, id := range warmIDs {
		r.closeWarmClient(id)
	}
}

// RespondPermission delivers the user's permission decision to a waiting run.
func (r *Runner) RespondPermission(runID int64, requestID, optionID string) error {
	r.mu.Lock()
	act := r.active[runID]
	r.mu.Unlock()
	if act == nil {
		return errors.New("run is not active")
	}
	act.permMu.Lock()
	ch := act.perms[requestID]
	act.permMu.Unlock()
	if ch == nil {
		return errors.New("permission request not found or already resolved")
	}
	select {
	case ch <- optionID:
	default:
	}
	return nil
}

// Cancel terminates an active run and waits briefly for the turn goroutine to finish.
func (r *Runner) Cancel(runID int64) error {
	r.closeWarmClient(runID)
	r.mu.Lock()
	act := r.active[runID]
	r.mu.Unlock()
	if act == nil {
		return errors.New("run is not active")
	}
	act.clientMu.Lock()
	cl := act.client
	act.clientMu.Unlock()
	if cl != nil {
		cl.Cancel()
	}
	if act.cancel != nil {
		act.cancel()
	}
	select {
	case <-act.done:
	case <-time.After(3 * time.Second):
	}
	return nil
}

func (r *Runner) trackToolEvent(act *activeRun, ev acp.Event) {
	if ev.Type != "tool_call" && ev.Type != "tool_call_update" {
		return
	}
	id, _ := ev.Data["toolCallId"].(string)
	if id == "" {
		return
	}
	st, _ := ev.Data["status"].(string)
	act.toolMu.Lock()
	defer act.toolMu.Unlock()
	if st == "completed" || st == "failed" {
		delete(act.openTools, id)
	} else {
		if st == "" {
			st = "in_progress"
		}
		act.openTools[id] = st
	}
}

func (r *Runner) settleOpenTools(act *activeRun, finalStatus string) {
	act.toolMu.Lock()
	ids := make([]string, 0, len(act.openTools))
	for id := range act.openTools {
		ids = append(ids, id)
	}
	act.openTools = map[string]string{}
	act.toolMu.Unlock()

	for _, id := range ids {
		r.emitEvent(act.runID, act.issueID, "tool_call_update", map[string]any{
			"toolCallId": id,
			"status":     finalStatus,
		})
	}
}

func (r *Runner) executeTurn(act *activeRun, proj *domain.Project, issue *domain.Issue, ac config.AgentConfig, prompt string, isResume bool) {
	runID := act.runID
	issueID := act.issueID
	ctx, cancel := context.WithCancel(context.Background())
	act.cancel = cancel
	defer func() {
		if rec := recover(); rec != nil {
			r.settleOpenTools(act, "failed")
			r.failRun(runID, issueID, fmt.Sprintf("agent runner panic: %v", rec))
		}
		cancel()
		r.mu.Lock()
		delete(r.active, runID)
		r.mu.Unlock()
		close(act.done)
	}()

	memMB := ac.MemoryMB
	if memMB <= 0 {
		memMB = 400
	}
	if err := r.budget.acquire(ctx, memMB); err != nil {
		r.setRunStatus(runID, issueID, domain.RunCancelled, false, true)
		return
	}
	defer r.budget.release(memMB)

	r.setRunStatus(runID, issueID, domain.RunStarting, true, false)

	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		r.failRun(runID, issueID, fmt.Sprintf("load run: %v", err))
		return
	}
	if !isResume {
		if err := r.prepareWorkspace(ctx, run, proj, issue); err != nil {
			r.failRun(runID, issueID, fmt.Sprintf("workspace setup failed: %v", err))
			return
		}
	} else if _, err := os.Stat(run.Workdir); err != nil {
		if err := r.prepareWorkspace(ctx, run, proj, issue); err != nil {
			r.failRun(runID, issueID, fmt.Sprintf("workspace setup failed: %v", err))
			return
		}
	}

	var wrap []string
	if r.cfg.Runner.UseSystemdRun && runtime.GOOS == "linux" && ac.MemoryMB > 0 {
		if _, err := exec.LookPath("systemd-run"); err == nil {
			wrap = []string{"systemd-run", "--user", "--scope", "-q", fmt.Sprintf("-pMemoryMax=%dM", ac.MemoryMB), "--"}
		}
	}

	handlers := acp.Handlers{
		OnEvent: func(ev acp.Event) {
			if ev.Type == "message" {
				if t, ok := ev.Data["text"].(string); ok {
					act.msgMu.Lock()
					act.msgBuf.WriteString(t)
					act.msgMu.Unlock()
				}
			}
			r.trackToolEvent(act, ev)
			r.emitEvent(runID, issueID, ev.Type, ev.Data)
		},
		OnPermission: func(pctx context.Context, req acp.PermissionRequest) string {
			return r.handlePermission(pctx, act, req)
		},
	}

	var client *acp.Client
	var sessPrompt string
	reusedWarm := false

	if isResume {
		if warmClient := r.takeWarmClient(runID); warmClient != nil {
			warmClient.SetHandlers(handlers)
			client = warmClient
			sessPrompt = prompt
			reusedWarm = true
			r.emitEvent(runID, issueID, "system", map[string]any{
				"text": "♻️ 复用当前常驻 ACP 会话进程（免重启、直接延续上下文）",
			})
		}
	}

	if client == nil {
		startedClient, err := acp.Start(ctx, acp.Options{
			Command: ac.Command,
			Args:    ac.Args,
			Env:     ac.Env,
			Dir:     run.Workdir,
			Wrap:    wrap,
		}, handlers)
		if err != nil {
			r.failRun(runID, issueID, fmt.Sprintf("start agent: %v", err))
			return
		}
		client = startedClient
	}

	act.clientMu.Lock()
	act.client = client
	act.clientMu.Unlock()

	keepWarm := false
	defer func() {
		if keepWarm && client != nil && client.Alive() {
			r.putWarmClient(runID, client, 20*time.Minute)
		} else if client != nil {
			client.Close()
		}
	}()

	if !reusedWarm {
		if isResume && run.SessionID != "" && client.CanLoad {
			if err := client.LoadSession(ctx, run.Workdir, run.SessionID); err == nil {
				sessPrompt = prompt
				r.emitEvent(runID, issueID, "system", map[string]any{
					"text": fmt.Sprintf("♻️ 已通过 ACP session/load 恢复会话 (%s)", run.SessionID),
				})
			}
		}
		if sessPrompt == "" {
			if err := client.NewSession(ctx, run.Workdir); err != nil {
				r.failRun(runID, issueID, fmt.Sprintf("session/new: %v", err))
				return
			}
			sid := client.SessionID
			_, _ = r.st.UpdateRun(context.Background(), runID, store.RunUpdate{SessionID: &sid})
			if isResume {
				sessPrompt = fmt.Sprintf("Continuing work on [%s] %s in this existing workspace.\n\nUser feedback / follow-up:\n%s", issue.Key, issue.Title, prompt)
			} else {
				sessPrompt = prompt
			}
		}

		if targetModel := strings.TrimSpace(run.Model); targetModel != "" {
			var setErr error
			for attempt := 0; attempt < 4; attempt++ {
				setErr = client.SetModel(ctx, targetModel)
				if setErr == nil {
					break
				}
				// opencode v2 background server may need ~500ms after first boot to finish loading custom providers
				if strings.Contains(setErr.Error(), "model not found") && attempt < 3 {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				break
			}
			if setErr != nil {
				r.failRun(runID, issueID, fmt.Sprintf("set model %q: %v", targetModel, setErr))
				return
			}
			r.emitEvent(runID, issueID, "system", map[string]any{
				"text": fmt.Sprintf("Using model: %s (%s)", targetModel, run.Agent),
			})
		} else if client.CurrentModel != "" {
			cur := client.CurrentModel
			if u, err := r.st.UpdateRun(context.Background(), runID, store.RunUpdate{Model: &cur}); err == nil {
				run = u
				r.notify.BroadcastRunUpdated(u)
			}
			r.emitEvent(runID, issueID, "system", map[string]any{
				"text": fmt.Sprintf("Using default model: %s (%s)", cur, run.Agent),
			})
		}
	}

	r.setRunStatus(runID, issueID, domain.RunRunning, false, false)

	_, promptErr := client.Prompt(ctx, sessPrompt)
	if ctx.Err() != nil {
		r.settleOpenTools(act, "failed")
		r.setRunStatus(runID, issueID, domain.RunCancelled, false, true)
		return
	}
	if promptErr != nil {
		r.settleOpenTools(act, "failed")
		r.failRun(runID, issueID, promptErr.Error())
		return
	}

	r.settleOpenTools(act, "completed")
	keepWarm = true

	act.msgMu.Lock()
	summary := strings.TrimSpace(act.msgBuf.String())
	act.msgMu.Unlock()
	if len(summary) > 4000 {
		summary = summary[len(summary)-4000:]
	}
	done := domain.RunCompleted
	updatedRun, _ := r.st.UpdateRun(context.Background(), runID, store.RunUpdate{
		Status: &done, Summary: &summary, Ended: true,
	})
	if updatedRun != nil {
		r.emitEvent(runID, issueID, "status", map[string]any{"status": done})
		r.notify.BroadcastRunUpdated(updatedRun)
	}

	review := domain.IssueReview
	if u, err := r.st.UpdateIssue(context.Background(), issueID, store.IssuePatch{Status: &review}); err == nil {
		r.notify.BroadcastIssueUpdated(u)
	}

	r.writeRunSummaryComment(context.Background(), proj, issueID, run, summary)
	r.ExportIssueMarkdown(context.Background(), issueID)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = r.ArchiveRun(context.Background(), run.ID)
	}()
	r.pushNotification("run_done", fmt.Sprintf("[%s] 执行完成，待验收", issue.Key), issue.Title, fmt.Sprintf("/issues/%d", issueID), "")
}

func (r *Runner) prepareWorkspace(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue) error {
	if err := os.MkdirAll(run.Workdir, 0o755); err != nil {
		return err
	}
	for _, repo := range proj.Repos {
		r.emitEvent(run.ID, issue.ID, "system", map[string]any{
			"text": fmt.Sprintf("Preparing repository %s (%s @ %s)…", repo.Name, repo.RemoteURL, repo.DefaultBranch),
		})
		mirror := filepath.Join(r.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repo.ID))
		if err := gitx.EnsureMirror(ctx, repo.RemoteURL, mirror); err != nil {
			return fmt.Errorf("mirror %s: %w", repo.Name, err)
		}
		wtDir := filepath.Join(run.Workdir, repo.Name)
		if _, err := os.Stat(wtDir); err == nil {
			continue
		}
		if err := gitx.AddWorktree(ctx, mirror, wtDir, run.Branch, repo.DefaultBranch); err != nil {
			return fmt.Errorf("worktree %s: %w", repo.Name, err)
		}
	}
	agentsMD := buildWorkspaceAGENTS(proj, issue)
	_ = os.WriteFile(filepath.Join(run.Workdir, "AGENTS.md"), []byte(agentsMD), 0o644)
	return nil
}

func (r *Runner) handlePermission(ctx context.Context, act *activeRun, req acp.PermissionRequest) string {
	title, _ := req.ToolCall["title"].(string)
	kind, _ := req.ToolCall["kind"].(string)
	if title == "" {
		title = "Tool execution permission"
	}
	reqID := fmt.Sprintf("perm-%d", act.reqSeq.Add(1))

	// Check policy for auto-approval.
	if opt := r.autoApproveOption(act.runID, kind, title, req.ToolCall, req.Options); opt != "" {
		r.emitEvent(act.runID, act.issueID, "permission_request", map[string]any{
			"requestId": reqID, "title": title, "toolCall": req.ToolCall, "options": req.Options,
		})
		r.emitEvent(act.runID, act.issueID, "permission_resolved", map[string]any{
			"requestId": reqID, "optionId": opt, "auto": true,
		})
		return opt
	}

	ch := make(chan string, 1)
	act.permMu.Lock()
	act.perms[reqID] = ch
	act.permMu.Unlock()
	defer func() {
		act.permMu.Lock()
		delete(act.perms, reqID)
		act.permMu.Unlock()
	}()

	r.emitEvent(act.runID, act.issueID, "permission_request", map[string]any{
		"requestId": reqID, "title": title, "toolCall": req.ToolCall, "options": req.Options,
	})
	r.setRunStatus(act.runID, act.issueID, domain.RunAwaiting, false, false)
	r.pushNotification("permission", fmt.Sprintf("Run #%d 等待授权审批", act.runID), title, fmt.Sprintf("/issues/%d", act.issueID), "")

	var chosen string
	select {
	case <-ctx.Done():
		r.emitEvent(act.runID, act.issueID, "permission_resolved", map[string]any{
			"requestId": reqID, "optionId": "cancelled", "auto": true,
		})
		return ""
	case chosen = <-ch:
	}

	for _, o := range req.Options {
		if o.OptionID == chosen && o.Kind == "allow_always" {
			r.mu.Lock()
			r.allowSet[act.runID] = true
			r.mu.Unlock()
		}
	}
	r.emitEvent(act.runID, act.issueID, "permission_resolved", map[string]any{
		"requestId": reqID, "optionId": chosen, "auto": false,
	})
	r.setRunStatus(act.runID, act.issueID, domain.RunRunning, false, false)
	return chosen
}

func (r *Runner) autoApproveOption(runID int64, kind, title string, tc map[string]any, opts []acp.PermissionOption) string {
	rawText := fmt.Sprintf("%s %v", title, tc["rawInput"])
	r.mu.Lock()
	alwaysAsk := append([]string(nil), r.cfg.Permissions.AlwaysAskPatterns...)
	autoAllow := append([]string(nil), r.cfg.Permissions.AutoAllowKinds...)
	always := r.allowSet[runID]
	r.mu.Unlock()

	for _, pat := range alwaysAsk {
		if pat != "" && strings.Contains(rawText, pat) {
			return ""
		}
	}

	allowedKind := always
	if !allowedKind {
		for _, k := range autoAllow {
			if strings.EqualFold(k, kind) {
				allowedKind = true
				break
			}
		}
	}
	if !allowedKind {
		return ""
	}
	for _, o := range opts {
		if o.Kind == "allow_once" || o.Kind == "allow_always" {
			return o.OptionID
		}
	}
	return ""
}

func (r *Runner) writeRunSummaryComment(ctx context.Context, proj *domain.Project, issueID int64, run *domain.Run, summary string) {
	var changed []string
	for _, repo := range proj.Repos {
		wt := filepath.Join(run.Workdir, repo.Name)
		if files, err := gitx.Changes(ctx, wt, repo.DefaultBranch); err == nil && len(files) > 0 {
			changed = append(changed, fmt.Sprintf("`%s` (%d files)", repo.Name, len(files)))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Run #%d** (`%s`, branch `%s`) completed.\n\n", run.ID, run.Agent, run.Branch)
	if len(changed) > 0 {
		fmt.Fprintf(&b, "- Modified repositories: %s\n\n", strings.Join(changed, ", "))
	} else {
		b.WriteString("- No file changes detected.\n\n")
	}
	if summary != "" {
		b.WriteString(summary)
	}
	_, _ = r.st.AddComment(ctx, issueID, "agent", b.String())
}

// ---------- Diff & Submit ----------

func (r *Runner) Changes(ctx context.Context, runID int64) ([]RepoChanges, error) {
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := make([]RepoChanges, 0, len(proj.Repos))
	for _, repo := range proj.Repos {
		wt := filepath.Join(run.Workdir, repo.Name)
		files, err := gitx.Changes(ctx, wt, repo.DefaultBranch)
		if err != nil || files == nil {
			files = []gitx.FileChange{}
		}
		out = append(out, RepoChanges{
			RepoID:   repo.ID,
			RepoName: repo.Name,
			Branch:   run.Branch,
			Files:    files,
		})
	}
	return out, nil
}

func (r *Runner) ReadFile(ctx context.Context, runID, repoID int64, relPath, side string) (string, error) {
	run, repo, wt, abs, err := r.resolveRepoFile(ctx, runID, repoID, relPath)
	if err != nil {
		return "", err
	}
	_ = run
	if side == "base" {
		return gitx.ShowBase(ctx, wt, repo.DefaultBranch, relPath)
	}
	b, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func (r *Runner) WriteFile(ctx context.Context, runID, repoID int64, relPath, content string) error {
	_, _, _, abs, err := r.resolveRepoFile(ctx, runID, repoID, relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0o644)
}

func (r *Runner) Submit(ctx context.Context, runID int64, req SubmitRequest) ([]SubmitResult, error) {
	r.mu.Lock()
	busy := r.active[runID] != nil
	r.mu.Unlock()
	if busy {
		return nil, errors.New("cannot submit while agent is still running")
	}
	r.closeWarmClient(runID)

	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return nil, err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
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

	var results []SubmitResult
	var prLinks []string
	var firstPRURL string
	var firstTargetBranch string
	var lastChangeRecord string
	anyPushed := false

	for _, repo := range proj.Repos {
		wt := filepath.Join(run.Workdir, repo.Name)
		files, err := gitx.Changes(ctx, wt, repo.DefaultBranch)
		if err != nil || len(files) == 0 {
			continue
		}
		targetBranch := strings.TrimSpace(req.TargetBranch)
		if targetBranch == "" {
			targetBranch = repo.DefaultBranch
		}
		if targetBranch == "" {
			targetBranch = "main"
		}
		res := SubmitResult{
			RepoID:       repo.ID,
			RepoName:     repo.Name,
			Branch:       run.Branch,
			TargetBranch: targetBranch,
		}
		sha, err := gitx.CommitAll(ctx, wt, commitMsg)
		if err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		res.Commit = sha
		if err := gitx.Push(ctx, wt, run.Branch); err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		anyPushed = true
		if firstTargetBranch == "" {
			firstTargetBranch = targetBranch
		}

		items := cleanChangeItems(req.ChangeItems)
		if len(items) == 0 {
			items = deriveChangeItems(issue.Title, files)
		}

		if req.Mode == "pr" {
			p := provider.For(repo.Provider)
			if p.Caps().PRs {
				preRecord := FormatChangeRecord(items, run.Branch, targetBranch, "(创建中)", req.Remark)
				prBody := strings.TrimSpace(req.Body)
				if prBody == "" {
					prBody = preRecord
					if issue.RemoteURL != "" {
						prBody += "\n\nRef: " + issue.RemoteURL
					}
				}
				url, err := p.CreatePR(ctx, repo, run.Branch, targetBranch, prTitle, prBody)
				if err != nil {
					res.Error = err.Error()
				} else {
					res.PRURL = url
					if firstPRURL == "" {
						firstPRURL = url
					}
					prLinks = append(prLinks, fmt.Sprintf("- `%s`: %s", repo.Name, url))
				}
			}
		}

		record := FormatChangeRecord(items, run.Branch, targetBranch, res.PRURL, req.Remark)
		res.ChangeRecord = record
		lastChangeRecord = record
		results = append(results, res)
	}

	if anyPushed {
		st := domain.RunSubmitted
		prState := ""
		if firstPRURL != "" {
			prState = "open"
		}
		if u, err := r.st.UpdateRun(ctx, run.ID, store.RunUpdate{
			Status:       &st,
			TargetBranch: &firstTargetBranch,
			PRURL:        &firstPRURL,
			PRState:      &prState,
			ChangeRecord: &lastChangeRecord,
		}); err == nil {
			r.notify.BroadcastRunUpdated(u)
		}
		ist := domain.IssueDone
		if len(prLinks) > 0 {
			ist = domain.IssuePROpen
		}
		if u, err := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &ist}); err == nil {
			r.notify.BroadcastIssueUpdated(u)
		}
		note := lastChangeRecord
		if note == "" {
			note = fmt.Sprintf("Submitted branch `%s`.", run.Branch)
		}
		_, _ = r.st.AddComment(ctx, issue.ID, "system", "```text\n"+note+"\n```")
		if issue.Source == "remote" && issue.RepoID != nil && issue.RemoteNumber != nil {
			if repo, err := r.st.GetRepo(ctx, *issue.RepoID); err == nil {
				_ = provider.For(repo.Provider).Comment(ctx, *repo, *issue.RemoteNumber, note)
			}
		}
		r.ExportIssueMarkdown(ctx, issue.ID)
		_, _ = r.ArchiveRun(ctx, run.ID)
		r.cleanupRunWorkdir(ctx, run, proj)
		empty := ""
		if u, err := r.st.UpdateRun(ctx, run.ID, store.RunUpdate{Workdir: &empty}); err == nil {
			r.notify.BroadcastRunUpdated(u)
		}
		if firstPRURL != "" {
			r.pushNotification("pr_status", fmt.Sprintf("[%s] PR 已创建 (Open)", issue.Key), firstPRURL, fmt.Sprintf("/issues/%d", issue.ID), "")
		}
	}
	return results, nil
}

func (r *Runner) cleanupRunWorkdir(ctx context.Context, run *domain.Run, proj *domain.Project) {
	cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = ctx
	for _, repo := range proj.Repos {
		mirror := filepath.Join(r.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repo.ID))
		wt := filepath.Join(run.Workdir, repo.Name)
		_ = gitx.RemoveWorktree(cctx, mirror, wt, run.Branch)
	}
	if run.Workdir != "" {
		_ = os.RemoveAll(run.Workdir)
	}
}

func (r *Runner) Discard(ctx context.Context, runID int64) error {
	_ = r.Cancel(runID)
	r.mu.Lock()
	delete(r.allowSet, runID)
	r.mu.Unlock()
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return err
	}
	_, _ = r.ArchiveRun(ctx, run.ID)
	r.cleanupRunWorkdir(ctx, run, proj)
	empty := ""
	st := domain.RunDiscarded
	if u, err := r.st.UpdateRun(ctx, run.ID, store.RunUpdate{Status: &st, Workdir: &empty, Ended: true}); err == nil {
		r.notify.BroadcastRunUpdated(u)
	}
	return nil
}

// Delete removes the run's worktree/branch on disk and deletes the run record.
func (r *Runner) Delete(ctx context.Context, runID int64) error {
	_ = r.Cancel(runID)
	r.mu.Lock()
	delete(r.allowSet, runID)
	r.mu.Unlock()
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return err
	}
	_, _ = r.ArchiveRun(ctx, run.ID)
	r.cleanupRunWorkdir(ctx, run, proj)
	if err := r.st.DeleteRun(ctx, run.ID); err != nil {
		return err
	}
	// If the issue was stuck in failed/running and has no active run remaining, reset it to todo.
	if issue, err := r.st.GetIssue(ctx, run.IssueID); err == nil {
		hasActive := false
		if runs, rerr := r.st.ListRuns(ctx, issue.ID); rerr == nil {
			for _, rem := range runs {
				if rem.Status == domain.RunQueued || rem.Status == domain.RunStarting || rem.Status == domain.RunRunning || rem.Status == domain.RunAwaiting {
					hasActive = true
					break
				}
			}
		}
		if !hasActive && (issue.Status == domain.IssueFailed || issue.Status == domain.IssueRunning) {
			todo := domain.IssueTodo
			if u, err := r.st.UpdateIssue(ctx, issue.ID, store.IssuePatch{Status: &todo}); err == nil {
				issue = u
			}
		}
		r.notify.BroadcastIssueUpdated(issue)
		r.ExportIssueMarkdown(ctx, issue.ID)
	}
	return nil
}

// ---------- helpers ----------

func (r *Runner) runAndProject(ctx context.Context, runID int64) (*domain.Run, *domain.Project, error) {
	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return nil, nil, err
	}
	proj, err := r.st.GetProject(ctx, issue.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	return run, proj, nil
}

func (r *Runner) resolveRepoFile(ctx context.Context, runID, repoID int64, relPath string) (*domain.Run, *domain.Repo, string, string, error) {
	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, "", "", err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return nil, nil, "", "", err
	}
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return nil, nil, "", "", err
	}
	if repo.ProjectID != issue.ProjectID {
		return nil, nil, "", "", errors.New("repository does not belong to this project")
	}
	for _, seg := range strings.Split(filepath.ToSlash(relPath), "/") {
		if seg == ".git" || seg == ".." {
			return nil, nil, "", "", errors.New("invalid file path")
		}
	}
	wt := filepath.Clean(filepath.Join(run.Workdir, repo.Name))
	abs := filepath.Clean(filepath.Join(wt, relPath))
	if abs != wt && !strings.HasPrefix(abs, wt+string(filepath.Separator)) {
		return nil, nil, "", "", errors.New("invalid file path")
	}
	if realWT, err := filepath.EvalSymlinks(wt); err == nil {
		if realAbs, err := filepath.EvalSymlinks(abs); err == nil {
			if realAbs != realWT && !strings.HasPrefix(realAbs, realWT+string(filepath.Separator)) {
				return nil, nil, "", "", errors.New("symlink escapes repository worktree")
			}
		} else if realParent, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
			if realParent != realWT && !strings.HasPrefix(realParent, realWT+string(filepath.Separator)) {
				return nil, nil, "", "", errors.New("symlink escapes repository worktree")
			}
		}
	}
	return run, repo, wt, abs, nil
}

func (r *Runner) emitEvent(runID, issueID int64, typ string, data any) {
	ev, err := r.st.AppendEvent(context.Background(), runID, typ, data)
	if err == nil {
		r.notify.BroadcastRunEvent(runID, issueID, ev)
	}
}

func (r *Runner) setRunStatus(runID, issueID int64, st domain.RunStatus, started, ended bool) {
	u, err := r.st.UpdateRun(context.Background(), runID, store.RunUpdate{Status: &st, Started: started, Ended: ended})
	if err == nil {
		r.emitEvent(runID, issueID, "status", map[string]any{"status": st})
		r.notify.BroadcastRunUpdated(u)
	}
	if issue, err := r.st.GetIssue(context.Background(), issueID); err == nil {
		r.notify.BroadcastIssueUpdated(issue)
	}
}

func (r *Runner) failRun(runID, issueID int64, msg string) {
	st := domain.RunFailed
	u, err := r.st.UpdateRun(context.Background(), runID, store.RunUpdate{Status: &st, Error: &msg, Ended: true})
	if err == nil {
		r.emitEvent(runID, issueID, "stderr", map[string]any{"text": msg})
		r.emitEvent(runID, issueID, "status", map[string]any{"status": st})
		r.notify.BroadcastRunUpdated(u)
	}
	ist := domain.IssueFailed
	if issue, err := r.st.UpdateIssue(context.Background(), issueID, store.IssuePatch{Status: &ist}); err == nil {
		r.notify.BroadcastIssueUpdated(issue)
		r.pushNotification("run_done", fmt.Sprintf("[%s] 执行失败", issue.Key), msg, fmt.Sprintf("/issues/%d", issueID), "")
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = r.ArchiveRun(context.Background(), runID)
	}()
}

var nonBranchChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeBranch(s string) string {
	s = strings.ToLower(nonBranchChars.ReplaceAllString(s, "-"))
	return strings.Trim(s, "-")
}

func buildInitialPrompt(proj *domain.Project, issue *domain.Issue, extra string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Please resolve issue [%s]: %s\n\n", issue.Key, issue.Title)
	if strings.TrimSpace(issue.Body) != "" {
		b.WriteString("## Issue Description\n\n")
		b.WriteString(issue.Body + "\n\n")
	}
	b.WriteString("## Workspace Repositories\n\n")
	for _, repo := range proj.Repos {
		fmt.Fprintf(&b, "- `./%s/` (base branch `%s`", repo.Name, repo.DefaultBranch)
		if repo.TestCommand != "" {
			fmt.Fprintf(&b, ", test command: `%s`", repo.TestCommand)
		}
		b.WriteString(")\n")
	}
	if strings.TrimSpace(extra) != "" {
		b.WriteString("\n## Additional Instructions\n\n" + extra + "\n")
	}
	b.WriteString("\nMake the required changes inside the repository directory/directories above, run relevant tests, and summarize what you changed.\n")
	b.WriteString("IMPORTANT SAFETY RULES:\n")
	b.WriteString("- Do NOT run `git push`; xIssue handles committing, pushing, and opening PRs after review.\n")
	b.WriteString("- Do NOT run `pkill xissue`, `killall xissue`, `make stop`, `make restart`, `make deploy`, or `install-service.sh`, and do NOT bind to port 7789 — xIssue itself is supervising your process on port 7789.\n")
	return b.String()
}
