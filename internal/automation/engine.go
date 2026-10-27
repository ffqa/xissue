package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/provider"
)

// NotificationEventContext provides notification dispatch details to hooks and plugins.
type NotificationEventContext struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Link      string `json:"link"`
	Timestamp string `json:"timestamp"`
}

// PREventContext is passed to Hook scripts and Plugins via stdin.
type PREventContext struct {
	Event          string                    `json:"event"` // pr_comment | pr_review | branch_push | pr_merged | pr_created | notification_dispatch
	Project        domain.Project            `json:"project"`
	Repo           *domain.Repo              `json:"repo,omitempty"`
	Branch         string                    `json:"branch,omitempty"`
	TipSHA         string                    `json:"tipSha,omitempty"`
	Commits        []gitx.CommitInfo         `json:"commits,omitempty"`
	Issue          *domain.Issue             `json:"issue,omitempty"`
	PR             *provider.PRDetailInfo    `json:"pr,omitempty"`
	Comment        *provider.PRComment       `json:"comment,omitempty"`
	Review         *provider.PRReview        `json:"review,omitempty"`
	Notification   *NotificationEventContext `json:"notification,omitempty"`
	Run            *domain.Run               `json:"run,omitempty"`
	PreviousStatus string                    `json:"previousStatus,omitempty"`
	Timestamp      string                    `json:"timestamp,omitempty"`
}

// HookResult is parsed from Hook script or Plugin stdout.
type HookResult struct {
	Actions []domain.RuleAction `json:"actions"`
}

// Engine dispatches events across Layer 1 (Rules), Layer 2 (Hooks), and Layer 3 (Plugins).
type Engine struct {
	pluginsDir string
}

func NewEngine(pluginsDir string) *Engine {
	if pluginsDir == "" {
		home, _ := os.UserHomeDir()
		pluginsDir = filepath.Join(home, ".xissue", "plugins")
	}
	_ = os.MkdirAll(pluginsDir, 0o755)
	return &Engine{pluginsDir: pluginsDir}
}

// MatchRule checks if a comment or review matches the given AutomationRule conditions.
func MatchRule(rule domain.AutomationRule, comment *provider.PRComment, review *provider.PRReview) bool {
	if !rule.Enabled {
		return false
	}
	cond := rule.Conditions

	// 1. Author check
	if cond.Author != "" {
		expectedAuthor := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(cond.Author)), "@")
		actualAuthor := ""
		if comment != nil {
			actualAuthor = strings.ToLower(strings.TrimSpace(comment.Author))
		} else if review != nil {
			actualAuthor = strings.ToLower(strings.TrimSpace(review.Author))
		}
		if actualAuthor != expectedAuthor {
			return false
		}
	}

	// 2. Review state check
	if cond.ReviewState != "" {
		if review == nil || !strings.EqualFold(review.State, cond.ReviewState) {
			return false
		}
	}

	// 3. Body regex pattern check
	if cond.Pattern != "" {
		body := ""
		if comment != nil {
			body = comment.Body
		} else if review != nil {
			body = review.Body
		}
		re, err := regexp.Compile(cond.Pattern)
		if err != nil {
			// Fallback: simple contains
			return strings.Contains(strings.ToLower(body), strings.ToLower(cond.Pattern))
		}
		if !re.MatchString(body) {
			return false
		}
	}

	return true
}

// MatchBranchPushRule checks if a branch push matches rule conditions.
func MatchBranchPushRule(rule domain.AutomationRule, branch string, author string, commitMsg string) bool {
	if !rule.Enabled || rule.Trigger != "branch_push" {
		return false
	}
	cond := rule.Conditions

	// 1. Branch match (支持精确匹配或正则表达式)
	if cond.Branch != "" {
		expectedBranch := strings.TrimSpace(cond.Branch)
		if !strings.EqualFold(expectedBranch, strings.TrimSpace(branch)) {
			re, err := regexp.Compile(expectedBranch)
			if err != nil || !re.MatchString(branch) {
				return false
			}
		}
	}

	// 2. Author check (exact identity, not substring: bob must not match bobby)
	if cond.Author != "" {
		expectedAuthor := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(cond.Author)), "@")
		actualAuthor := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(author)), "@")
		if actualAuthor != expectedAuthor {
			return false
		}
	}

	// 3. Pattern regex check on commit message
	if cond.Pattern != "" {
		re, err := regexp.Compile(cond.Pattern)
		if err != nil {
			if !strings.Contains(strings.ToLower(commitMsg), strings.ToLower(cond.Pattern)) {
				return false
			}
		} else if !re.MatchString(commitMsg) {
			return false
		}
	}

	return true
}

