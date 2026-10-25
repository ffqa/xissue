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
	"github.com/xpier/xissue/internal/automation"
	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
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
	Warning      string            `json:"warning,omitempty"`
	ProbeError   string            `json:"probeError,omitempty"`
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
	runID        int64
	issueID      int64
	clientMu     sync.Mutex
	client       *acp.Client
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	permMu       sync.Mutex
	perms        map[string]chan string // requestID -> optionID
	toolMu       sync.Mutex
	openTools    map[string]string // toolCallId -> last non-terminal status
	reqSeq       atomic.Int64
	msgMu        sync.Mutex
	msgBuf       strings.Builder
	streamMu     sync.Mutex
	streams      map[string]*streamBuffer
	streamTimer  *time.Timer
	usageMu      sync.Mutex
	usageSawCost bool
}

// queuedTurn is a prepared agent turn waiting for a concurrency/memory slot.
// Turns are dispatched FIFO by a single scheduler goroutine.
type queuedTurn struct {
	act      *activeRun
	proj     *domain.Project
	issue    *domain.Issue
	ac       config.AgentConfig
	prompt   string
	isResume bool
	autoPush bool // CI auto-fix: push the existing branch after the turn
}

type ciFixAttempt struct {
	head     string
	attempts int
	at       time.Time
}

// ErrQueueFull is returned when the configured pending-run limit is reached.
var ErrQueueFull = errors.New("run queue is full")

type streamBuffer struct {
	data map[string]any
	text strings.Builder
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
	cfg             *config.Config
	st              *store.Store
	notify          Notifier
	budget          *budget
	queueMu         sync.Mutex
	queue           []*queuedTurn
	queueReserved   int
	queueSignal     chan struct{}
	ciFixMu         sync.Mutex
	ciFixAttempts   map[int64]ciFixAttempt
	mu              sync.Mutex
	active          map[int64]*activeRun      // runID -> activeRun
	warm            map[int64]*warmSession    // runID -> idle warm ACP process
	allowSet        map[int64]map[string]bool // runID -> allowed ACP tool kinds
	ctx             context.Context
	cancel          context.CancelFunc
	stopOnce        sync.Once
	watchMu         sync.Mutex
	watchWG         sync.WaitGroup
	retentionMu     sync.Mutex
	modelMu         sync.Mutex
	modelCache      map[string]cachedAgentModels
	monitorMu       sync.RWMutex
	logMu           sync.Mutex
	monitorLogs     []MonitorLogEntry
	lastCheckTime   time.Time
	nextCheckTime   time.Time
	lastDurationMs  int64
	lastTrigWatches int
	lastUpdatedPRs  int
	automation      *automation.Engine
}

func New(cfg *config.Config, st *store.Store, notify Notifier) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{
		cfg:           cfg,
		st:            st,
		notify:        notify,
		ctx:           ctx,
		cancel:        cancel,
		budget:        newBudget(cfg.Runner.MaxConcurrent, cfg.Runner.MemoryBudgetMB),
		queueSignal:   make(chan struct{}, 1),
		ciFixAttempts: map[int64]ciFixAttempt{},
		active:        map[int64]*activeRun{},
		warm:          map[int64]*warmSession{},
		allowSet:      map[int64]map[string]bool{},
		modelCache:    map[string]cachedAgentModels{},
		monitorLogs:   []MonitorLogEntry{},
		automation:    automation.NewEngine(cfg.PluginsDir()),
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
	go r.queueLoop()
	r.watchWG.Add(1)
	go func() {
		defer r.watchWG.Done()
		r.startWatcherLoop()
	}()
	r.setupLifecycleObservers()
	return r
}

