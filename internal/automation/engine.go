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
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/provider"
	"gopkg.in/yaml.v3"
)

// PREventContext is passed to Hook scripts and Plugins via stdin.
type PREventContext struct {
	Event   string                 `json:"event"` // pr_comment | pr_review
	Project domain.Project         `json:"project"`
	Repo    *domain.Repo           `json:"repo,omitempty"`
	Issue   *domain.Issue          `json:"issue,omitempty"`
	PR      *provider.PRDetailInfo `json:"pr,omitempty"`
	Comment *provider.PRComment    `json:"comment,omitempty"`
	Review  *provider.PRReview     `json:"review,omitempty"`
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
		if !p.Enabled {
			continue
		}
		hasEvent := false
		for _, ev := range p.Events {
			if ev == payload.Event || ev == "all" || ev == "*" {
				hasEvent = true
				break
			}
		}
		if !hasEvent {
			continue
		}

		manifestFile := filepath.Join(p.Path, "plugin.yaml")
		entrypoint := e.getPluginEntrypoint(manifestFile)
		if entrypoint == "" {
			entrypoint = "run"
		}
		entryPath := filepath.Join(p.Path, entrypoint)
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
	cmd.Cancel = func() error {
		return cmd.Process.Kill()
	}

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
		{"on-pr-comment", "pr_comment", "PR 收到新评论或报告时触发"},
		{"on-pr-review", "pr_review", "PR 收到审核批准 (Review) 时触发"},
		{"on-pr-created", "pr_created", "新 PR 提交创建成功时触发"},
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

// ListPlugins scans global plugins directory (~/.xissue/plugins).
func (e *Engine) ListPlugins() []domain.PluginInfo {
	entries, err := os.ReadDir(e.pluginsDir)
	if err != nil {
		return nil
	}
	var out []domain.PluginInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginDir := filepath.Join(e.pluginsDir, entry.Name())
		manifestFile := filepath.Join(pluginDir, "plugin.yaml")
		if _, err := os.Stat(manifestFile); err != nil {
			manifestFile = filepath.Join(pluginDir, "plugin.json")
			if _, err2 := os.Stat(manifestFile); err2 != nil {
				continue
			}
		}

		info := domain.PluginInfo{
			ID:      entry.Name(),
			Name:    entry.Name(),
			Version: "1.0.0",
			Path:    pluginDir,
			Enabled: true,
			Events:  []string{"pr_comment", "pr_review"},
		}

		data, err := os.ReadFile(manifestFile)
		if err == nil {
			var m struct {
				Name        string   `yaml:"name" json:"name"`
				Version     string   `yaml:"version" json:"version"`
				Description string   `yaml:"description" json:"description"`
				Author      string   `yaml:"author" json:"author"`
				Events      []string `yaml:"events" json:"events"`
				Enabled     *bool    `yaml:"enabled" json:"enabled"`
			}
			_ = yaml.Unmarshal(data, &m)
			if m.Name != "" {
				info.Name = m.Name
			}
			if m.Version != "" {
				info.Version = m.Version
			}
			if m.Description != "" {
				info.Description = m.Description
			}
			if m.Author != "" {
				info.Author = m.Author
			}
			if len(m.Events) > 0 {
				info.Events = m.Events
			}
			if m.Enabled != nil {
				info.Enabled = *m.Enabled
			}
		}

		out = append(out, info)
	}
	return out
}

func (e *Engine) getPluginEntrypoint(manifestFile string) string {
	data, err := os.ReadFile(manifestFile)
	if err != nil {
		return "run"
	}
	var m struct {
		Entrypoint string `yaml:"entrypoint" json:"entrypoint"`
	}
	_ = yaml.Unmarshal(data, &m)
	ep := filepath.Clean(strings.TrimSpace(m.Entrypoint))
	if ep != "" && !filepath.IsAbs(ep) && !strings.HasPrefix(ep, "..") {
		return ep
	}
	return "run"
}
