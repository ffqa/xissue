package automation

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/domain"
)

func TestMatchBranchPushRule(t *testing.T) {
	rule := domain.AutomationRule{
		ID:      "r1",
		Name:    "Dev branch push",
		Enabled: true,
		Trigger: "branch_push",
		Conditions: domain.RuleConditions{
			Branch:  "dev",
			Author:  "release-bot",
			Pattern: `(?i)^feat:`,
		},
		Actions: []domain.RuleAction{
			{Type: "create_issue"},
		},
	}

	// 1. 完全匹配
	if !MatchBranchPushRule(rule, "dev", "release-bot", "feat: add user login") {
		t.Errorf("expected rule to match, got false")
	}

	// 2. 分支不匹配
	if MatchBranchPushRule(rule, "main", "release-bot", "feat: add user login") {
		t.Errorf("expected branch mismatch, got true")
	}

	// 3. 作者不匹配
	if MatchBranchPushRule(rule, "dev", "alice", "feat: add user login") {
		t.Errorf("expected author mismatch, got true")
	}

	// 4. 正则不匹配
	if MatchBranchPushRule(rule, "dev", "release-bot", "docs: update readme") {
		t.Errorf("expected pattern mismatch, got true")
	}

	// 5. 正则分支匹配 (例如 release/.*)
	ruleRegex := domain.AutomationRule{
		ID:      "r2",
		Enabled: true,
		Trigger: "branch_push",
		Conditions: domain.RuleConditions{
			Branch: `^release/v\d+`,
		},
	}
	if !MatchBranchPushRule(ruleRegex, "release/v1.2", "bob", "any message") {
		t.Errorf("expected regex branch to match, got false")
	}
	if MatchBranchPushRule(ruleRegex, "feature/v1.2", "bob", "any message") {
		t.Errorf("expected regex branch to not match, got true")
	}

	// 6. 作者必须是精确身份，bob 不应命中 bobby
	authorRule := domain.AutomationRule{
		ID:      "r3",
		Enabled: true,
		Trigger: "branch_push",
		Conditions: domain.RuleConditions{
			Author: "bob",
		},
	}
	if !MatchBranchPushRule(authorRule, "main", "@Bob", "msg") {
		t.Errorf("expected case-insensitive author match")
	}
	if MatchBranchPushRule(authorRule, "main", "bobby", "msg") {
		t.Errorf("substring author must not match")
	}
}

func TestListPluginsDisablesMalformedManifest(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "bad-plugin")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "plugin.yaml"), []byte("events: [branch_push\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "run.py"), []byte("print('x')\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	good := filepath.Join(root, "good-plugin")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: 1\nname: good\nevents:\n  - branch_push\nentrypoint: run.py\n"
	if err := os.WriteFile(filepath.Join(good, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "run.py"), []byte("print('x')\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	plugins := NewEngine(root).ListPlugins()
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(plugins))
	}
	for _, p := range plugins {
		switch p.ID {
		case "bad-plugin":
			if p.Enabled {
				t.Fatal("malformed manifest must not enable the plugin")
			}
		case "good-plugin":
			if !p.Enabled {
				t.Fatal("valid manifest should be enabled")
			}
		}
	}
}

func writeRunPlugin(t *testing.T, root, name, extraManifest, script string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: 1\nname: " + name + "\nevents: [branch_push]\nentrypoint: run\n" + extraManifest
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// A plugin that crashes, times out, or prints garbage must surface in the
// returned error (id + reason + stderr tail) without blocking other plugins.
func TestExecutePluginsAggregatesFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script plugins require unix")
	}
	root := t.TempDir()
	writeRunPlugin(t, root, "good", "",
		"#!/bin/sh\necho '{\"actions\":[{\"type\":\"notify\",\"message\":\"hi\"}]}'\n")
	writeRunPlugin(t, root, "crasher", "",
		"#!/bin/sh\necho 'boom detail' >&2\nexit 1\n")
	writeRunPlugin(t, root, "sleeper", "timeout: 1s\n",
		"#!/bin/sh\necho '{\"actions\":[{\"type\":\"notify\",\"message\":\"late\"}]}'\nsleep 10\n")
	writeRunPlugin(t, root, "noisy", "",
		"#!/bin/sh\necho 'this is not json'\n")

	acts, err := NewEngine(root).ExecutePlugins(context.Background(), PREventContext{Event: "branch_push"})
	if err == nil {
		t.Fatal("expected aggregated plugin failures")
	}
	msg := err.Error()
	for _, want := range []string{"crasher", "exit status 1", "boom detail", "sleeper", "timeout", "noisy", "non-JSON"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q: %s", want, msg)
		}
	}
	// good's actions plus the JSON the sleeper printed before being killed.
	if len(acts) != 2 {
		t.Fatalf("expected 2 actions (good + salvaged timeout output), got %d (%v)", len(acts), acts)
	}
}

func TestPluginManifestTimeout(t *testing.T) {
	m := &PluginManifest{}
	if m.TimeoutDuration() != 15*time.Second {
		t.Fatalf("default timeout = %v", m.TimeoutDuration())
	}
	m.Timeout = "30s"
	if m.TimeoutDuration() != 30*time.Second {
		t.Fatalf("parsed timeout = %v", m.TimeoutDuration())
	}
	m.Timeout = "garbage"
	if m.TimeoutDuration() != 15*time.Second {
		t.Fatalf("invalid timeout must fall back to default, got %v", m.TimeoutDuration())
	}
}
