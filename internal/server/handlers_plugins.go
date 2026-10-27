package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/xpier/xissue/internal/automation"
	"github.com/xpier/xissue/internal/domain"
)

func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	plugins := s.runner.ListPlugins()
	if plugins == nil {
		plugins = []domain.PluginInfo{}
	}
	writeJSON(w, http.StatusOK, plugins)
}

func (s *Server) handlePatchPlugin(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, errors.New("plugin id is required"))
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Enabled == nil {
		writeErr(w, http.StatusBadRequest, errors.New("enabled is required"))
		return
	}
	if err := automation.SetPluginEnabled(s.cfg.PluginsDir(), id, *body.Enabled); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	plugins := s.runner.ListPlugins()
	if plugins == nil {
		plugins = []domain.PluginInfo{}
	}
	writeJSON(w, http.StatusOK, plugins)
}

func (s *Server) handlePluginReadme(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	name, content, path, err := automation.ReadPluginReadme(s.cfg.PluginsDir(), id)
	if errors.Is(err, os.ErrNotExist) {
		writeErr(w, http.StatusNotFound, errors.New("plugin README not found"))
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    name,
		"content": content,
		"path":    path,
	})
}

func (s *Server) handlePluginConfig(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	switch r.Method {
	case http.MethodGet:
		cfg, err := automation.ReadPluginConfig(s.cfg.PluginsDir(), id)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var body struct {
			Content string `json:"content"`
		}
		if err := readJSON(r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		cfg, err := automation.WritePluginConfig(s.cfg.PluginsDir(), id, body.Content)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePluginUI serves a plugin's static UI. The iframe is sandboxed by the
// frontend; this handler additionally prevents traversal outside <plugin>/ui.
func (s *Server) handlePluginUI(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid plugin id"))
		return
	}
	pluginDir := filepath.Join(s.cfg.PluginsDir(), id)
	manifest, _, err := automation.LoadPluginManifest(pluginDir)
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("plugin not found or invalid"))
		return
	}
	if manifest.UI == nil {
		writeErr(w, http.StatusNotFound, errors.New("plugin has no ui"))
		return
	}
	rel := strings.TrimSpace(r.PathValue("path"))
	if rel == "" {
		rel = strings.TrimPrefix(manifest.UI.Entry, "ui/")
	}
	target, err := automation.ResolvePluginUIFile(pluginDir, rel)
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("ui asset not found"))
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, target)
}
