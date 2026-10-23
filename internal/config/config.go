// Package config loads xIssue configuration from ~/.xissue/config.yaml.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xpier/xissue/internal/domain"
	"gopkg.in/yaml.v3"
)

// AgentConfig describes how to launch one agent CLI as an ACP server over stdio.
type AgentConfig struct {
	Command      string            `yaml:"command" json:"command"`
	Args         []string          `yaml:"args" json:"args"`
	Env          map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	MemoryMB     int               `yaml:"memoryMB" json:"memoryMB"` // estimated resident memory, used by the scheduler
	DefaultModel string            `yaml:"defaultModel,omitempty" json:"defaultModel"`
	Models       []string          `yaml:"models,omitempty" json:"models"` // user-defined / bound provider/model list
}

// PermissionPolicy decides which agent permission requests are auto-approved.
type PermissionPolicy struct {
	// AutoAllowKinds lists ACP tool kinds approved without asking (e.g. read, edit, search, think, fetch).
	AutoAllowKinds []string `yaml:"autoAllowKinds"`
	// AlwaysAskPatterns: if the request title/command contains any of these, always ask.
	AlwaysAskPatterns []string `yaml:"alwaysAskPatterns"`
}

// RunnerConfig controls local execution.
type RunnerConfig struct {
	MaxConcurrent  int  `yaml:"maxConcurrent"`
	MemoryBudgetMB int  `yaml:"memoryBudgetMB"` // 0 = unlimited
	UseSystemdRun  bool `yaml:"useSystemdRun"`  // Linux: wrap agents with systemd-run --user --scope
	IdleTimeoutSec int  `yaml:"idleTimeoutSec"` // stop idle agent process while waiting for review
}

// MonitorConfig controls branch and PR background monitoring and external notifications.
type MonitorConfig struct {
	Enabled           bool   `yaml:"enabled" json:"enabled"`                                       // 监控总开关 (true=运行, false=暂停)
	ActiveHours       string `yaml:"activeHours" json:"activeHours"`                               // 活跃时间段，如 "08:00-20:00" 或 "8-20"，留空全天
	PublicURL         string `yaml:"publicUrl,omitempty" json:"publicUrl,omitempty"`               // 外部通知里链接使用的访问地址，如 http://100.64.0.2:7789；留空自动推导
	TelegramBotToken  string `yaml:"telegramBotToken,omitempty" json:"telegramBotToken,omitempty"` // Telegram Bot Token
	TelegramChatID    string `yaml:"telegramChatId,omitempty" json:"telegramChatId,omitempty"`     // Telegram Chat ID
	WebhookURL        string `yaml:"webhookUrl,omitempty" json:"webhookUrl,omitempty"`             // 通用 Webhook (飞书/企微/钉钉/Slack)
	SyncIssues        bool   `yaml:"syncIssues" json:"syncIssues"`                                 // 全局自动同步各项目的远程 Issue
	SyncIntervalMin   int    `yaml:"syncIntervalMin" json:"syncIntervalMin"`                       // 自动同步间隔（分钟），默认 10
	AutoDevelopIssues bool   `yaml:"autoDevelopIssues" json:"autoDevelopIssues"`                   // 全局自动开发判定为 Bug/需求的 Issue
	// NotificationRoutes maps notification event kinds to external channels
	// (browser/telegram/webhook). An empty map preserves legacy "send all".
	NotificationRoutes map[string][]string `yaml:"notificationRoutes,omitempty" json:"notificationRoutes,omitempty"`
}

// Config is the root configuration.
type Config struct {
	mu                    sync.RWMutex
	Path                  string                  `yaml:"-" json:"-"`
	Listen                string                  `yaml:"listen"`
	Token                 string                  `yaml:"token"`
	DataDir               string                  `yaml:"dataDir"`
	EditorURLTemplate     string                  `yaml:"editorUrlTemplate"`
	ExportMarkdown        bool                    `yaml:"exportMarkdown"`
	AutoUpdate            bool                    `yaml:"autoUpdate" json:"autoUpdate"`
	DefaultAgent          string                  `yaml:"defaultAgent,omitempty" json:"defaultAgent,omitempty"`
	Agents                map[string]AgentConfig  `yaml:"agents"`
	Permissions           PermissionPolicy        `yaml:"permissions"`
	Runner                RunnerConfig            `yaml:"runner"`
	Monitor               MonitorConfig           `yaml:"monitor"`
	GlobalAutomationRules []domain.AutomationRule `yaml:"globalAutomationRules,omitempty" json:"globalAutomationRules,omitempty"`
}

