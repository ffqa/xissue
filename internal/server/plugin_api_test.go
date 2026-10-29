package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginManagerAPI(t *testing.T) {
	srv, _, tmpDir := newTestServer(t)
	pluginDir := filepath.Join(tmpDir, "plugins", "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: 1
name: demo
version: 1.0.0
entrypoint: run.py
events: [issue_created]
permissions: [notify]
`
	files := map[string]string{
		"plugin.yaml":         manifest,
		"run.py":              "print('{}')\n",
		"README.md":           "# Demo\n\nPlugin readme.\n",
		"config.example.json": "{\n  \"channel\": \"#general\"\n}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(pluginDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:7789"
		req.Header.Set("X-XIssue", "1")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w
	}

	w := do(http.MethodGet, "/api/plugins", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"hasReadme":true`) {
		t.Fatalf("list plugins: %d %s", w.Code, w.Body.String())
	}

	w = do(http.MethodGet, "/api/plugins/ai", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatalf("plugin AI info: %d %s", w.Code, w.Body.String())
	}
	w = do(http.MethodPost, "/api/plugins/ai", `{"prompt":"  "}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty plugin AI prompt: %d %s", w.Code, w.Body.String())
	}

	w = do(http.MethodGet, "/api/plugins/demo/readme", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Plugin readme") {
		t.Fatalf("readme: %d %s", w.Code, w.Body.String())
	}

	w = do(http.MethodGet, "/api/plugins/demo/config", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"template":true`) {
		t.Fatalf("config template: %d %s", w.Code, w.Body.String())
	}

	w = do(http.MethodPut, "/api/plugins/demo/config", `{"content":"{\"channel\":\"#ops\"}"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save config: %d %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(pluginDir, "config.json"))
	if err != nil || !strings.Contains(string(data), "#ops") {
		t.Fatalf("config not written: %v %s", err, data)
	}

	w = do(http.MethodPatch, "/api/plugins/demo", `{"enabled":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("disable plugin: %d %s", w.Code, w.Body.String())
	}
	var list []struct {
		ID        string `json:"id"`
		Enabled   bool   `json:"enabled"`
		HasReadme bool   `json:"hasReadme"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Enabled || !list[0].HasReadme {
		t.Fatalf("unexpected plugin list: %+v", list)
	}
}
