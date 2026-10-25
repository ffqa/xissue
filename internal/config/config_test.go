package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentConfigAccessAndAtomicSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := range 40 {
				switch i % 4 {
				case 0:
					_ = cfg.UpdateMonitor(MonitorConfig{Enabled: j%2 == 0, ActiveHours: "00:00-24:00"})
				case 1:
					_ = cfg.SetAgent("worker", AgentConfig{Command: "echo", MemoryMB: 100})
				case 2:
					_, _ = cfg.Agent("worker")
					_ = cfg.AgentsSnapshot()
					_ = cfg.MonitorSnapshot()
					_ = cfg.PermissionsSnapshot()
					_ = cfg.RunnerSnapshot()
					_ = cfg.ExportMarkdownEnabled()
					_ = cfg.EditorTemplate()
				case 3:
					_ = cfg.DeleteAgent("worker")
				}
			}
		}(i)
	}
	wg.Wait()

	if _, err := Load(path); err != nil {
		t.Fatalf("config file corrupted after concurrent saves: %v", err)
	}
	tmp, err := filepath.Glob(filepath.Join(dir, ".config-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmp) != 0 {
		t.Fatalf("temporary config files leaked: %v", tmp)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestPublicBaseURLAndAbsoluteURL(t *testing.T) {
	cfg := Default()
	cfg.Listen = "127.0.0.1:7789"
	if got := cfg.PublicBaseURL(); got != "http://127.0.0.1:7789" {
		t.Fatalf("derived base URL = %q", got)
	}
	if got := cfg.AbsoluteURL("/projects/2?watch=5"); got != "http://127.0.0.1:7789/projects/2?watch=5" {
		t.Fatalf("absolute URL = %q", got)
	}
	if got := cfg.AbsoluteURL("https://example.com/x"); got != "https://example.com/x" {
		t.Fatalf("absolute URL should stay unchanged, got %q", got)
	}

	cfg.Monitor = MonitorConfig{PublicURL: "100.64.0.2:7789/"}
	if got := cfg.PublicBaseURL(); got != "http://100.64.0.2:7789" {
		t.Fatalf("explicit base URL = %q", got)
	}
	if got := cfg.AbsoluteURL("/projects/1"); got != "http://100.64.0.2:7789/projects/1" {
		t.Fatalf("explicit absolute URL = %q", got)
	}
}

func TestMonitorSecretsAreMaskedAndPreserved(t *testing.T) {
	cfg := Default()
	dir := t.TempDir()
	cfg.DataDir = dir
	cfg.Path = filepath.Join(dir, "config.yaml")
	cfg.Monitor.TelegramBotToken = "BOT-SECRET"
	cfg.Monitor.TelegramChatID = "123"
	cfg.Monitor.WebhookURL = "https://hooks.example/secret"

	masked := cfg.MaskedMonitorSnapshot()
	if masked.TelegramBotToken != MaskedSecret || masked.WebhookURL != MaskedSecret {
		t.Fatalf("secrets not masked: %+v", masked)
	}
	if masked.TelegramChatID != "123" {
		t.Fatalf("non-secret chat id should stay visible: %+v", masked)
	}

	// A masked round-trip must not overwrite the stored credentials.
	if err := cfg.UpdateMonitor(masked); err != nil {
		t.Fatalf("UpdateMonitor: %v", err)
	}
	if cfg.MonitorSnapshot().TelegramBotToken != "BOT-SECRET" || cfg.MonitorSnapshot().WebhookURL != "https://hooks.example/secret" {
		t.Fatalf("masked values overwrote secrets: %+v", cfg.MonitorSnapshot())
	}

	// Partial pause must leave credentials untouched.
	disabled := false
	if err := cfg.PatchMonitor(MonitorPatch{Enabled: &disabled}); err != nil {
		t.Fatalf("PatchMonitor: %v", err)
	}
	got := cfg.MonitorSnapshot()
	if got.Enabled || got.TelegramBotToken != "BOT-SECRET" || got.WebhookURL != "https://hooks.example/secret" {
		t.Fatalf("partial patch clobbered config: %+v", got)
	}

	// A real replacement still works.
	replacement := "NEW-TOKEN"
	if err := cfg.PatchMonitor(MonitorPatch{TelegramBotToken: &replacement}); err != nil {
		t.Fatalf("PatchMonitor token: %v", err)
	}
	if cfg.MonitorSnapshot().TelegramBotToken != "NEW-TOKEN" {
		t.Fatalf("token not replaced: %+v", cfg.MonitorSnapshot())
	}

	autoDevelop := true
	if err := cfg.PatchMonitor(MonitorPatch{AutoDevelopIssues: &autoDevelop}); err != nil {
		t.Fatalf("PatchMonitor autoDevelop: %v", err)
	}
	if !cfg.MonitorSnapshot().AutoDevelopIssues {
		t.Fatalf("autoDevelopIssues not patched: %+v", cfg.MonitorSnapshot())
	}
}

func TestAgentSnapshotsAreDefensiveCopies(t *testing.T) {
	cfg := Default()
	cfg.Agents = map[string]AgentConfig{
		"demo": {Command: "echo", Args: []string{"acp"}, Env: map[string]string{"A": "1"}},
	}
	snap := cfg.AgentsSnapshot()
	snap["demo"].Args[0] = "mutated"
	snap["demo"].Env["A"] = "mutated"
	snap["other"] = AgentConfig{Command: "bad"}

	agent, ok := cfg.Agent("demo")
	if !ok {
		t.Fatal("agent not found")
	}
	if agent.Args[0] != "acp" || agent.Env["A"] != "1" {
		t.Fatalf("snapshot mutation leaked into config: %+v", agent)
	}
	if _, ok := cfg.Agent("other"); ok {
		t.Fatal("map mutation leaked into config")
	}
}

func TestAutoUpdateDefaultsOn(t *testing.T) {
	if !Default().AutoUpdate {
		t.Fatal("auto update should default to enabled")
	}
	// Loading a legacy file without the key must keep the default (true).
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:7789\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoUpdate {
		t.Fatal("legacy config without autoUpdate should keep default true")
	}
}

func TestNotificationRouteSanitizing(t *testing.T) {
	routes := map[string][]string{
		"pr_status":   {"telegram", "webhook", "telegram", " nope "},
		"unknown":     {"telegram"},
		"branch_push": {},
	}
	got := sanitizeNotificationRoutes(routes)
	if len(got) != 2 {
		t.Fatalf("sanitized routes = %#v, want two known events", got)
	}
	if len(got["pr_status"]) != 2 || got["pr_status"][0] != "telegram" || got["pr_status"][1] != "webhook" {
		t.Fatalf("pr_status routes = %#v", got["pr_status"])
	}
	if got["branch_push"] == nil || len(got["branch_push"]) != 0 {
		t.Fatalf("branch_push should stay empty, got %#v", got["branch_push"])
	}
}

func TestMigrateDeprecatedCodexACP(t *testing.T) {
	agents := map[string]AgentConfig{
		"codex": {Command: "npx", Args: []string{"-y", "@zed-industries/codex-acp"}},
		"other": {Command: "npx", Args: []string{"-y", "@zed-industries/codex-acp@0.16.0"}},
		"keep":  {Command: "opencode", Args: []string{"acp"}},
	}
	migrateDeprecatedAgents(agents)
	if got := agents["codex"].Args[1]; got != "@agentclientprotocol/codex-acp" {
		t.Fatalf("codex arg = %q", got)
	}
	if got := agents["other"].Args[1]; got != "@agentclientprotocol/codex-acp" {
		t.Fatalf("versioned arg = %q", got)
	}
	if got := agents["keep"].Args[0]; got != "acp" {
		t.Fatalf("unrelated agent changed: %q", got)
	}
}

func TestAgentRolesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	roles := AgentRolesConfig{
		IssueReview:  "reviewer",
		IssueDevelop: "developer",
		PRReview:     "pr-reviewer",
		AutoMerge:    "merger",
	}
	cfg.SetAgentRoles(roles)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.AgentRolesSnapshot(); got != roles {
		t.Fatalf("roles = %+v, want %+v", got, roles)
	}
}

func TestLoadMigratesIssueSyncDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	raw := "monitor:\n  enabled: false\n  syncIssues: false\n  syncIntervalMin: 10\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Monitor.SyncIssues || cfg.Monitor.SyncIntervalMin != 10 {
		t.Fatalf("migration overrode explicit values: syncIssues:%v interval:%d", cfg.Monitor.SyncIssues, cfg.Monitor.SyncIntervalMin)
	}
	if cfg.ConfigVersion != CurrentConfigVersion {
		t.Fatalf("configVersion = %d", cfg.ConfigVersion)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Monitor.SyncIssues || reloaded.Monitor.SyncIntervalMin != 10 {
		t.Fatalf("reload lost explicit values: %+v", reloaded.Monitor)
	}
}