func cloneAgent(ac AgentConfig) AgentConfig {
	ac.Args = append([]string(nil), ac.Args...)
	ac.Models = append([]string(nil), ac.Models...)
	if ac.Env != nil {
		env := make(map[string]string, len(ac.Env))
		for k, v := range ac.Env {
			env[k] = v
		}
		ac.Env = env
	}
	return ac
}

// Agent returns a defensive copy of one agent configuration.
func (c *Config) Agent(name string) (AgentConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ac, ok := c.Agents[name]
	if !ok {
		return AgentConfig{}, false
	}
	return cloneAgent(ac), true
}

// AgentsSnapshot returns a defensive copy of all configured agents.
func (c *Config) AgentsSnapshot() map[string]AgentConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]AgentConfig, len(c.Agents))
	for name, ac := range c.Agents {
		out[name] = cloneAgent(ac)
	}
	return out
}

// SetAgent stores an agent configuration and persists config.yaml atomically.
func (c *Config) SetAgent(name string, ac AgentConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Agents == nil {
		c.Agents = map[string]AgentConfig{}
	}
	c.Agents[name] = cloneAgent(ac)
	return c.saveLocked()
}

// DeleteAgent removes an agent configuration and persists config.yaml atomically.
func (c *Config) DeleteAgent(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Agents, name)
	return c.saveLocked()
}

// RunnerSnapshot returns a copy of the runner settings.
func (c *Config) RunnerSnapshot() RunnerConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Runner
}

// PermissionsSnapshot returns a copy of the permission policy.
func (c *Config) PermissionsSnapshot() PermissionPolicy {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p := c.Permissions
	p.AutoAllowKinds = append([]string(nil), c.Permissions.AutoAllowKinds...)
	p.AlwaysAskPatterns = append([]string(nil), c.Permissions.AlwaysAskPatterns...)
	return p
}

// MaskedSecret is returned instead of stored credentials. Clients send it back
// unchanged to mean "keep the existing value".
const MaskedSecret = "******"

// MonitorPatch updates only the supplied monitor fields.
type MonitorPatch struct {
	Enabled            *bool                `json:"enabled"`
	ActiveHours        *string              `json:"activeHours"`
	PublicURL          *string              `json:"publicUrl"`
	TelegramBotToken   *string              `json:"telegramBotToken"`
	TelegramChatID     *string              `json:"telegramChatId"`
	WebhookURL         *string              `json:"webhookUrl"`
	SyncIssues         *bool                `json:"syncIssues"`
	SyncIntervalMin    *int                 `json:"syncIntervalMin"`
	AutoDevelopIssues  *bool                `json:"autoDevelopIssues"`
	NotificationRoutes *map[string][]string `json:"notificationRoutes"`
}

// MaskedMonitorSnapshot returns monitor settings with credentials masked.
func (c *Config) MaskedMonitorSnapshot() MonitorConfig {
	mon := c.MonitorSnapshot()
	if strings.TrimSpace(mon.TelegramBotToken) != "" {
		mon.TelegramBotToken = MaskedSecret
	}
	if strings.TrimSpace(mon.WebhookURL) != "" {
		mon.WebhookURL = MaskedSecret
	}
	return mon
}

// MonitorSnapshot returns a copy of the monitor settings.
func (c *Config) MonitorSnapshot() MonitorConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Monitor
}

// PublicBaseURL returns the externally reachable base URL used in notifications.
// An explicit monitor.publicUrl wins; otherwise it is derived from the listen
// address (unspecified hosts fall back to a non-loopback interface when present).
func (c *Config) PublicBaseURL() string {
	mon := c.MonitorSnapshot()
	if raw := strings.TrimSpace(mon.PublicURL); raw != "" {
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		return strings.TrimRight(raw, "/")
	}
	listen := strings.TrimSpace(c.Listen)
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		if listen == "" {
			return "http://127.0.0.1:7789"
		}
		return "http://" + strings.TrimRight(listen, "/")
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = firstNonLoopbackIPv4()
	}
	return "http://" + net.JoinHostPort(host, port)
}

// AbsoluteURL turns an in-app path (e.g. /projects/2?watch=5) into a clickable
// absolute URL for external notification channels.
func (c *Config) AbsoluteURL(target string) string {
	target = strings.TrimSpace(target)
	if target == "" || strings.Contains(target, "://") {
		return target
	}
	base := c.PublicBaseURL()
	if strings.HasPrefix(target, "/") {
		return base + target
	}
	return base + "/" + target
}

func firstNonLoopbackIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			return ip.String()
		}
	}
	return "127.0.0.1"
}

// EditorTemplate returns the configured editor URL template.
func (c *Config) EditorTemplate() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.EditorURLTemplate
}

// ExportMarkdownEnabled reports whether markdown export is enabled.
func (c *Config) ExportMarkdownEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ExportMarkdown
}

