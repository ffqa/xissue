package automation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PluginAPIVersion is the manifest contract understood by this binary.
const PluginAPIVersion = 1

// PluginDisabledFile is a marker that keeps a source-controlled manifest intact.
const PluginDisabledFile = ".disabled"

// Lifecycle events that may be declared by plugins and hooks.
const (
	EventBranchPush         = "branch_push"
	EventPRMerged           = "pr_merged"
	EventPRCreated          = "pr_created"
	EventPRComment          = "pr_comment"
	EventPRReview           = "pr_review"
	EventNotification       = "notification_dispatch"
	EventIssueCreated       = "issue_created"
	EventIssueStatusChanged = "issue_status_changed"
	EventRunStarted         = "run_started"
	EventRunFinished        = "run_finished"
)

var knownPluginEvents = map[string]struct{}{
	EventBranchPush:         {},
	EventPRMerged:           {},
	EventPRCreated:          {},
	EventPRComment:          {},
	EventPRReview:           {},
	EventNotification:       {},
	EventIssueCreated:       {},
	EventIssueStatusChanged: {},
	EventRunStarted:         {},
	EventRunFinished:        {},
}

// Permissions are declarations used for audit/UI. They are not an OS sandbox:
// plugins run as the current user, so only install code you trust.
var knownPluginPermissions = map[string]struct{}{
	"repo:read":  {},
	"repo:write": {},
	"network":    {},
	"notify":     {},
	"process":    {},
}

var pluginNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// PluginManifest is the v1 plugin.yaml contract.
type PluginManifest struct {
	APIVersion  int      `yaml:"apiVersion" json:"apiVersion"`
	Name        string   `yaml:"name" json:"name"`
	Version     string   `yaml:"version" json:"version"`
	Description string   `yaml:"description" json:"description"`
	Author      string   `yaml:"author" json:"author"`
	Entrypoint  string   `yaml:"entrypoint" json:"entrypoint"`
	Events      []string `yaml:"events" json:"events"`
	Permissions []string `yaml:"permissions" json:"permissions"`
	Enabled     *bool    `yaml:"enabled" json:"enabled"`
}

// KnownPluginEvents returns the supported event names in stable order.
func KnownPluginEvents() []string {
	out := make([]string, 0, len(knownPluginEvents))
	for name := range knownPluginEvents {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// LoadPluginManifest reads and validates plugin.yaml (or legacy plugin.json).
func LoadPluginManifest(dir string) (*PluginManifest, string, error) {
	manifestPath := filepath.Join(dir, "plugin.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		manifestPath = filepath.Join(dir, "plugin.json")
		if _, jerr := os.Stat(manifestPath); jerr != nil {
			return nil, "", errors.New("missing plugin.yaml")
		}
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, "", err
	}
	var m PluginManifest
	if strings.HasSuffix(manifestPath, ".json") {
		err = json.Unmarshal(data, &m)
	} else {
		err = yaml.Unmarshal(data, &m)
	}
	if err != nil {
		return nil, manifestPath, fmt.Errorf("parse manifest: %w", err)
	}
	if m.APIVersion == 0 {
		// Legacy manifests are treated as v1 so existing installs keep working.
		m.APIVersion = PluginAPIVersion
	}
	if err := ValidatePluginManifest(dir, &m); err != nil {
		return nil, manifestPath, err
	}
	return &m, manifestPath, nil
}

// ValidatePluginManifest validates the contract and the entrypoint containment.
func ValidatePluginManifest(dir string, m *PluginManifest) error {
	if m.APIVersion != PluginAPIVersion {
		return fmt.Errorf("unsupported apiVersion %d (want %d)", m.APIVersion, PluginAPIVersion)
	}
	if m.Name == "" {
		m.Name = filepath.Base(dir)
	}
	if !pluginNamePattern.MatchString(m.Name) {
		return fmt.Errorf("invalid plugin name %q (use lowercase letters, digits, dot, dash, underscore)", m.Name)
	}
	if m.Version == "" {
		m.Version = "0.0.0"
	}
	if m.Entrypoint == "" {
		m.Entrypoint = "run"
	}
	if filepath.IsAbs(m.Entrypoint) || strings.Contains(m.Entrypoint, "..") {
		return fmt.Errorf("invalid entrypoint %q", m.Entrypoint)
	}
	entryPath, err := securePluginPath(dir, m.Entrypoint)
	if err != nil {
		return err
	}
	info, err := os.Stat(entryPath)
	if err != nil {
		return fmt.Errorf("entrypoint %q not found", m.Entrypoint)
	}
	if info.IsDir() {
		return fmt.Errorf("entrypoint %q is a directory", m.Entrypoint)
	}
	if len(m.Events) == 0 {
		return errors.New("events must declare at least one lifecycle event")
	}
	seen := map[string]bool{}
	for _, ev := range m.Events {
		ev = strings.TrimSpace(ev)
		if ev == "all" || ev == "*" {
			seen[ev] = true
			continue
		}
		if _, ok := knownPluginEvents[ev]; !ok {
			return fmt.Errorf("unknown event %q (known: %s)", ev, strings.Join(KnownPluginEvents(), ", "))
		}
		seen[ev] = true
	}
	for _, perm := range m.Permissions {
		if _, ok := knownPluginPermissions[perm]; !ok {
			return fmt.Errorf("unknown permission %q", perm)
		}
	}
	if len(m.Permissions) == 0 {
		m.Permissions = []string{}
	}
	return nil
}

// securePluginPath resolves a relative path and rejects symlink escapes.
func securePluginPath(dir, rel string) (string, error) {
	base, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	target := filepath.Join(base, filepath.FromSlash(rel))
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if abs != base && !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes plugin directory", rel)
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	realTarget, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// The final component may not exist yet (e.g. install copy checks).
		return abs, nil
	}
	if realTarget != realBase && !strings.HasPrefix(realTarget, realBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes plugin directory via symlink", rel)
	}
	return realTarget, nil
}

func pluginEventMatches(manifestEvents []string, event string) bool {
	for _, ev := range manifestEvents {
		if ev == event || ev == "all" || ev == "*" {
			return true
		}
	}
	return false
}
