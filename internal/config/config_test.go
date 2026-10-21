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
