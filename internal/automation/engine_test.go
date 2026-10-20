package automation

import (
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
}
