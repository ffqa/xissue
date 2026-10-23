package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
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
	want := "/projects/2?commits=1&repo=7&branch=feature%2Fa+b&sha=abc123"
	if got != want {
		t.Fatalf("branchPushLink = %q, want %q", got, want)
	}
}

func TestValidPullBranches(t *testing.T) {
	got := validPullBranches([]string{" main ", "dev", "main", "", "-x", "a..b", "feature/x"})
	want := []string{"main", "dev", "feature/x"}
	if len(got) != len(want) {
		t.Fatalf("validPullBranches = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("validPullBranches = %v, want %v", got, want)
		}
	}
}

func TestFetchBranchesOnlySelected(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")
	clone := filepath.Join(root, "clone")
	mustGit := func(dir string, args ...string) string {
		t.Helper()
		out, err := gitx.Run(ctx, dir, args...)
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return out
	}
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit(root, "init", "--bare", remote)
	runGit(root, "init", "-b", "main", work)
	mustGit(work, "config", "user.email", "test@example.com")
	mustGit(work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(work, "add", ".")
	mustGit(work, "commit", "-m", "init")
	mustGit(work, "branch", "dev")
	mustGit(work, "remote", "add", "origin", remote)
	mustGit(work, "push", "origin", "main", "dev")
	runGit(root, "clone", remote, clone)
	// Start from a clone that has both remote-tracking refs, then remove dev to
	// prove the selected fetch does not re-create it.
	mustGit(clone, "update-ref", "-d", "refs/remotes/origin/dev")
	fetchBranches(ctx, clone, []string{"main"})
	if _, err := gitx.Run(ctx, clone, "rev-parse", "--verify", "refs/remotes/origin/main"); err != nil {
		t.Fatalf("expected origin/main after selected fetch: %v", err)
	}
	if _, err := gitx.Run(ctx, clone, "rev-parse", "--verify", "refs/remotes/origin/dev"); err == nil {
		t.Fatal("origin/dev must not be fetched when only main is selected")
	}
}

func TestNotificationRouteAllows(t *testing.T) {
	if !notificationRouteAllows(nil, "pr_status", "telegram") {
		t.Fatal("empty routes must preserve legacy send-all behavior")
	}
	routes := map[string][]string{
		"pr_status":   {"telegram"},
		"branch_push": {},
	}
	if !notificationRouteAllows(routes, "pr_status", "telegram") {
		t.Fatal("telegram should be allowed for pr_status")
	}
	if notificationRouteAllows(routes, "pr_status", "webhook") {
		t.Fatal("webhook must be filtered out for pr_status")
	}
	if notificationRouteAllows(routes, "branch_push", "telegram") {
		t.Fatal("an explicitly empty route must disable all channels")
	}
	if !notificationRouteAllows(routes, "run_done", "webhook") {
		t.Fatal("unlisted event should keep the legacy default")
	}
}

func TestEffectiveAutomationRulesProjectScope(t *testing.T) {
	cfg := config.Default()
	cfg.GlobalAutomationRules = []domain.AutomationRule{
		{ID: "all", Name: "all"},
		{ID: "scoped", Name: "scoped", ProjectIDs: []int64{2}},
		{ID: "other", Name: "other", ProjectIDs: []int64{3}},
	}
	r := &Runner{cfg: cfg}
	project := &domain.Project{ID: 2, AutomationRules: []domain.AutomationRule{{ID: "local"}}}
	rules := r.effectiveAutomationRules(project)
	ids := make([]string, 0, len(rules))
	for _, rule := range rules {
		ids = append(ids, rule.ID)
	}
	want := []string{"all", "scoped", "local"}
	if len(ids) != len(want) {
		t.Fatalf("rules = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("rules = %v, want %v", ids, want)
		}
	}
}
