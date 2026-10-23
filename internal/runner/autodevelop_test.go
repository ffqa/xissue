package runner

import (
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
)

func TestIsDevelopableIssue(t *testing.T) {
	defaults := domain.DefaultAutoDevelopLabels()
	tests := []struct {
		name    string
		issue   domain.Issue
		labels  []string
		useText bool
		want    bool
	}{
		{
			name:  "label match is case insensitive",
			issue: domain.Issue{Title: "Something odd", Labels: []string{"BUG"}},
			want:  true,
		},
		{
			name:    "chinese requirement keyword in title",
			issue:   domain.Issue{Title: "希望增加导出报表功能"},
			useText: true,
			want:    true,
		},
		{
			name:    "english keyword in body with word boundary",
			issue:   domain.Issue{Title: "Export", Body: "This is a feature request for CSV export."},
			useText: true,
			want:    true,
		},
		{
			name:    "debug must not count as bug",
			issue:   domain.Issue{Title: "Improve debug logging", Body: "Add more debug output."},
			useText: true,
			want:    false,
		},
		{
			name:  "plain question is ignored",
			issue: domain.Issue{Title: "How do I configure the port?", Body: "Where is the setting?"},
			want:  false,
		},
		{
			name:   "empty configured list does not match anything",
			issue:  domain.Issue{Title: "Crash on startup"},
			labels: []string{},
			want:   false,
		},
		{
			name:   "custom keyword",
			issue:  domain.Issue{Title: "Sentry: nil pointer", Labels: []string{"panic"}},
			labels: []string{"panic"},
			want:   true,
		},
		{
			name:   "custom list excludes default words",
			issue:  domain.Issue{Title: "优化页面"},
			labels: []string{"panic"},
			want:   false,
		},
		{
			name:    "keyword is ignored when text matching is off",
			issue:   domain.Issue{Title: "Crash on startup"},
			labels:  defaults,
			useText: false,
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := tt.labels
			if tt.labels == nil {
				labels = defaults
			}
			if got := IsDevelopableIssue(tt.issue, labels, tt.useText); got != tt.want {
				t.Fatalf("IsDevelopableIssue(%q) = %v, want %v", tt.issue.Title, got, tt.want)
			}
		})
	}
}

func TestAutoDevelopPromptDoesNotAuthorizeMerge(t *testing.T) {
	issue := &domain.Issue{Key: "gh-42", Title: "Fix crash"}
	prompt := AutoDevelopPrompt(issue)
	for _, want := range []string{"gh-42", "Do NOT merge", "do NOT create the PR"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func TestDefaultAgentName(t *testing.T) {
	cfg := config.Default()
	cfg.Agents = map[string]config.AgentConfig{
		"zeta":     {Command: "zeta"},
		"opencode": {Command: "opencode"},
	}
	r := &Runner{cfg: cfg}
	if got := r.DefaultAgentName(); got != "opencode" {
		t.Fatalf("DefaultAgentName = %q, want opencode", got)
	}
	cfg.DefaultAgent = "zeta"
	if got := r.DefaultAgentName(); got != "zeta" {
		t.Fatalf("configured DefaultAgentName = %q, want zeta", got)
	}
	cfg.DefaultAgent = "missing"
	if got := r.DefaultAgentName(); got != "opencode" {
		t.Fatalf("missing DefaultAgent fallback = %q, want opencode", got)
	}

	cfg.Agents = map[string]config.AgentConfig{
		"zeta":  {Command: "zeta"},
		"alpha": {Command: "alpha"},
	}
	if got := r.DefaultAgentName(); got != "alpha" {
		t.Fatalf("DefaultAgentName = %q, want alpha", got)
	}

	cfg.Agents = map[string]config.AgentConfig{}
	if got := r.DefaultAgentName(); got != "" {
		t.Fatalf("DefaultAgentName = %q, want empty", got)
	}
}
