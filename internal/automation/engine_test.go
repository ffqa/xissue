package automation

import (
	"os"
	"path/filepath"
	"testing"

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
