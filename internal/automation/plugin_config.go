package automation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// PluginConfigFile is the conventional per-plugin JSON configuration.
	PluginConfigFile = "config.json"
	// PluginConfigExampleFile is used as a template when config.json is absent.
	PluginConfigExampleFile = "config.example.json"
)

const maxPluginAssetBytes = 1 << 20

// PluginConfig is the editable JSON configuration surfaced by the Web UI.
type PluginConfig struct {
	Exists   bool   `json:"exists"`
	Template bool   `json:"template"`
	Content  string `json:"content"`
	Path     string `json:"path"`
}

func pluginFileExists(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && info.Mode().IsRegular()
}

func readPluginAsset(dir, name string) ([]byte, string, error) {
	path, err := securePluginPath(dir, name)
	if err != nil {
		return nil, "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, path, err
	}
	if !info.Mode().IsRegular() {
		return nil, path, errors.New("not a regular file")
	}
	if info.Size() > maxPluginAssetBytes {
		return nil, path, fmt.Errorf("%s is too large (max %d bytes)", name, maxPluginAssetBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path, err
	}
	return data, path, nil
}

// ReadPluginReadme returns the plugin README used by the global manager.
func ReadPluginReadme(pluginsDir, id string) (name, content, path string, err error) {
	dir, err := findPluginDir(pluginsDir, id)
	if err != nil {
		return "", "", "", err
	}
	for _, candidate := range []string{"README.md", "readme.md", "README.markdown"} {
		data, p, readErr := readPluginAsset(dir, candidate)
		if readErr == nil {
			return candidate, string(data), p, nil
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return "", "", "", readErr
		}
	}
	return "", "", "", os.ErrNotExist
}

// ReadPluginConfig returns config.json, or a template when it has not been
// created yet. The UI can save the returned content to generate the file.
func ReadPluginConfig(pluginsDir, id string) (PluginConfig, error) {
	dir, err := findPluginDir(pluginsDir, id)
	if err != nil {
		return PluginConfig{}, err
	}
	configPath, err := securePluginPath(dir, PluginConfigFile)
	if err != nil {
		return PluginConfig{}, err
	}
	if data, _, readErr := readPluginAsset(dir, PluginConfigFile); readErr == nil {
		return PluginConfig{Exists: true, Content: string(data), Path: configPath}, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return PluginConfig{}, readErr
	}
	if data, _, readErr := readPluginAsset(dir, PluginConfigExampleFile); readErr == nil {
		return PluginConfig{Template: true, Content: string(data), Path: configPath}, nil
	}
	return PluginConfig{Content: "{}\n", Path: configPath}, nil
}

// WritePluginConfig validates JSON and atomically writes <plugin>/config.json.
func WritePluginConfig(pluginsDir, id, content string) (PluginConfig, error) {
	dir, err := findPluginDir(pluginsDir, id)
	if err != nil {
		return PluginConfig{}, err
	}
	if len(content) > maxPluginAssetBytes {
		return PluginConfig{}, fmt.Errorf("config.json is too large (max %d bytes)", maxPluginAssetBytes)
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(content), &probe); err != nil {
		return PluginConfig{}, fmt.Errorf("invalid JSON: %w", err)
	}
	if probe == nil {
		return PluginConfig{}, errors.New("config.json must be a JSON object")
	}
	configPath, err := securePluginPath(dir, PluginConfigFile)
	if err != nil {
		return PluginConfig{}, err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return PluginConfig{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(strings.TrimRight(content, "\n") + "\n"); err != nil {
		tmp.Close()
		return PluginConfig{}, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return PluginConfig{}, err
	}
	if err := tmp.Close(); err != nil {
		return PluginConfig{}, err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return PluginConfig{}, err
	}
	if err := os.Rename(tmpName, configPath); err != nil {
		return PluginConfig{}, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return PluginConfig{}, err
	}
	return PluginConfig{Exists: true, Content: string(data), Path: configPath}, nil
}
