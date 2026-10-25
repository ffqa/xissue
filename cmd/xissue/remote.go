package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/xpier/xissue/internal/remoterunner"
	"github.com/xpier/xissue/internal/server"
)

func runRemoteRunner(args []string) {
	fs := flag.NewFlagSet("runner", flag.ExitOnError)
	serverURL := fs.String("server", "", "xIssue server URL (http://host:7789)")
	token := fs.String("token", os.Getenv("XISSUE_TOKEN"), "server access token")
	name := fs.String("name", "", "runner name shown on the server (default: hostname)")
	dataDir := fs.String("data-dir", "", "runner data directory (default ~/.xissue-runner)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: xissue runner --server http://HOST:7789 --token TOKEN [--name NAME]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return
	}
	if strings.TrimSpace(*serverURL) == "" {
		fs.Usage()
		os.Exit(2)
	}
	hostname, _ := os.Hostname()
	if strings.TrimSpace(*name) == "" {
		*name = hostname
	}
	if strings.TrimSpace(*name) == "" {
		*name = "remote-runner"
	}
	dir := strings.TrimSpace(*dataDir)
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("home dir: %v", err)
		}
		dir = filepath.Join(home, ".xissue-runner")
	}
	for _, d := range []string{dir, filepath.Join(dir, "runs"), filepath.Join(dir, "mirrors"), filepath.Join(dir, "archive")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Fatalf("mkdir %s: %v", d, err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := log.New(os.Stderr, "xissue-runner: ", log.LstdFlags)
	logger.Printf("connecting to %s as %q (data %s)", *serverURL, *name, dir)
	err := remoterunner.Run(ctx, remoterunner.Options{
		Server: *serverURL, Token: *token, Name: *name, DataDir: dir,
		Version: server.Version, Logger: logger,
	})
	if err != nil && ctx.Err() == nil {
		log.Fatalf("remote runner: %v", err)
	}
}
