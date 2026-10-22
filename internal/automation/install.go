package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultCatalogURL is the curated first-party plugin index.
const DefaultCatalogURL = "https://raw.githubusercontent.com/ffqa/xissue-plugins/main/index.json"

var repoSpecPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// CatalogEntry describes one plugin in the curated index.
type CatalogEntry struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Repo        string   `json:"repo"`
	Path        string   `json:"path"`
	Events      []string `json:"events"`
	Permissions []string `json:"permissions"`
}

type catalogFile struct {
	Version int            `json:"version"`
	Plugins []CatalogEntry `json:"plugins"`
}

// InstallOptions controls plugin installation.
type InstallOptions struct {
	Path        string // optional subdirectory inside the repository
	Force       bool
	CatalogURL  string
	HTTPClient  *http.Client
	GitBinary   string
	CloneBranch string
}

// InstallResult reports the installed plugin.
type InstallResult struct {
	Manifest *PluginManifest
	Dir      string
	Source   string
}

// FetchCatalog downloads and validates the curated plugin index.
func FetchCatalog(ctx context.Context, url string, client *http.Client) ([]CatalogEntry, error) {
	if strings.TrimSpace(url) == "" {
		url = DefaultCatalogURL
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "xissue-plugin-installer")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog returned status %d", resp.StatusCode)
	}
	var cf catalogFile
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&cf); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}
	if cf.Version != 1 {
		return nil, fmt.Errorf("unsupported catalog version %d", cf.Version)
	}
	for i := range cf.Plugins {
		if strings.TrimSpace(cf.Plugins[i].Name) == "" {
			return nil, fmt.Errorf("catalog entry %d has no name", i)
		}
		if cf.Plugins[i].Repo == "" {
			cf.Plugins[i].Repo = "ffqa/xissue-plugins"
		}
	}
	return cf.Plugins, nil
}

// InstallPlugin installs an ecosystem plugin from either a catalog name or a
// GitHub owner/repo spec. The optional path selects a subdirectory (monorepo).
func InstallPlugin(ctx context.Context, pluginsDir, spec string, opts InstallOptions) (*InstallResult, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("plugin name or owner/repo is required")
	}
	if pluginsDir == "" {
		return nil, errors.New("plugins directory is required")
	}
	if opts.CatalogURL == "" {
		opts.CatalogURL = os.Getenv("XISSUE_PLUGIN_INDEX")
	}
	if opts.CatalogURL == "" {
		opts.CatalogURL = DefaultCatalogURL
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	if opts.GitBinary == "" {
		opts.GitBinary = "git"
	}

	repoURL := spec
	subPath := opts.Path
	source := spec
	if !strings.Contains(spec, "/") || strings.HasPrefix(spec, "file://") || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") {
		if !strings.Contains(spec, "/") {
			entries, err := FetchCatalog(ctx, opts.CatalogURL, opts.HTTPClient)
			if err != nil {
				return nil, err
			}
			var found *CatalogEntry
			for i := range entries {
				if strings.EqualFold(entries[i].Name, spec) {
					found = &entries[i]
					break
				}
			}
			if found == nil {
				return nil, fmt.Errorf("plugin %q not found in catalog %s", spec, opts.CatalogURL)
			}
			repoURL = found.Repo
			if subPath == "" {
				subPath = found.Path
			}
			source = "catalog:" + found.Name
		}
	}

	cloneURL := repoURL
	if repoSpecPattern.MatchString(repoURL) {
		cloneURL = "https://github.com/" + repoURL + ".git"
	} else if !strings.Contains(repoURL, "://") && !filepath.IsAbs(repoURL) {
		return nil, fmt.Errorf("invalid repository %q (want owner/repo or a clone URL)", repoURL)
	}

	tmpRoot, err := os.MkdirTemp("", "xissue-plugin-install-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpRoot)
	cloneDir := filepath.Join(tmpRoot, "repo")
	args := []string{"clone", "--depth", "1"}
	if opts.CloneBranch != "" {
		args = append(args, "--branch", opts.CloneBranch)
	}
	args = append(args, cloneURL, cloneDir)
	cmd := exec.CommandContext(ctx, opts.GitBinary, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git clone failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	srcDir := cloneDir
	if strings.TrimSpace(subPath) != "" {
		clean := filepath.Clean(filepath.FromSlash(subPath))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("invalid plugin path %q", subPath)
		}
		srcDir = filepath.Join(cloneDir, clean)
		if !strings.HasPrefix(srcDir, cloneDir+string(filepath.Separator)) {
			return nil, fmt.Errorf("invalid plugin path %q", subPath)
		}
	}
	manifest, _, err := LoadPluginManifest(srcDir)
	if err != nil {
		return nil, fmt.Errorf("validate %s: %w", srcDir, err)
	}
	dstDir := filepath.Join(pluginsDir, manifest.Name)
	if _, statErr := os.Stat(dstDir); statErr == nil && !opts.Force {
		return nil, fmt.Errorf("plugin %q is already installed (use --force to replace)", manifest.Name)
	}

	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(pluginsDir, ".install-"+manifest.Name+"-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	if err := copyPluginDir(srcDir, staging); err != nil {
		return nil, err
	}
	if _, _, err := LoadPluginManifest(staging); err != nil {
		return nil, fmt.Errorf("staged plugin invalid: %w", err)
	}
	if opts.Force {
		backup := dstDir + ".bak"
		_ = os.RemoveAll(backup)
		if _, statErr := os.Stat(dstDir); statErr == nil {
			if err := os.Rename(dstDir, backup); err != nil {
				return nil, fmt.Errorf("backup existing plugin: %w", err)
			}
		}
		if err := os.Rename(staging, dstDir); err != nil {
			_ = os.Rename(backup, dstDir)
			return nil, fmt.Errorf("install plugin: %w", err)
		}
		_ = os.RemoveAll(backup)
	} else {
		if err := os.Rename(staging, dstDir); err != nil {
			return nil, fmt.Errorf("install plugin: %w", err)
		}
	}
	_ = os.Remove(filepath.Join(dstDir, PluginDisabledFile))
	return &InstallResult{Manifest: manifest, Dir: dstDir, Source: source}, nil
}

func copyPluginDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		base := d.Name()
		if d.IsDir() && (base == ".git" || base == "node_modules" || base == "__pycache__") {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin contains symlink %q; symlinks are not supported", rel)
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

// SetPluginEnabled toggles a plugin without editing its source manifest.
func SetPluginEnabled(pluginsDir, id string, enabled bool) error {
	dir, err := findPluginDir(pluginsDir, id)
	if err != nil {
		return err
	}
	marker := filepath.Join(dir, PluginDisabledFile)
	if enabled {
		if m, _, err := LoadPluginManifest(dir); err == nil && m.Enabled != nil && !*m.Enabled {
			return fmt.Errorf("plugin %s is disabled in plugin.yaml (enabled: false); edit the manifest first", id)
		}
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(marker, []byte("disabled by xissue plugin disable\n"), 0o644)
}

func findPluginDir(pluginsDir, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
		return "", errors.New("invalid plugin id")
	}
	direct := filepath.Join(pluginsDir, id)
	if info, err := os.Stat(direct); err == nil && info.IsDir() {
		return direct, nil
	}
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(pluginsDir, entry.Name())
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		if m, _, err := LoadPluginManifest(dir); err == nil && strings.EqualFold(m.Name, id) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("plugin %q not found", id)
}
