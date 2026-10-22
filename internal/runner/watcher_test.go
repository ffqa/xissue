package runner

import (
	"testing"
	"time"

	"github.com/xpier/xissue/internal/domain"
)

func TestIsWithinActiveHours(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		t        time.Time
		expected bool
	}{
		{
			name:     "empty allows all",
			raw:      "",
			t:        time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC),
			expected: true,
		},
		{
			name:     "all-day string",
			raw:      "0-24",
			t:        time.Date(2026, 1, 1, 23, 59, 0, 0, time.UTC),
			expected: true,
		},
		{
			name:     "within regular daytime",
			raw:      "08:00-20:00",
			t:        time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC),
			expected: true,
		},
		{
			name:     "before daytime start",
			raw:      "08:00-20:00",
			t:        time.Date(2026, 1, 1, 7, 59, 0, 0, time.UTC),
			expected: false,
		},
		{
			name:     "after daytime end",
			raw:      "08:00-20:00",
			t:        time.Date(2026, 1, 1, 20, 0, 0, 0, time.UTC),
			expected: false,
		},
		{
			name:     "simple hour string",
			raw:      "8-20",
			t:        time.Date(2026, 1, 1, 19, 59, 0, 0, time.UTC),
			expected: true,
		},
		{
			name:     "cross midnight night shift - before midnight",
			raw:      "22:00-06:00",
			t:        time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC),
			expected: true,
		},
		{
			name:     "cross midnight night shift - after midnight",
			raw:      "22:00-06:00",
			t:        time.Date(2026, 1, 1, 4, 0, 0, 0, time.UTC),
			expected: true,
		},
		{
			name:     "cross midnight night shift - daytime outside",
			raw:      "22:00-06:00",
			t:        time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsWithinActiveHours(tt.raw, tt.t)
			if got != tt.expected {
				t.Errorf("IsWithinActiveHours(%q, %v) = %v; want %v", tt.raw, tt.t, got, tt.expected)
			}
		})
	}
}

func TestShouldRunProjectHooks(t *testing.T) {
	localDir := t.TempDir()
	localRepo := &domain.Repo{RemoteURL: localDir}
	remoteRepo := &domain.Repo{RemoteURL: "https://github.com/acme/repo.git"}
	rules := []domain.AutomationRule{{
		Enabled:    true,
		Trigger:    "branch_push",
		Conditions: domain.RuleConditions{Author: "@release-bot"},
	}}

	if !shouldRunProjectHooks(localRepo, localDir, "anyone", nil) {
		t.Fatal("local working copy should be allowed to run hooks")
	}
	if shouldRunProjectHooks(remoteRepo, "/tmp/worktree", "stranger", rules) {
		t.Fatal("remote repository with untrusted author must not run hooks")
	}
	if !shouldRunProjectHooks(remoteRepo, "/tmp/worktree", "release-bot", rules) {
		t.Fatal("remote repository with a trusted rule author should run hooks")
	}
	if shouldRunProjectHooks(remoteRepo, "", "release-bot", rules) {
		t.Fatal("missing workdir must not run hooks")
	}
	if shouldRunProjectHooks(nil, localDir, "anyone", nil) {
		t.Fatal("nil repo must not run hooks")
	}
}

func TestBranchPushLink(t *testing.T) {
	got := branchPushLink(2, 7, "feature/a b", "abc123")
	want := "/projects/2/commits?repo=7&branch=feature%2Fa+b&sha=abc123"
	if got != want {
		t.Fatalf("branchPushLink = %q, want %q", got, want)
	}
}