// DefaultDataDir returns ~/.xissue.
func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".xissue"
	}
	return filepath.Join(home, ".xissue")
}

// Default returns a config with sensible defaults.
func Default() *Config {
	return &Config{
		Listen:            "127.0.0.1:7789",
		DataDir:           DefaultDataDir(),
		EditorURLTemplate: "vscode://file{path}",
		ExportMarkdown:    true,
		AutoUpdate:        true,
		Agents: map[string]AgentConfig{
			"opencode": {Command: "opencode", Args: []string{"acp"}, MemoryMB: 500},
			"omp":      {Command: "omp", Args: []string{"acp"}, MemoryMB: 500},
			"codex":    {Command: "npx", Args: []string{"-y", "@agentclientprotocol/codex-acp"}, MemoryMB: 200},
		},
		Permissions: PermissionPolicy{
			AutoAllowKinds:    []string{"read", "search", "think", "fetch", "edit"},
			AlwaysAskPatterns: []string{"git push", "rm -rf", "sudo", "npm publish", "pnpm publish"},
		},
		Runner: RunnerConfig{
			MaxConcurrent:  2,
			MemoryBudgetMB: 0,
			UseSystemdRun:  false,
			IdleTimeoutSec: 600,
		},
		Monitor: MonitorConfig{
			Enabled:         true,
			ActiveHours:     "", // empty = all day; avoids silently pausing overnight after upgrades
			SyncIntervalMin: 10,
		},
	}
}

// Load reads the config file at path (or <dataDir>/config.yaml when path is empty).
// A missing file yields defaults and writes a starter file.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		path = filepath.Join(cfg.DataDir, "config.yaml")
	}
	cfg.Path = path
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		out, _ := yaml.Marshal(cfg)
		_ = os.WriteFile(path, out, 0o600)
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.Path = path
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	if cfg.Agents == nil {
		cfg.Agents = map[string]AgentConfig{}
	}
	if cfg.Runner.MaxConcurrent <= 0 {
		cfg.Runner.MaxConcurrent = 1
	}
	cfg.Monitor.SyncIntervalMin = normalizeSyncInterval(cfg.Monitor.SyncIntervalMin)
	cfg.Monitor.NotificationRoutes = sanitizeNotificationRoutes(cfg.Monitor.NotificationRoutes)
	return cfg, nil
}

// migrateDeprecatedAgents upgrades the retired Zed Codex ACP npm package to
// the maintained Agent Client Protocol package. The old package's optional
// platform binaries are no longer installed reliably and exit with status 1.
func migrateDeprecatedAgents(agents map[string]AgentConfig) {
	for name, ac := range agents {
		if ac.Command != "npx" {
			continue
		}
		changed := false
		for i, arg := range ac.Args {
			if strings.HasPrefix(arg, "@zed-industries/codex-acp") {
				ac.Args[i] = "@agentclientprotocol/codex-acp"
				changed = true
			}
		}
		if changed {
			agents[name] = ac
		}
	}
}

func normalizeSyncInterval(v int) int {
	if v <= 0 {
		return 10
	}
	if v > 24*60 {
		return 24 * 60
	}
	return v
}

// Save writes the current configuration back to disk atomically.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

