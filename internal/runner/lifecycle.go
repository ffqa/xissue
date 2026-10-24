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
	r.executeLifecycleActions(ctx, payload, actions)
}

// lifecycleActionAllowed defines the safe action surface for each lifecycle
// event. Mutations are limited to events that cannot form an unbounded loop:
// create_issue only on run_finished, trigger_run only on pr_created, and
// add_labels on events that carry an issue.
func lifecycleActionAllowed(event, action string) bool {
	switch action {
	case "notify":
		return true
	case "add_labels":
		switch event {
		case automation.EventIssueCreated, automation.EventIssueStatusChanged, automation.EventRunFinished, automation.EventPRCreated:
			return true
		}
	case "create_issue":
		return event == automation.EventRunFinished
	case "trigger_run":
		return event == automation.EventPRCreated
	}
	return false
}

func (r *Runner) executeLifecycleActions(ctx context.Context, payload automation.PREventContext, actions []domain.RuleAction) {
	if len(actions) > 50 {
		r.LogMonitor("WARN", "automation", fmt.Sprintf("事件 %s 返回 %d 个动作，仅执行前 50 个", payload.Event, len(actions)))
		actions = actions[:50]
	}
	for _, act := range actions {
		if !lifecycleActionAllowed(payload.Event, act.Type) {
			r.LogMonitor("WARN", "automation", fmt.Sprintf("事件 %s 不支持动作 %q，已忽略", payload.Event, act.Type))
			continue
		}
		switch act.Type {
		case "notify":
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

		case "add_labels":
			if payload.Issue == nil || len(act.Labels) == 0 {
				continue
			}
			updated, err := r.st.AddIssueLabels(ctx, payload.Issue.ID, act.Labels...)
			if err != nil {
				r.LogMonitor("WARN", "automation", fmt.Sprintf("事件 %s 添加标签失败: %v", payload.Event, err))
				continue
			}
			r.notify.BroadcastIssueUpdated(updated)
			r.LogMonitor("INFO", "automation", fmt.Sprintf("事件 %s 已为 Issue %s 添加标签 %v", payload.Event, payload.Issue.Key, act.Labels))

		case "create_issue":
			if payload.Issue == nil {
				continue
			}
			title := strings.TrimSpace(act.Prompt)
			if title == "" {
				title = fmt.Sprintf("跟进 %s: %s", lifecycleTitle(payload.Event), payload.Issue.Key)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "## 插件跟进任务\n\n- 来源 Issue: `%s`\n- 事件: `%s`\n", payload.Issue.Key, payload.Event)
			if payload.Run != nil {
				fmt.Fprintf(&b, "- 来源 Run: #%d (%s)\n", payload.Run.ID, payload.Run.Status)
			}
			if payload.Issue.Title != "" {
				fmt.Fprintf(&b, "\n> %s\n", payload.Issue.Title)
			}
			labels := append([]string{}, act.Labels...)
			newIssue, err := r.st.CreateLocalIssue(ctx, payload.Issue.ProjectID, title, b.String(), labels)
			if err != nil {
				r.LogMonitor("WARN", "automation", fmt.Sprintf("事件 %s 创建跟进 Issue 失败: %v", payload.Event, err))
				continue
			}
			if payload.Issue.RepoID != nil {
				_ = r.st.BindIssueRepo(ctx, newIssue.ID, *payload.Issue.RepoID)
			}
			r.notify.BroadcastIssueUpdated(newIssue)
			r.LogMonitor("INFO", "automation", fmt.Sprintf("事件 %s 已创建跟进 Issue %s", payload.Event, newIssue.Key))

		case "trigger_run":
			if payload.Issue == nil || payload.PR == nil {
				continue
			}
			agent := strings.TrimSpace(act.Agent)
			if agent == "" {
				agent = r.AgentForRoleOrDefault(AgentRolePRReview)
			}
			if agent == "" {
				r.LogMonitor("WARN", "automation", "事件 pr_created 无法调度 Agent：未配置可用 Agent")
				continue
			}
			prompt := strings.TrimSpace(act.Prompt)
			if prompt == "" {
				prompt = fmt.Sprintf("Review PR #%d %s for issue %s. Report findings and update the branch only when necessary. Do NOT merge or close the PR yourself.", payload.PR.Number, payload.PR.URL, payload.Issue.Key)
			}
			if _, err := r.StartRun(ctx, payload.Issue.ID, agent, "", prompt); err != nil {
				r.LogMonitor("WARN", "automation", fmt.Sprintf("事件 pr_created 调度 Agent 失败: %v", err))
			} else {
				r.LogMonitor("INFO", "automation", fmt.Sprintf("事件 pr_created 已启动 Agent [%s] 审查 Issue %s", agent, payload.Issue.Key))
			}
		}
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
