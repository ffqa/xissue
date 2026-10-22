package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/xpier/xissue/internal/automation"
	"github.com/xpier/xissue/internal/config"
)

func runPlugin(cfg *config.Config, args []string) {
	if len(args) == 0 {
		pluginUsage()
		return
	}
	pluginsDir := cfg.PluginsDir()
	switch strings.ToLower(args[0]) {
	case "list", "ls":
		listPluginsCmd(pluginsDir)
	case "install", "add":
		installPluginCmd(pluginsDir, args[1:])
	case "enable":
		setPluginEnabledCmd(pluginsDir, args[1:], true)
	case "disable":
		setPluginEnabledCmd(pluginsDir, args[1:], false)
	case "check", "validate":
		checkPluginCmd(args[1:])
	default:
		pluginUsage()
		os.Exit(2)
	}
}

func pluginUsage() {
	fmt.Fprint(os.Stderr, `Usage: xissue plugin <command>

Commands:
  list                         List installed plugins and validation status
  install <name|owner/repo>    Install from the curated catalog or a GitHub repo
  enable <id>                  Enable an installed plugin
  disable <id>                 Disable an installed plugin without editing its manifest
  check [dir]                  Validate a plugin manifest and entrypoint

Install options:
  --path <subdir>              Plugin subdirectory inside a monorepo
  --branch <name>              Git branch to clone
  --catalog <url>              Override the plugin index (or XISSUE_PLUGIN_INDEX)
  --force                      Replace an existing install
`)
}

func listPluginsCmd(pluginsDir string) {
	plugins := automation.NewEngine(pluginsDir).ListPlugins()
	if len(plugins) == 0 {
		fmt.Printf("No plugins installed in %s\n", pluginsDir)
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tVERSION\tAPI\tEVENTS\tPERMISSIONS\tSTATUS")
	for _, p := range plugins {
		events := append([]string(nil), p.Events...)
		sort.Strings(events)
		perms := append([]string(nil), p.Permissions...)
		sort.Strings(perms)
		status := "enabled"
		if !p.Enabled {
			status = "disabled"
		}
		if p.Error != "" {
			status = "invalid: " + p.Error
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\tv%d\t%s\t%s\t%s\n",
			p.ID, p.Name, p.Version, p.APIVersion,
			strings.Join(events, ","), strings.Join(perms, ","), status)
	}
	_ = tw.Flush()
}

func installPluginCmd(pluginsDir string, args []string) {
	fs := flag.NewFlagSet("plugin install", flag.ContinueOnError)
	path := fs.String("path", "", "plugin subdirectory inside the repository")
	branch := fs.String("branch", "", "git branch to clone")
	catalog := fs.String("catalog", "", "plugin index URL")
	force := fs.Bool("force", false, "replace an existing install")
	if err := fs.Parse(args); err != nil {
		return
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: xissue plugin install <name|owner/repo> [--path subdir] [--force]")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := automation.InstallPlugin(ctx, pluginsDir, fs.Arg(0), automation.InstallOptions{
		Path:        *path,
		Force:       *force,
		CatalogURL:  *catalog,
		CloneBranch: *branch,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "install failed:", err)
		os.Exit(1)
	}
	fmt.Printf("Installed %s v%s (api v%d) -> %s\n", res.Manifest.Name, res.Manifest.Version, res.Manifest.APIVersion, res.Dir)
	fmt.Printf("Events: %s\n", strings.Join(res.Manifest.Events, ", "))
	if len(res.Manifest.Permissions) > 0 {
		fmt.Printf("Permissions declared: %s\n", strings.Join(res.Manifest.Permissions, ", "))
		fmt.Println("Note: plugins run as your user. Permission declarations are informational, not a sandbox.")
	}
}

func setPluginEnabledCmd(pluginsDir string, args []string, enabled bool) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: xissue plugin enable|disable <id>")
		os.Exit(2)
	}
	if err := automation.SetPluginEnabled(pluginsDir, args[0], enabled); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	action := "Enabled"
	if !enabled {
		action = "Disabled"
	}
	fmt.Printf("%s plugin %s\n", action, args[0])
}

func checkPluginCmd(args []string) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m, path, err := automation.LoadPluginManifest(abs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid plugin at %s: %v\n", abs, err)
		os.Exit(1)
	}
	fmt.Printf("OK %s v%s (api v%d)\n", m.Name, m.Version, m.APIVersion)
	fmt.Printf("Manifest: %s\n", path)
	fmt.Printf("Entrypoint: %s\n", m.Entrypoint)
	fmt.Printf("Events: %s\n", strings.Join(m.Events, ", "))
	if len(m.Permissions) > 0 {
		fmt.Printf("Permissions declared: %s\n", strings.Join(m.Permissions, ", "))
	}
}
