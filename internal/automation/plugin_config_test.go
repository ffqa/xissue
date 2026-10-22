package automation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigPluginFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: 1
name: demo
version: 1.0.0
entrypoint: run.py
events: [issue_created]
permissions: [notify]
`
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.py"), []byte("print('{}')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Demo\n\nHello plugin.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.example.json"), []byte("{\n  \"channel\": \"#general\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, dir
}

func TestPluginReadmeAndConfigLifecycle(t *testing.T) {
	root, _ := writeConfigPluginFixture(t)

	name, readme, _, err := ReadPluginReadme(root, "demo")
	if err != nil {
		t.Fatalf("read readme: %v", err)
	}
	if name != "README.md" || !strings.Contains(readme, "Hello plugin") {
		t.Fatalf("unexpected readme: %q %q", name, readme)
	}

	cfg, err := ReadPluginConfig(root, "demo")
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if cfg.Exists || !cfg.Template || !strings.Contains(cfg.Content, "#general") {
		t.Fatalf("expected example template, got %+v", cfg)
	}

	cfg, err = WritePluginConfig(root, "demo", `{"channel":"#ops"}`)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}
	if !cfg.Exists || cfg.Template || !strings.Contains(cfg.Content, "#ops") {
		t.Fatalf("unexpected saved config: %+v", cfg)
	}
	info, err := os.Stat(filepath.Join(root, "demo", PluginConfigFile))
	if err != nil {
		t.Fatalf("config.json not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config.json mode = %o, want 600", info.Mode().Perm())
	}

	if _, err := WritePluginConfig(root, "demo", `{"broken":`); err == nil {
		t.Fatal("expected invalid JSON to be rejected")
	}
	if _, _, _, err := ReadPluginReadme(root, "../escape"); err == nil {
		t.Fatal("expected path traversal id to be rejected")
	}
}

func TestPluginInfoReportsReadmeConfig(t *testing.T) {
	root, _ := writeConfigPluginFixture(t)
	list := NewEngine(root).ListPlugins()
	if len(list) != 1 {
		t.Fatalf("expected one plugin, got %+v", list)
	}
	if !list[0].HasReadme {
		t.Fatal("expected HasReadme")
	}
	if list[0].HasConfig {
		t.Fatal("config.json is not created yet, HasConfig should be false")
	}
	if _, err := WritePluginConfig(root, "demo", `{}`); err != nil {
		t.Fatal(err)
	}
	if list := NewEngine(root).ListPlugins(); !list[0].HasConfig {
		t.Fatal("expected HasConfig after write")
	}
}