// EvaluateBranchPushRules (Layer 1) evaluates built-in rules for branch_push events.
func (e *Engine) EvaluateBranchPushRules(rules []domain.AutomationRule, branch string, author string, commitMsg string) []domain.RuleAction {
	var actions []domain.RuleAction
	for _, rule := range rules {
		if MatchBranchPushRule(rule, branch, author, commitMsg) {
			actions = append(actions, rule.Actions...)
		}
	}
	return actions
}

// EvaluateRules (Layer 1) evaluates built-in project declarative rules.
func (e *Engine) EvaluateRules(rules []domain.AutomationRule, trigger string, comment *provider.PRComment, review *provider.PRReview) []domain.RuleAction {
	var actions []domain.RuleAction
	for _, rule := range rules {
		if !rule.Enabled || rule.Trigger != trigger {
			continue
		}
		if MatchRule(rule, comment, review) {
			actions = append(actions, rule.Actions...)
		}
	}
	return actions
}

// ExecuteProjectHooks (Layer 2) runs local scripts in <workdir>/.xissue/hooks/.
func (e *Engine) ExecuteProjectHooks(ctx context.Context, workdir string, payload PREventContext) ([]domain.RuleAction, error) {
	if workdir == "" {
		return nil, nil
	}
	hooksDir := filepath.Join(workdir, ".xissue", "hooks")
	if _, err := os.Stat(hooksDir); err != nil {
		return nil, nil
	}

	hookName := "on-" + strings.ReplaceAll(payload.Event, "_", "-")
	candidates := []string{
		filepath.Join(hooksDir, hookName),
		filepath.Join(hooksDir, hookName+".sh"),
		filepath.Join(hooksDir, hookName+".py"),
		filepath.Join(hooksDir, hookName+".js"),
	}

	var scriptPath string
	for _, c := range candidates {
		info, err := os.Stat(c)
		if err == nil && !info.IsDir() {
			scriptPath = c
			break
		}
	}
	if scriptPath == "" {
		return nil, nil
	}

	return e.runScriptWithPayload(ctx, scriptPath, payload)
}

// ExecutePlugins (Layer 3) runs enabled ecosystem plugins matching the event.
func (e *Engine) ExecutePlugins(ctx context.Context, payload PREventContext) ([]domain.RuleAction, error) {
	plugins := e.ListPlugins()
	var actions []domain.RuleAction

	for _, p := range plugins {
		if !p.Enabled || p.Error != "" {
			continue
		}
		if !pluginEventMatches(p.Events, payload.Event) {
			continue
		}
		manifest, _, err := LoadPluginManifest(p.Path)
		if err != nil {
			continue
		}
		entryPath, err := securePluginPath(p.Path, manifest.Entrypoint)
		if err != nil {
			continue
		}
		if info, err := os.Stat(entryPath); err == nil && !info.IsDir() {
			acts, err := e.runScriptWithPayload(ctx, entryPath, payload)
			if err == nil {
				actions = append(actions, acts...)
			}
		}
	}
	return actions, nil
}

func (e *Engine) runScriptWithPayload(ctx context.Context, scriptPath string, payload PREventContext) ([]domain.RuleAction, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	// Timeout protection: max 15 seconds per hook
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS != "windows" {
		// Ensure execution permission
		_ = os.Chmod(scriptPath, 0o755)
		if strings.HasSuffix(scriptPath, ".py") {
			cmd = exec.CommandContext(tctx, "python3", scriptPath)
		} else if strings.HasSuffix(scriptPath, ".js") {
			cmd = exec.CommandContext(tctx, "node", scriptPath)
		} else {
			cmd = exec.CommandContext(tctx, scriptPath)
		}
	} else {
		cmd = exec.CommandContext(tctx, scriptPath)
	}

	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Stdin = bytes.NewReader(payloadBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	setProcessGroup(cmd)
	cmd.Cancel = func() error {
		return killProcessTree(cmd)
	}
	// Bound Wait even if a grandchild inherited the stdout/stderr pipes.
	cmd.WaitDelay = 2 * time.Second

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("hook %s failed: %v (%s)", filepath.Base(scriptPath), err, strings.TrimSpace(stderr.String()))
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return nil, nil
	}

	var res HookResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		// Output wasn't JSON actions, treat as no-op
		return nil, nil
	}
	return res.Actions, nil
}

