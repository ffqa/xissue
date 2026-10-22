package runner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/automation"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/provider"
)

// setupLifecycleObservers forwards store-level issue changes into the plugin
// bus. The observers only spawn goroutines so SQLite writes never wait on a
// plugin subprocess.
func (r *Runner) setupLifecycleObservers() {
	if r.st == nil {
		return
	}
	r.st.SetIssueObservers(func(issue *domain.Issue) {
		issueCopy := *issue
		go r.dispatchIssueEvent(automation.EventIssueCreated, &issueCopy, "")
	}, func(issue *domain.Issue, previous domain.IssueStatus) {
		issueCopy := *issue
		go r.dispatchIssueEvent(automation.EventIssueStatusChanged, &issueCopy, string(previous))
	})
}

func (r *Runner) lifecycleContext() (context.Context, context.CancelFunc) {
	base := r.ctx
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, 30*time.Second)
}

func (r *Runner) dispatchIssueEvent(event string, issue *domain.Issue, previous string) {
	if r.automation == nil || issue == nil {
		return
	}
	ctx, cancel := r.lifecycleContext()
	defer cancel()
	project, err := r.st.GetProject(ctx, issue.ProjectID)
	if err != nil || project == nil {
		return
	}
	var repo *domain.Repo
	if issue.RepoID != nil {
		repo, _ = r.st.GetRepo(ctx, *issue.RepoID)
	}
	if repo == nil {
		for i := range project.Repos {
			if gitx.IsLocalDir(project.Repos[i].RemoteURL) {
				repo = &project.Repos[i]
				break
			}
		}
	}
	payload := automation.PREventContext{
		Event:          event,
		Project:        *project,
		Repo:           repo,
		Issue:          issue,
		PreviousStatus: previous,
		Timestamp:      time.Now().Format(time.RFC3339),
	}
	r.dispatchLifecycle(ctx, payload, repo)
}

func (r *Runner) dispatchRunEvent(runID int64, event string) {
	if r.automation == nil {
		return
	}
	ctx, cancel := r.lifecycleContext()
	defer cancel()
	run, err := r.st.GetRun(ctx, runID)
	if err != nil || run == nil {
		return
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil || issue == nil {
		return
	}
	project, err := r.st.GetProject(ctx, issue.ProjectID)
	if err != nil || project == nil {
		return
	}
	var repo *domain.Repo
	if issue.RepoID != nil {
		repo, _ = r.st.GetRepo(ctx, *issue.RepoID)
	}
	if repo == nil {
		for i := range project.Repos {
			if gitx.IsLocalDir(project.Repos[i].RemoteURL) {
				repo = &project.Repos[i]
				break
			}
		}
	}
	payload := automation.PREventContext{
		Event:     event,
		Project:   *project,
		Repo:      repo,
		Issue:     issue,
		Run:       run,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	r.dispatchLifecycle(ctx, payload, repo)
}

func (r *Runner) dispatchPRCreated(project *domain.Project, repo *domain.Repo, issue *domain.Issue, pr *provider.PRDetailInfo) {
	if r.automation == nil || project == nil || pr == nil {
		return
	}
	go func() {
		ctx, cancel := r.lifecycleContext()
		defer cancel()
		payload := automation.PREventContext{
			Event:     automation.EventPRCreated,
			Project:   *project,
			Repo:      repo,
			Issue:     issue,
			PR:        pr,
			Timestamp: time.Now().Format(time.RFC3339),
		}
		r.dispatchLifecycle(ctx, payload, repo)
	}()
}

// dispatchLifecycle runs Layer 2 hooks (local repos only) and Layer 3 plugins,
// then executes the small action set that makes sense for lifecycle events.
func (r *Runner) dispatchLifecycle(ctx context.Context, payload automation.PREventContext, repo *domain.Repo) {
	workdir := ""
	if repo != nil && gitx.IsLocalDir(repo.RemoteURL) {
		workdir = repo.RemoteURL
	}
	var actions []domain.RuleAction
	if workdir != "" {
		if acts, err := r.automation.ExecuteProjectHooks(ctx, workdir, payload); err == nil {
			actions = append(actions, acts...)
		} else {
			r.LogMonitor("WARN", "automation", fmt.Sprintf("Layer 2 Hook 执行失败 (%s): %v", payload.Event, err))
		}
	}
	if acts, err := r.automation.ExecutePlugins(ctx, payload); err == nil {
		actions = append(actions, acts...)
	} else {
		r.LogMonitor("WARN", "automation", fmt.Sprintf("插件执行失败 (%s): %v", payload.Event, err))
	}
	if len(actions) > 0 {
		r.LogMonitor("EVENT", "automation", fmt.Sprintf("%s 触发 %d 个生命周期动作", payload.Event, len(actions)))
	}
	r.executeLifecycleActions(payload, actions)
}

// Lifecycle events intentionally support only notifications. Actions that
// mutate issues/runs (trigger_run, merge_pr, ...) are ignored to prevent
// recursive event loops and unbounded token spend.
func (r *Runner) executeLifecycleActions(payload automation.PREventContext, actions []domain.RuleAction) {
	for _, act := range actions {
		if act.Type != "notify" {
			r.LogMonitor("WARN", "automation", fmt.Sprintf("事件 %s 不支持动作 %q，已忽略", payload.Event, act.Type))
			continue
		}
		msg := strings.TrimSpace(act.Message)
		if msg == "" {
			msg = "插件触发通知: " + payload.Event
		}
		title := fmt.Sprintf("⚡ [%s] %s", payload.Project.Name, lifecycleTitle(payload.Event))
		link := ""
		if payload.Issue != nil {
			link = fmt.Sprintf("/issues/%d", payload.Issue.ID)
		}
		r.pushNotification("automation", title, msg, link, "")
	}
}

func lifecycleTitle(event string) string {
	switch event {
	case automation.EventIssueCreated:
		return "新 Issue"
	case automation.EventIssueStatusChanged:
		return "Issue 状态变更"
	case automation.EventRunStarted:
		return "Agent 开始执行"
	case automation.EventRunFinished:
		return "Agent 执行结束"
	case automation.EventPRCreated:
		return "PR 已创建"
	default:
		return event
	}
}

func prNumberFromURL(raw string) int64 {
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "/")
	if raw == "" {
		return 0
	}
	idx := strings.LastIndex(raw, "/")
	if idx < 0 || idx == len(raw)-1 {
		return 0
	}
	n, err := strconv.ParseInt(raw[idx+1:], 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