func TestLoadKeepsIssueSyncDefaultsOnlyWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("monitor:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Monitor.SyncIssues || cfg.Monitor.SyncIntervalMin != 5 {
		t.Fatalf("missing fields did not receive defaults: %+v", cfg.Monitor)
	}
}

func TestRunnerRetentionDefaultsAndExplicitDisable(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.yaml")
	if err := os.WriteFile(missing, []byte("runner:\n  maxConcurrent: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runner.EventRetentionDays != 7 || cfg.Runner.WorkspaceRetentionDays != 7 {
		t.Fatalf("missing retention defaults = %+v", cfg.Runner)
	}
	if cfg.Runner.MaxQueuedRuns != 100 {
		t.Fatalf("missing queue default = %+v", cfg.Runner)
	}
	disabled := filepath.Join(dir, "disabled.yaml")
	if err := os.WriteFile(disabled, []byte("runner:\n  eventRetentionDays: 0\n  workspaceRetentionDays: 0\n  maxQueuedRuns: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(disabled)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runner.EventRetentionDays != 0 || cfg.Runner.WorkspaceRetentionDays != 0 || cfg.Runner.MaxQueuedRuns != 0 {
		t.Fatalf("explicit disable was overwritten: %+v", cfg.Runner)
	}
}

func TestLegacyConfigRemovesFetchFromAutoAllow(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.yaml")
	body := "configVersion: 2\npermissions:\n  autoAllowKinds: [read, search, fetch, edit]\n"
	if err := os.WriteFile(legacy, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigVersion != CurrentConfigVersion {
		t.Fatalf("configVersion = %d", cfg.ConfigVersion)
	}
	for _, kind := range cfg.Permissions.AutoAllowKinds {
		if kind == "fetch" {
			t.Fatalf("legacy fetch auto-allow survived migration: %+v", cfg.Permissions.AutoAllowKinds)
		}
	}

	explicit := filepath.Join(dir, "explicit.yaml")
	body = "configVersion: 3\npermissions:\n  autoAllowKinds: [fetch]\n"
	if err := os.WriteFile(explicit, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Permissions.AutoAllowKinds) != 1 || cfg.Permissions.AutoAllowKinds[0] != "fetch" {
		t.Fatalf("explicit fetch setting was overwritten: %+v", cfg.Permissions.AutoAllowKinds)
	}
}