func (c *Config) saveLocked() error {
	path := c.Path
	if path == "" {
		path = filepath.Join(c.DataDir, "config.yaml")
		c.Path = path
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Reload re-reads the config file from disk and updates c in-place.
func (c *Config) Reload() error {
	fresh, err := Load(c.Path)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Agents = fresh.Agents
	c.Permissions = fresh.Permissions
	c.Runner = fresh.Runner
	c.Monitor = fresh.Monitor
	c.EditorURLTemplate = fresh.EditorURLTemplate
	c.ExportMarkdown = fresh.ExportMarkdown
	return nil
}

// UpdateMonitor updates the monitor configuration and writes config.yaml back to disk.
func (c *Config) UpdateMonitor(m MonitorConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m.TelegramBotToken == MaskedSecret {
		m.TelegramBotToken = c.Monitor.TelegramBotToken
	}
	if m.WebhookURL == MaskedSecret {
		m.WebhookURL = c.Monitor.WebhookURL
	}
	m.TelegramBotToken = strings.TrimSpace(m.TelegramBotToken)
	m.TelegramChatID = strings.TrimSpace(m.TelegramChatID)
	m.WebhookURL = strings.TrimSpace(m.WebhookURL)
	m.ActiveHours = strings.TrimSpace(m.ActiveHours)
	m.PublicURL = strings.TrimSpace(m.PublicURL)
	m.SyncIntervalMin = normalizeSyncInterval(m.SyncIntervalMin)
	m.NotificationRoutes = sanitizeNotificationRoutes(m.NotificationRoutes)
	c.Monitor = m
	return c.saveLocked()
}

// PatchMonitor applies a partial update without clobbering other settings.
func (c *Config) PatchMonitor(p MonitorPatch) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.Monitor
	if p.Enabled != nil {
		m.Enabled = *p.Enabled
	}
	if p.ActiveHours != nil {
		m.ActiveHours = strings.TrimSpace(*p.ActiveHours)
	}
	if p.PublicURL != nil {
		m.PublicURL = strings.TrimSpace(*p.PublicURL)
	}
	if p.TelegramBotToken != nil {
		if v := strings.TrimSpace(*p.TelegramBotToken); v != MaskedSecret {
			m.TelegramBotToken = v
		}
	}
	if p.TelegramChatID != nil {
		m.TelegramChatID = strings.TrimSpace(*p.TelegramChatID)
	}
	if p.WebhookURL != nil {
		if v := strings.TrimSpace(*p.WebhookURL); v != MaskedSecret {
			m.WebhookURL = v
		}
	}
	if p.SyncIssues != nil {
		m.SyncIssues = *p.SyncIssues
	}
	if p.SyncIntervalMin != nil {
		m.SyncIntervalMin = *p.SyncIntervalMin
	}
	if p.AutoDevelopIssues != nil {
		m.AutoDevelopIssues = *p.AutoDevelopIssues
	}
	if p.NotificationRoutes != nil {
		m.NotificationRoutes = sanitizeNotificationRoutes(*p.NotificationRoutes)
	}
	m.SyncIntervalMin = normalizeSyncInterval(m.SyncIntervalMin)
	c.Monitor = m
	return c.saveLocked()
}

// NotificationEventKinds are the event kinds exposed in the routing UI.
var NotificationEventKinds = []string{
	"run_done", "permission", "pr_status", "branch_push",
	"automation", "update", "test",
}

var notificationChannels = map[string]bool{
	"browser":  true,
	"telegram": true,
	"webhook":  true,
}

func sanitizeNotificationRoutes(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	known := make(map[string]bool, len(NotificationEventKinds))
	for _, kind := range NotificationEventKinds {
		known[kind] = true
	}
	out := make(map[string][]string, len(in))
	for kind, channels := range in {
		kind = strings.TrimSpace(kind)
		if !known[kind] {
			continue
		}
		seen := map[string]bool{}
		clean := make([]string, 0, len(channels))
		for _, ch := range channels {
			ch = strings.TrimSpace(ch)
			if !notificationChannels[ch] || seen[ch] {
				continue
			}
			seen[ch] = true
			clean = append(clean, ch)
		}
		out[kind] = clean
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// GlobalAutomationRulesSnapshot returns a defensive copy of global rules.
func (c *Config) GlobalAutomationRulesSnapshot() []domain.AutomationRule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]domain.AutomationRule, len(c.GlobalAutomationRules))
	for i, rule := range c.GlobalAutomationRules {
		rule.Actions = append([]domain.RuleAction(nil), rule.Actions...)
		rule.ProjectIDs = append([]int64(nil), rule.ProjectIDs...)
		out[i] = rule
	}
	return out
}

// SetGlobalAutomationRules replaces the global automation rule set.
func (c *Config) SetGlobalAutomationRules(rules []domain.AutomationRule) {
	copied := make([]domain.AutomationRule, len(rules))
	for i, rule := range rules {
		rule.Actions = append([]domain.RuleAction(nil), rule.Actions...)
		rule.ProjectIDs = append([]int64(nil), rule.ProjectIDs...)
		copied[i] = rule
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.GlobalAutomationRules = copied
}

// Path helpers.
func (c *Config) DBPath() string     { return filepath.Join(c.DataDir, "xissue.db") }
func (c *Config) MirrorsDir() string { return filepath.Join(c.DataDir, "mirrors") }
func (c *Config) RunsDir() string    { return filepath.Join(c.DataDir, "runs") }
func (c *Config) IssuesDir() string  { return filepath.Join(c.DataDir, "issues") }
func (c *Config) PluginsDir() string { return filepath.Join(c.DataDir, "plugins") }

// DefaultAgentName returns the globally preferred agent, if configured.
func (c *Config) DefaultAgentName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return strings.TrimSpace(c.DefaultAgent)
}

// SetDefaultAgent stores the preferred agent name.
func (c *Config) SetDefaultAgent(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.DefaultAgent = strings.TrimSpace(name)
}

// AutoUpdateEnabled reports whether the hourly self-update check is enabled.
func (c *Config) AutoUpdateEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AutoUpdate
}

// SetAutoUpdate toggles the hourly self-update check.
func (c *Config) SetAutoUpdate(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AutoUpdate = v
}
