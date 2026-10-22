package automation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initPluginRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins", "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: 1\nname: demo\nversion: 1.0.0\ndescription: demo plugin\nentrypoint: run.py\nevents: [issue_created]\npermissions: [notify]\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "run.py"), []byte("print('{}')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, PluginDisabledFile), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	runGit("init", "-q")
	runGit("add", "-A")
	runGit("commit", "-qm", "init")
	return dir
}

func TestInstallPluginFromCatalog(t *testing.T) {
	repo := initPluginRepo(t)
	catalog := catalogFile{Version: 1, Plugins: []CatalogEntry{{
		Name: "demo", Version: "1.0.0", Repo: repo, Path: "plugins/demo",
	}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(catalog)
	}))
	defer srv.Close()

	pluginsDir := filepath.Join(t.TempDir(), "plugins")
	res, err := InstallPlugin(context.Background(), pluginsDir, "demo", InstallOptions{CatalogURL: srv.URL})
	if err != nil {
		t.Fatalf("InstallPlugin: %v", err)
	}
	if res.Manifest.Name != "demo" {
		t.Fatalf("unexpected manifest: %+v", res.Manifest)
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "demo", "run.py")); err != nil {
		t.Fatalf("entrypoint missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "demo", PluginDisabledFile)); !os.IsNotExist(err) {
		t.Fatalf("install should clear disabled marker: %v", err)
	}
	if list := NewEngine(pluginsDir).ListPlugins(); len(list) != 1 || !list[0].Enabled {
		t.Fatalf("installed plugin not enabled: %+v", list)
	}
	if _, err := InstallPlugin(context.Background(), pluginsDir, "demo", InstallOptions{CatalogURL: srv.URL}); err == nil {
		t.Fatal("duplicate install should fail without --force")
	}
	if _, err := InstallPlugin(context.Background(), pluginsDir, "demo", InstallOptions{CatalogURL: srv.URL, Force: true}); err != nil {
		t.Fatalf("forced reinstall: %v", err)
	}
	if _, err := InstallPlugin(context.Background(), pluginsDir, "missing", InstallOptions{CatalogURL: srv.URL}); err == nil {
		t.Fatal("unknown catalog plugin should fail")
	}
}

func TestInstallPluginRejectsEscapingPath(t *testing.T) {
	repo := initPluginRepo(t)
	_, err := InstallPlugin(context.Background(), t.TempDir(), repo, InstallOptions{Path: "../outside"})
	if err == nil {
		t.Fatal("expected path escape rejection")
	}
}
