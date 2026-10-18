// Package config loads xIssue configuration from ~/.xissue/config.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// AgentConfig describes how to launch one agent CLI as an ACP server over stdio.
type AgentConfig struct {
	Command  string            `yaml:"command"`
	Args     []string          `yaml:"args"`
	Env      map[string]string `yaml:"env"`
	MemoryMB int               `yaml:"memoryMB"` // estimated resident memory, used by the scheduler
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

// Config is the root configuration.
type Config struct {
	Listen            string                 `yaml:"listen"`
	Token             string                 `yaml:"token"`
	DataDir           string                 `yaml:"dataDir"`
	EditorURLTemplate string                 `yaml:"editorUrlTemplate"`
	ExportMarkdown    bool                   `yaml:"exportMarkdown"`
	Agents            map[string]AgentConfig `yaml:"agents"`
	Permissions       PermissionPolicy       `yaml:"permissions"`
	Runner            RunnerConfig           `yaml:"runner"`
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
	}
}

// Load reads the config file at path (or <dataDir>/config.yaml when path is empty).
// A missing file yields defaults and writes a starter file.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		path = filepath.Join(cfg.DataDir, "config.yaml")
	}
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
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	if cfg.Runner.MaxConcurrent <= 0 {
		cfg.Runner.MaxConcurrent = 1
	}
	return cfg, nil
}

// Path helpers.
func (c *Config) DBPath() string     { return filepath.Join(c.DataDir, "xissue.db") }
func (c *Config) MirrorsDir() string { return filepath.Join(c.DataDir, "mirrors") }
func (c *Config) RunsDir() string    { return filepath.Join(c.DataDir, "runs") }
func (c *Config) IssuesDir() string  { return filepath.Join(c.DataDir, "issues") }
