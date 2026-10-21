// Package config loads xIssue configuration from ~/.xissue/config.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	Enabled          bool   `yaml:"enabled" json:"enabled"`                         // 监控总开关 (true=运行, false=暂停)
	ActiveHours      string `yaml:"activeHours" json:"activeHours"`                 // 活跃时间段，如 "08:00-20:00" 或 "8-20"，留空全天
	TelegramBotToken string `yaml:"telegramBotToken,omitempty" json:"telegramBotToken,omitempty"` // Telegram Bot Token
	TelegramChatID   string `yaml:"telegramChatId,omitempty" json:"telegramChatId,omitempty"`     // Telegram Chat ID
	WebhookURL       string `yaml:"webhookUrl,omitempty" json:"webhookUrl,omitempty"`             // 通用 Webhook (飞书/企微/钉钉/Slack)
}

// Config is the root configuration.
type Config struct {
	mu                sync.RWMutex
	Path              string                 `yaml:"-" json:"-"`
	Listen            string                 `yaml:"listen"`
	Token             string                 `yaml:"token"`
	DataDir           string                 `yaml:"dataDir"`
	EditorURLTemplate string                 `yaml:"editorUrlTemplate"`
	ExportMarkdown    bool                   `yaml:"exportMarkdown"`
	Agents            map[string]AgentConfig `yaml:"agents"`
	Permissions       PermissionPolicy       `yaml:"permissions"`
	Runner            RunnerConfig           `yaml:"runner"`
	Monitor           MonitorConfig          `yaml:"monitor"`
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

// MonitorSnapshot returns a copy of the monitor settings.
func (c *Config) MonitorSnapshot() MonitorConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Monitor
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
		Agents: map[string]AgentConfig{
			"opencode": {Command: "opencode", Args: []string{"acp"}, MemoryMB: 500},
			"omp":      {Command: "omp", Args: []string{"acp"}, MemoryMB: 500},
			"codex":    {Command: "npx", Args: []string{"-y", "@zed-industries/codex-acp"}, MemoryMB: 200},
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
			Enabled:     true,
			ActiveHours: "08:00-20:00",
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
	return cfg, nil
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
	c.Monitor = m
	return c.saveLocked()
}

// Path helpers.
func (c *Config) DBPath() string     { return filepath.Join(c.DataDir, "xissue.db") }
func (c *Config) MirrorsDir() string { return filepath.Join(c.DataDir, "mirrors") }
func (c *Config) RunsDir() string    { return filepath.Join(c.DataDir, "runs") }
func (c *Config) IssuesDir() string  { return filepath.Join(c.DataDir, "issues") }
