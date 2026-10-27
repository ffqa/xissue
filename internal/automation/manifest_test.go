package automation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePluginFixture(t *testing.T, root, name, manifest string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadPluginManifestV1(t *testing.T) {
	root := t.TempDir()
	dir := writePluginFixture(t, root, "notifier", `apiVersion: 1
name: notifier
version: 1.2.3
description: test plugin
author: alice
entrypoint: run
events:
  - notification_dispatch
  - issue_created
permissions:
  - network
  - notify
`)
	m, _, err := LoadPluginManifest(dir)
	if err != nil {
		t.Fatalf("LoadPluginManifest: %v", err)
	}
	if m.Name != "notifier" || m.APIVersion != 1 || len(m.Events) != 2 || len(m.Permissions) != 2 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}

func TestLoadPluginManifestRejectsInvalidContracts(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name     string
		manifest string
		want     string
	}{
		{"bad-api", "apiVersion: 2\nname: p\nevents: [issue_created]\nentrypoint: run\n", "unsupported apiVersion"},
		{"no-events", "apiVersion: 1\nname: p\nentrypoint: run\n", "at least one"},
		{"unknown-event", "apiVersion: 1\nname: p\nevents: [nope]\nentrypoint: run\n", "unknown event"},
		{"bad-name", "apiVersion: 1\nname: Bad/Name\nevents: [issue_created]\nentrypoint: run\n", "invalid plugin name"},
		{"escape", "apiVersion: 1\nname: p\nevents: [issue_created]\nentrypoint: ../run\n", "invalid entrypoint"},
		{"unknown-permission", "apiVersion: 1\nname: p\nevents: [issue_created]\nentrypoint: run\npermissions: [root]\n", "unknown permission"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writePluginFixture(t, root, tc.name, tc.manifest)
			_, _, err := LoadPluginManifest(dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got err %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestListPluginsReportsDisabledAndBroken(t *testing.T) {
	root := t.TempDir()
	good := writePluginFixture(t, root, "good", "apiVersion: 1\nname: good\nevents: [issue_created]\nentrypoint: run\n")
	if err := os.WriteFile(filepath.Join(good, PluginDisabledFile), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(root, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "plugin.yaml"), []byte("apiVersion: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	list := NewEngine(root).ListPlugins()
	if len(list) != 2 {
		t.Fatalf("want 2 plugins, got %d", len(list))
	}
	byID := map[string]string{}
	for _, p := range list {
		byID[p.ID] = p.Error
		if p.ID == "good" && p.Enabled {
			t.Fatal("disabled marker was ignored")
		}
	}
	if !strings.Contains(byID["bad"], "unsupported apiVersion") {
		t.Fatalf("broken plugin error missing: %q", byID["bad"])
	}
}

func TestListPluginsSupportsSymlinkDevelopment(t *testing.T) {
	pluginsDir := t.TempDir()
	source := writePluginFixture(t, t.TempDir(), "dev-plugin", "apiVersion: 1\nname: dev-plugin\nevents: [issue_created]\nentrypoint: run\n")
	if err := os.Symlink(source, filepath.Join(pluginsDir, "dev-plugin")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	list := NewEngine(pluginsDir).ListPlugins()
	if len(list) != 1 || !list[0].Enabled {
		t.Fatalf("symlinked plugin not listed: %+v", list)
	}
	if err := SetPluginEnabled(pluginsDir, "dev-plugin", false); err != nil {
		t.Fatalf("disable symlinked plugin: %v", err)
	}
}

func TestSetPluginEnabledRejectsManifestDisabled(t *testing.T) {
	root := t.TempDir()
	dir := writePluginFixture(t, root, "locked", "apiVersion: 1\nname: locked\nevents: [issue_created]\nentrypoint: run\nenabled: false\n")
	if err := SetPluginEnabled(root, "locked", true); err == nil {
		t.Fatal("enabling a manifest-disabled plugin should fail")
	}
	if _, _, err := LoadPluginManifest(dir); err != nil {
		t.Fatal(err)
	}
}

func TestSetPluginEnabled(t *testing.T) {
	root := t.TempDir()
	writePluginFixture(t, root, "toggle", "apiVersion: 1\nname: toggle\nevents: [issue_created]\nentrypoint: run\n")
	if err := SetPluginEnabled(root, "toggle", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "toggle", PluginDisabledFile)); err != nil {
		t.Fatalf("disabled marker missing: %v", err)
	}
	if err := SetPluginEnabled(root, "toggle", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "toggle", PluginDisabledFile)); !os.IsNotExist(err) {
		t.Fatalf("disabled marker still present: %v", err)
	}
	if err := SetPluginEnabled(root, "../escape", false); err == nil {
		t.Fatal("path traversal should be rejected")
	}
}

func TestLoadPluginManifestUI(t *testing.T) {
	root := t.TempDir()
	dir := writePluginFixture(t, root, "dashboard", `apiVersion: 1
name: dashboard
events: [issue_created]
entrypoint: run
ui:
  entry: ui/index.html
  title: Issue dashboard
  slots: [project.page, issue.actions]
`)
	if err := os.MkdirAll(filepath.Join(dir, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ui", "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _, err := LoadPluginManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.UI == nil || m.UI.Entry != "ui/index.html" || m.UI.Title != "Issue dashboard" || len(m.UI.Slots) != 2 {
		t.Fatalf("ui = %+v", m.UI)
	}
	list := NewEngine(root).ListPlugins()
	if len(list) != 1 || list[0].UI == nil || list[0].UI.Entry != "ui/index.html" {
		t.Fatalf("listed ui = %+v", list)
	}
}

func TestLoadPluginManifestRejectsInvalidUI(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name     string
		manifest string
	}{
		{"outside-ui", "apiVersion: 1\nname: p\nevents: [issue_created]\nentrypoint: run\nui:\n  entry: index.html\n"},
		{"traversal", "apiVersion: 1\nname: p\nevents: [issue_created]\nentrypoint: run\nui:\n  entry: ui/../run\n"},
		{"unknown-slot", "apiVersion: 1\nname: p\nevents: [issue_created]\nentrypoint: run\nui:\n  entry: ui/index.html\n  slots: [unknown.slot]\n"},
		{"missing-entry", "apiVersion: 1\nname: p\nevents: [issue_created]\nentrypoint: run\nui:\n  entry: ui/missing.html\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writePluginFixture(t, root, tc.name, tc.manifest)
			_ = os.MkdirAll(filepath.Join(dir, "ui"), 0o755)
			if tc.name != "missing-entry" && tc.name != "traversal" {
				_ = os.WriteFile(filepath.Join(dir, "ui", "index.html"), []byte("x"), 0o644)
			}
			_, _, err := LoadPluginManifest(dir)
			if err == nil {
				t.Fatal("expected invalid ui to be rejected")
			}
		})
	}
}

func TestResolvePluginUIFileRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "p")
	if err := os.MkdirAll(filepath.Join(dir, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ui", "ok.html"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ResolvePluginUIFile(dir, "ok.html"); err != nil || !strings.HasSuffix(got, "ui/ok.html") {
		t.Fatalf("ok = %q, %v", got, err)
	}
	if _, err := ResolvePluginUIFile(dir, "../secret.txt"); err == nil {
		t.Fatal("traversal must be rejected")
	}
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "ui", "link.html")); err == nil {
		if _, err := ResolvePluginUIFile(dir, "link.html"); err == nil {
			t.Fatal("symlink escape must be rejected")
		}
	}
}