// DetectHooks checks known hooks in project directory.
func DetectHooks(workdir string) []domain.HookStatus {
	known := []struct {
		name  string
		event string
		desc  string
	}{
		{"on-branch-push", "branch_push", "监控分支检测到新代码推送 (Push) 时触发"},
		{"on-pr-merged", "pr_merged", "PR 成功合并到目标分支时触发"},
		{"on-notify", "notification_dispatch", "系统发出通知时触发，用于扩展自定义通知渠道 (飞书/企微/钉钉/桌面)"},
		{"on-pr-comment", "pr_comment", "PR 收到新评论或报告时触发"},
		{"on-pr-review", "pr_review", "PR 收到审核批准 (Review) 时触发"},
		{"on-pr-created", "pr_created", "新 PR 提交创建成功时触发"},
		{"on-issue-created", "issue_created", "本地或远程 Issue 首次创建时触发"},
		{"on-issue-status-changed", "issue_status_changed", "Issue 状态发生变化时触发"},
		{"on-run-started", "run_started", "Agent Run 开始执行时触发"},
		{"on-run-finished", "run_finished", "Agent Run 完成、失败或取消时触发"},
	}

	var out []domain.HookStatus
	hooksDir := filepath.Join(workdir, ".xissue", "hooks")

	for _, k := range known {
		st := domain.HookStatus{
			Name:        k.name,
			Event:       k.event,
			Path:        filepath.Join(".xissue", "hooks", k.name),
			Description: k.desc,
		}

		cands := []string{
			filepath.Join(hooksDir, k.name),
			filepath.Join(hooksDir, k.name+".sh"),
			filepath.Join(hooksDir, k.name+".py"),
			filepath.Join(hooksDir, k.name+".js"),
		}

		for _, c := range cands {
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				st.Exists = true
				st.Path = filepath.Join(".xissue", "hooks", filepath.Base(c))
				if runtime.GOOS == "windows" || (info.Mode()&0111 != 0) {
					st.Executable = true
				}
				break
			}
		}
		out = append(out, st)
	}
	return out
}

// ListPlugins scans the plugins directory and reports both valid and broken installs.
func (e *Engine) ListPlugins() []domain.PluginInfo {
	entries, err := os.ReadDir(e.pluginsDir)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var out []domain.PluginInfo
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		pluginDir := filepath.Join(e.pluginsDir, entry.Name())
		if info, err := os.Stat(pluginDir); err != nil || !info.IsDir() {
			continue
		}
		info := domain.PluginInfo{
			ID:          entry.Name(),
			Name:        entry.Name(),
			Version:     "0.0.0",
			APIVersion:  PluginAPIVersion,
			Path:        pluginDir,
			Enabled:     false,
			Events:      []string{},
			Permissions: []string{},
		}
		manifest, _, err := LoadPluginManifest(pluginDir)
		if err != nil {
			info.Error = err.Error()
			info.Description = "插件清单无效: " + err.Error()
			out = append(out, info)
			continue
		}
		info.Name = manifest.Name
		info.Version = manifest.Version
		info.APIVersion = manifest.APIVersion
		info.Description = manifest.Description
		info.Author = manifest.Author
		info.Events = manifest.Events
		info.Permissions = manifest.Permissions
		info.Enabled = manifest.Enabled == nil || *manifest.Enabled
		if _, statErr := os.Stat(filepath.Join(pluginDir, PluginDisabledFile)); statErr == nil {
			info.Enabled = false
		}
		info.HasReadme = pluginFileExists(pluginDir, "README.md") ||
			pluginFileExists(pluginDir, "readme.md") ||
			pluginFileExists(pluginDir, "README.markdown")
		info.HasConfig = pluginFileExists(pluginDir, PluginConfigFile)
		if manifest.UI != nil {
			info.UI = &domain.PluginUIInfo{
				Entry: manifest.UI.Entry,
				Title: manifest.UI.Title,
				Slots: append([]string(nil), manifest.UI.Slots...),
			}
		}
		out = append(out, info)
	}
	return out
}
