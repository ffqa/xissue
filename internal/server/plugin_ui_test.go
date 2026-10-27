package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/store"
)

func TestPluginUIServedFromSandboxDirectory(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = root
	cfg.Monitor.Enabled = false
	pluginDir := filepath.Join(cfg.PluginsDir(), "demo")
	if err := os.MkdirAll(filepath.Join(pluginDir, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: 1
name: demo
events: [issue_created]
entrypoint: run
ui:
  entry: ui/index.html
  title: Demo
  slots: [project.page]
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "run"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "ui", "index.html"), []byte("<html>demo-ui</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := New(cfg, st)
	defer srv.Shutdown()
	hs := httptest.NewServer(srv.Handler())
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/api/plugins/demo/ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "<html>demo-ui</html>" {
		t.Fatalf("GET ui = %d %q", resp.StatusCode, body)
	}

	resp, err = http.Get(hs.URL + "/api/plugins/demo/ui/missing.html")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing asset status = %d", resp.StatusCode)
	}
}