// ListAgents returns configured agents and whether their command is installed.
func (r *Runner) ListAgents() []AgentStatus {
	agents := r.cfg.AgentsSnapshot()
	out := make([]AgentStatus, 0, len(agents))
	for name, ac := range agents {
		_, err := LookupAgentBinary(ac.Command)
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
			Env:          maskSensitiveEnv(ac.Env),
			MemoryMB:     ac.MemoryMB,
			DefaultModel: ac.DefaultModel,
			Models:       models,
			Available:    err == nil,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func maskSensitiveEnv(env map[string]string) map[string]string {
	if env == nil {
		return map[string]string{}
	}
	res := make(map[string]string, len(env))
	for k, v := range env {
		lk := strings.ToUpper(k)
		if strings.Contains(lk, "KEY") || strings.Contains(lk, "SECRET") || strings.Contains(lk, "TOKEN") || strings.Contains(lk, "PASS") || strings.Contains(lk, "AUTH") {
			if len(v) > 0 {
				res[k] = "******"
				continue
			}
		}
		res[k] = v
	}
	return res
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

	oldAgent, existed := r.cfg.Agent(name)
	if existed && oldAgent.Env != nil && ac.Env != nil {
		for k, v := range ac.Env {
			if v == "******" && oldAgent.Env[k] != "" {
				ac.Env[k] = oldAgent.Env[k]
			}
		}
	}
	if err := r.cfg.SetAgent(name, ac); err != nil {
		return nil, err
	}

	r.modelMu.Lock()
	delete(r.modelCache, name)
	r.modelMu.Unlock()

	_, lookErr := LookupAgentBinary(ac.Command)
	return &AgentStatus{
		Name:         name,
		Command:      ac.Command,
		Args:         ac.Args,
		Env:          maskSensitiveEnv(ac.Env),
		MemoryMB:     ac.MemoryMB,
		DefaultModel: ac.DefaultModel,
		Models:       ac.Models,
		Available:    lookErr == nil,
	}, nil
}

// DeleteAgent removes an agent from ~/.xissue/config.yaml.
func (r *Runner) DeleteAgent(name string) error {
	if err := r.cfg.DeleteAgent(name); err != nil {
		return err
	}
	r.modelMu.Lock()
	delete(r.modelCache, name)
	r.modelMu.Unlock()
	return nil
}

// ReloadAgents reloads ~/.xissue/config.yaml from disk and clears the model discovery cache.
func (r *Runner) ReloadAgents() ([]AgentStatus, error) {
	if err := r.cfg.Reload(); err != nil {
		return nil, err
	}
	r.modelMu.Lock()
	r.modelCache = map[string]cachedAgentModels{}
	r.modelMu.Unlock()
	return r.ListAgents(), nil
}

// ListAgentModels probes an agent via ACP (and local config files + bound custom models) for available providers and models.
func (r *Runner) ListAgentModels(ctx context.Context, agentName string, refresh bool) (*AgentModels, error) {
	ac, ok := r.cfg.Agent(agentName)
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

	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var currentModel string
	var probeErr string
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

	// 1. If opencode, also merge any custom providers/models declared in opencode config
	if agentName == "opencode" || filepath.Base(ac.Command) == "opencode" {
		home, _ := os.UserHomeDir()
		var candidateConfigs []string
		if home != "" {
			candidateConfigs = append(candidateConfigs,
				filepath.Join(home, ".config", "opencode", "opencode.json"),
				filepath.Join(home, ".config", "opencode", "config.json"),
			)
		}
		if runtime.GOOS == "windows" {
			if appData := os.Getenv("APPDATA"); appData != "" {
				candidateConfigs = append(candidateConfigs,
					filepath.Join(appData, "opencode", "opencode.json"),
					filepath.Join(appData, "opencode", "config.json"),
				)
			}
		}

		for _, p := range candidateConfigs {
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

	// 2. Probe via ACP initialize + session/new if binary is installed
	binPath, lookErr := LookupAgentBinary(ac.Command)
	if lookErr != nil {
		probeErr = fmt.Sprintf("未在系统 PATH 或常用目录中找到命令 %q，请确认已安装并在终端中可用", ac.Command)
	} else {
		probeDir := r.cfg.DataDir
		if err := os.MkdirAll(probeDir, 0o755); err != nil {
			probeDir = os.TempDir()
		}
		client, err := acp.Start(pctx, acp.Options{
			Command: binPath,
			Args:    ac.Args,
			Env:     ac.Env,
			Dir:     probeDir,
		}, acp.Handlers{})
		if err != nil {
			probeErr = fmt.Sprintf("启动 Agent 失败 (%s): %v", strings.Join(append([]string{ac.Command}, ac.Args...), " "), err)
		} else {
			defer client.Close()
			if err := client.NewSession(pctx, probeDir); err != nil {
				probeErr = fmt.Sprintf("ACP 会话握手失败: %v", err)
			} else {
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

	if probeErr == "" && len(models) == 0 {
		probeErr = fmt.Sprintf("Agent %q 握手成功，但未返回可用模型。请确认已在 Agent 中登录或配置至少一个 Provider / API Key", agentName)
	}

	res := &AgentModels{
		Agent:        agentName,
		CurrentModel: currentModel,
		DefaultModel: ac.DefaultModel,
		CustomModels: customModels,
		Providers:    providers,
		Models:       models,
		ProbeError:   probeErr,
	}
	if len(models) > 0 {
		r.modelMu.Lock()
		r.modelCache[agentName] = cachedAgentModels{data: res, fetchedAt: time.Now()}
		r.modelMu.Unlock()
	}
	return res, nil
}

// reserveQueueSlot reserves capacity for one pending turn. It is separate
// from commitQueueSlot so StartRun can create the run row without racing the
// configured queue limit.
func (r *Runner) reserveQueueSlot() bool {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()
	if r.ctx != nil && r.ctx.Err() != nil {
		return false
	}
	limit := r.cfg.Runner.MaxQueuedRuns
	pending := len(r.queue) + r.queueReserved
	if r.budget != nil {
		pending += r.budget.waitingCount()
	}
	if limit > 0 && pending >= limit {
		return false
	}
	r.queueReserved++
	return true
}

func (r *Runner) releaseQueueSlot() {
	r.queueMu.Lock()
	if r.queueReserved > 0 {
		r.queueReserved--
	}
	r.queueMu.Unlock()
}

func (r *Runner) commitQueueSlot(t *queuedTurn) {
	r.queueMu.Lock()
	if r.queueReserved > 0 {
		r.queueReserved--
	}
	r.queue = append(r.queue, t)
	r.queueMu.Unlock()
	r.signalQueue()
}

func (r *Runner) signalQueue() {
	if r.queueSignal == nil {
		return
	}
	select {
	case r.queueSignal <- struct{}{}:
	default:
	}
}

func (r *Runner) nextQueuedTurn() *queuedTurn {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()
	if len(r.queue) == 0 {
		return nil
	}
	t := r.queue[0]
	r.queue[0] = nil
	r.queue = r.queue[1:]
	return t
}

func (r *Runner) pendingTurns() int {
	r.queueMu.Lock()
	n := len(r.queue)
	r.queueMu.Unlock()
	// The dispatcher pops a turn before blocking on the budget, so include
	// that head-of-line turn while it waits for a slot.
	if r.budget == nil {
		return n
	}
	return n + r.budget.waitingCount()
}

// queueLoop is the single dispatcher: it pops turns FIFO and only starts an
// ACP process after the count/memory budget admits it. This makes concurrency
// limits a real queue instead of an arbitrary goroutine race.
func (r *Runner) queueLoop() {
	for {
		if r.ctx != nil && r.ctx.Err() != nil {
			r.drainQueue()
			return
		}
		t := r.nextQueuedTurn()
		if t == nil {
			if r.queueSignal == nil {
				return
			}
			select {
			case <-r.queueSignal:
				continue
			case <-r.ctx.Done():
				r.drainQueue()
				return
			}
		}
		memMB := t.ac.MemoryMB
		if memMB <= 0 {
			memMB = 400
		}
		if err := r.budget.acquire(t.act.ctx, memMB); err != nil {
			r.abortQueuedTurn(t)
			continue
		}
		if !t.isResume {
			go r.dispatchRunEvent(t.act.runID, automation.EventRunStarted)
		}
		go func(t *queuedTurn, memMB int) {
			defer r.budget.release(memMB)
			r.executeTurn(t.act, t.proj, t.issue, t.ac, t.prompt, t.isResume, t.autoPush)
		}(t, memMB)
	}
}

func (r *Runner) drainQueue() {
	for {
		t := r.nextQueuedTurn()
		if t == nil {
			break
		}
		r.abortQueuedTurn(t)
	}
	r.queueMu.Lock()
	r.queueReserved = 0
	r.queueMu.Unlock()
}

// abortQueuedTurn finishes a turn that was cancelled before its agent process
// was launched. executeTurn never runs, so its normal cleanup is done here.
func (r *Runner) abortQueuedTurn(t *queuedTurn) {
	act := t.act
	act.cancel()
	st := domain.RunCancelled
	if u, err := r.st.UpdateRun(context.Background(), act.runID, store.RunUpdate{Status: &st, Ended: true}); err == nil {
		r.emitEvent(act.runID, act.issueID, "status", map[string]any{"status": st})
		r.notify.BroadcastRunUpdated(u)
	}
	if issue, err := r.st.GetIssue(context.Background(), act.issueID); err == nil {
		r.notify.BroadcastIssueUpdated(issue)
	}
	r.mu.Lock()
	delete(r.active, act.runID)
	delete(r.allowSet, act.runID)
	r.mu.Unlock()
	close(act.done)
	go r.dispatchRunEvent(act.runID, automation.EventRunFinished)
	if r.st != nil {
		go func() {
			time.Sleep(300 * time.Millisecond)
			_, _ = r.ArchiveRun(context.Background(), act.runID)
		}()
	}
}

// QueueStats reports scheduler state for the status bar.
func (r *Runner) QueueStats() (pending, running, maxConcurrent, maxQueued int) {
	pending = r.pendingTurns()
	if r.budget != nil {
		running = r.budget.running()
	}
	maxConcurrent = r.cfg.Runner.MaxConcurrent
	maxQueued = r.cfg.Runner.MaxQueuedRuns
	return
}

// StartRun creates a new Run record and launches the agent asynchronously.
func (r *Runner) StartRun(ctx context.Context, issueID int64, agentName, modelName, extraPrompt string) (*domain.Run, error) {
	return r.startRun(ctx, issueID, agentName, modelName, extraPrompt, false)
}

// StartAutoDevelopRun starts a run whose changes are submitted as a PR (never
// merged) once the agent finishes.
func (r *Runner) StartAutoDevelopRun(ctx context.Context, issueID int64, agentName, extraPrompt string) (*domain.Run, error) {
	return r.startRun(ctx, issueID, agentName, "", extraPrompt, true)
}

func (r *Runner) startRun(ctx context.Context, issueID int64, agentName, modelName, extraPrompt string, autoPR bool) (*domain.Run, error) {
	ac, ok := r.cfg.Agent(agentName)
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", agentName)
	}
	r.mu.Lock()
	for _, a := range r.active {
		if a.issueID == issueID {
			r.mu.Unlock()
			return nil, errors.New("this issue already has an active run in progress")
		}
	}
	r.mu.Unlock()
	binPath, err := LookupAgentBinary(ac.Command)
	if err != nil {
		return nil, fmt.Errorf("agent binary %q not found in PATH or standard user directories", ac.Command)
	}
	ac.Command = binPath
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
	if !r.reserveQueueSlot() {
		return nil, ErrQueueFull
	}
	slotHeld := true
	defer func() {
		if slotHeld {
			r.releaseQueueSlot()
		}
	}()
	run, err := r.st.CreateRun(ctx, issueID, agentName, modelName)
	if err != nil {
		return nil, err
	}
	branch := fmt.Sprintf("agent/%s-run-%d", sanitizeBranch(issue.Key), run.ID)
	workdir := filepath.Join(r.cfg.RunsDir(), fmt.Sprintf("run-%d", run.ID))
	upd := store.RunUpdate{Branch: &branch, Workdir: &workdir}
	if autoPR {
		upd.AutoPR = &autoPR
	}
	run, err = r.st.UpdateRun(ctx, run.ID, upd)
	if err != nil {
		return nil, err
	}

	act, ok := r.reserveActiveRun(run.ID, issue.ID)
	if !ok {
		return nil, errors.New("run is already active")
	}

	r.notify.BroadcastRunUpdated(run)

	prompt := buildInitialPrompt(proj, issue, extraPrompt)
	if strings.TrimSpace(extraPrompt) != "" {
		r.emitEvent(run.ID, issue.ID, "user", map[string]any{"text": extraPrompt})
	}
	r.commitQueueSlot(&queuedTurn{
		act:    act,
		proj:   proj,
		issue:  issue,
		ac:     ac,
		prompt: prompt,
	})
	slotHeld = false
	return run, nil
}

// SendMessage sends a follow-up instruction to a running or completed run (resuming in the same workdir).
func (r *Runner) SendMessage(ctx context.Context, runID int64, text string) error {
	return r.sendMessage(ctx, runID, text, false)
}

func (r *Runner) sendMessage(ctx context.Context, runID int64, text string, autoPush bool) error {
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
	ac, ok := r.cfg.Agent(run.Agent)
	if !ok {
		return fmt.Errorf("unknown agent %q", run.Agent)
	}
	if !r.reserveQueueSlot() {
		return ErrQueueFull
	}
	slotHeld := true
	defer func() {
		if slotHeld {
			r.releaseQueueSlot()
		}
	}()

	act, reserved := r.reserveActiveRun(run.ID, issue.ID)
	if !reserved {
		return errors.New("agent is currently busy; cancel or wait for the current turn to finish")
	}

	r.emitEvent(run.ID, issue.ID, "user", map[string]any{"text": text})
	queued := domain.RunQueued
	if u, err := r.st.UpdateRun(ctx, run.ID, store.RunUpdate{Status: &queued, Reopen: true}); err == nil {
		r.emitEvent(run.ID, issue.ID, "status", map[string]any{"status": queued})
		r.notify.BroadcastRunUpdated(u)
	}
	r.commitQueueSlot(&queuedTurn{
		act:      act,
		proj:     proj,
		issue:    issue,
		ac:       ac,
		prompt:   text,
		isResume: true,
		autoPush: autoPush,
	})
	slotHeld = false
	return nil
}

func (r *Runner) reserveActiveRun(runID, issueID int64) (*activeRun, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active[runID] != nil {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	act := &activeRun{
		runID:     runID,
		issueID:   issueID,
		ctx:       ctx,
		cancel:    cancel,
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
	r.stopOnce.Do(func() {
		if r.st != nil {
			r.st.SetIssueObservers(nil, nil)
		}
		if r.cancel != nil {
			r.cancel()
		}
	})

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
	// Let watcher children exit before the caller removes the data directory
	// (also keeps tests and shutdown free of late monitor.log writes).
	r.watchWG.Wait()
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

// executeTurn runs one prepared turn. The caller (queueLoop) must already hold
// a budget slot; the slot is released after executeTurn returns.
func (r *Runner) executeTurn(act *activeRun, proj *domain.Project, issue *domain.Issue, ac config.AgentConfig, prompt string, isResume, autoPush bool) {
	runID := act.runID
	issueID := act.issueID
	ctx := act.ctx
	autoSubmit := false
	autoPushReady := false
	defer func() {
		r.flushStreams(act)
		if rec := recover(); rec != nil {
			r.settleOpenTools(act, "failed")
			r.failRun(runID, issueID, fmt.Sprintf("agent runner panic: %v", rec))
		}
		act.cancel()
		r.mu.Lock()
		delete(r.active, runID)
		delete(r.allowSet, runID)
		r.mu.Unlock()
		close(act.done)
		if autoSubmit && !isResume {
			go r.autoSubmitPR(runID)
		}
		if autoPush && autoPushReady {
			// Submit() refuses while the run is active, so wait until the defer
			// has removed it from the active map.
			go r.autoPushCIFix(runID)
		}
	}()

	if err := ctx.Err(); err != nil {
		r.setRunStatus(runID, issueID, domain.RunCancelled, false, true)
		return
	}
	running := domain.IssueRunning
	if u, err := r.st.UpdateIssue(context.Background(), issueID, store.IssuePatch{Status: &running}); err == nil {
		r.notify.BroadcastIssueUpdated(u)
	}
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
	if r.cfg.RunnerSnapshot().UseSystemdRun && runtime.GOOS == "linux" && ac.MemoryMB > 0 {
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
				r.queueStreamEvent(act, ev.Type, ev.Data)
				return
			}
			if ev.Type == "stderr" {
				r.queueStreamEvent(act, ev.Type, ev.Data)
				return
			}
			if ev.Type == "usage" {
				r.recordUsage(act, runID, issueID, ev.Data)
				r.flushStreams(act)
				r.emitEvent(runID, issueID, ev.Type, ev.Data)
				return
			}
			r.flushStreams(act)
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

	pres, promptErr := client.Prompt(ctx, sessPrompt)
	if pres.Usage != nil {
		r.recordTurnUsage(act, runID, issueID, pres.Usage)
	}
	if ctx.Err() != nil {
		r.settleOpenTools(act, "failed")
		r.flushStreams(act)
		r.setRunStatus(runID, issueID, domain.RunCancelled, false, true)
		return
	}
	if promptErr != nil {
		r.settleOpenTools(act, "failed")
		r.flushStreams(act)
		r.failRun(runID, issueID, promptErr.Error())
		return
	}

	r.settleOpenTools(act, "completed")
	r.flushStreams(act)
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

	r.writeRunSummaryComment(context.Background(), proj, issue, run, summary)
	r.ExportIssueMarkdown(context.Background(), issueID)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = r.ArchiveRun(context.Background(), run.ID)
	}()
	r.pushNotification("run_done", fmt.Sprintf("[%s] 执行完成，待验收", issue.Key), issue.Title, fmt.Sprintf("/issues/%d", issueID), "")
	autoPushReady = true
	go r.dispatchRunEvent(runID, automation.EventRunFinished)
	if run.AutoPR {
		autoSubmit = true
	}
}

func (r *Runner) writeRunSummaryComment(ctx context.Context, proj *domain.Project, issue *domain.Issue, run *domain.Run, summary string) {
	var changed []string
	for _, repo := range reposForIssue(proj, issue) {
		wt, err := repoWorktreePath(run.Workdir, repo.Name)
		if err != nil {
			continue
		}
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
	_, _ = r.st.AddComment(ctx, issue.ID, "agent", b.String())
}

// ---------- Diff & Submit ----------

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

func (r *Runner) emitEvent(runID, issueID int64, typ string, data any) {
	ev, err := r.st.AppendEvent(context.Background(), runID, typ, data)
	if err == nil && r.notify != nil {
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
	if ended {
		go r.dispatchRunEvent(runID, automation.EventRunFinished)
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
	go r.dispatchRunEvent(runID, automation.EventRunFinished)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = r.ArchiveRun(context.Background(), runID)
	}()
}

var nonBranchChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
