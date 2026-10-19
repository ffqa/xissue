package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/server"
	"github.com/xpier/xissue/internal/store"
)

func main() {
	var (
		cfgPath = flag.String("config", "", "path to config.yaml (default ~/.xissue/config.yaml)")
		listen  = flag.String("listen", "", "override listen address (e.g. 127.0.0.1:7789)")
		dataDir = flag.String("data-dir", "", "override data directory (default ~/.xissue)")
		token   = flag.String("token", "", "optional static bearer/cookie token")
		showVer = flag.Bool("version", false, "print version and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: xissue [options] [subcommand]\n\n")
		fmt.Fprintf(os.Stderr, "Subcommands:\n")
		fmt.Fprintf(os.Stderr, "  update [--check]     检查并升级 xIssue (支持 Homebrew / 独立二进制自更新)\n")
		fmt.Fprintf(os.Stderr, "  mcp [--install]      运行或注册 MCP JSON-RPC 服务到本机 IDE / Agent\n")
		fmt.Fprintf(os.Stderr, "  version              打印当前版本并退出\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVer || (flag.NArg() > 0 && flag.Arg(0) == "version") {
		fmt.Println("xissue v" + server.Version)
		return
	}

	if flag.NArg() > 0 && flag.Arg(0) == "update" {
		runUpdate(flag.Args()[1:])
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if *token != "" {
		cfg.Token = *token
	}

	if flag.NArg() > 0 && flag.Arg(0) == "mcp" {
		if flag.NArg() > 1 && flag.Arg(1) == "install" {
			exe, _ := os.Executable()
			if exe == "" {
				exe = "xissue"
			}
			installed, err := server.InstallMCPTargets(exe, nil)
			if err != nil {
				log.Fatalf("mcp install: %v", err)
			}
			fmt.Printf("Installed xIssue MCP (%s mcp) into: %s\n", exe, strings.Join(installed, ", "))
			return
		}
		runMCPStdio(cfg)
		return
	}

	for _, dir := range []string{cfg.DataDir, cfg.MirrorsDir(), cfg.RunsDir(), cfg.IssuesDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	srv := server.New(cfg, st)
	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("xIssue shutting down gracefully...")
		srv.Shutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}()

	log.Printf("xIssue v%s listening on http://%s (data: %s)", server.Version, cfg.Listen, cfg.DataDir)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// runMCPStdio bridges standard MCP JSON-RPC 2.0 over stdin/stdout to the running xIssue HTTP server.
func runMCPStdio(cfg *config.Config) {
	addr := cfg.Listen
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	} else if strings.HasPrefix(addr, "0.0.0.0:") {
		addr = "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	endpoint := "http://" + addr + "/api/mcp"
	client := &http.Client{Timeout: 30 * time.Second}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		req, err := http.NewRequest("POST", endpoint, bytes.NewReader(line))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.Token != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "xissue mcp forward error: %v\n", err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		body = bytes.TrimSpace(body)
		if len(body) > 0 && resp.StatusCode == http.StatusOK {
			os.Stdout.Write(body)
			os.Stdout.Write([]byte("\n"))
		}
	}
}

