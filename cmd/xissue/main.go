package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/runner"
	"github.com/xpier/xissue/internal/server"
	"github.com/xpier/xissue/internal/store"
)

func main() {
	runner.EnsureEnhancedPath()
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
		fmt.Fprintf(os.Stderr, "  service [cmd]        管理开机自启常驻服务 (install/uninstall/status/restart)\n")
		fmt.Fprintf(os.Stderr, "  update [--check]     检查并升级 xIssue (支持 Homebrew / 独立二进制自更新)\n")
		fmt.Fprintf(os.Stderr, "  mcp [--install]      运行或注册 MCP JSON-RPC 服务到本机 IDE / Agent\n")
		fmt.Fprintf(os.Stderr, "  plugin <cmd>         插件管理 (list/install/enable/disable/check)\n")
		fmt.Fprintf(os.Stderr, "  version              打印当前版本并退出\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVer || (flag.NArg() > 0 && flag.Arg(0) == "version") {
		fmt.Println("xissue v" + server.Version)
		return
	}

	if flag.NArg() > 0 && flag.Arg(0) == "service" {
		runService(flag.Args()[1:])
		return
	}

	if flag.NArg() > 0 && flag.Arg(0) == "update" {
		runUpdate(flag.Args()[1:])
		return
	}

	loadPath := *cfgPath
	if loadPath == "" && *dataDir != "" {
		// Keep test/dev instances fully isolated; otherwise config.Save would
		// write a -data-dir override back into the real ~/.xissue/config.yaml.
		loadPath = filepath.Join(*dataDir, "config.yaml")
	}
	cfg, err := config.Load(loadPath)
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
		if flag.NArg() > 1 {
			sub := strings.ToLower(strings.TrimLeft(flag.Arg(1), "-"))
			if sub == "install" || sub == "i" {
				transport := "stdio"
				for _, arg := range flag.Args()[2:] {
					if strings.EqualFold(strings.TrimSpace(arg), "--http") || strings.EqualFold(strings.TrimSpace(arg), "http") {
						transport = "http"
					}
				}
				exe, _ := os.Executable()
				if exe == "" {
					exe = "xissue"
				}
				headers := map[string]string{"X-XIssue": "1"}
				if strings.TrimSpace(cfg.Token) != "" {
					headers = map[string]string{"Authorization": "Bearer " + strings.TrimSpace(cfg.Token)}
				}
				installed, err := server.InstallMCPTargets(exe, nil, transport, server.MCPHTTPURL(cfg.Listen), headers)
				if err != nil {
					log.Fatalf("mcp install: %v", err)
				}
				if transport == "http" {
					fmt.Printf("Installed xIssue MCP over HTTP (%s) into: %s\n", server.MCPHTTPURL(cfg.Listen), strings.Join(installed, ", "))
				} else {
					fmt.Printf("Installed xIssue MCP (%s mcp) into: %s\n", exe, strings.Join(installed, ", "))
				}
				return
			}
			if sub == "http" {
				fmt.Printf("MCP Streamable HTTP endpoint: %s\n", server.MCPHTTPURL(cfg.Listen))
				fmt.Println("Clients supporting HTTP MCP can share this endpoint; no `xissue mcp` subprocess is needed per session.")
				return
			}
		}
		runMCPStdio(cfg)
		return
	}

	if flag.NArg() > 0 && flag.Arg(0) == "plugin" {
		runPlugin(cfg, flag.Args()[1:])
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
		req.Header.Set("X-XIssue", "1")
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

		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "xissue mcp server returned status %d: %s\n", resp.StatusCode, string(body))
			var reqMsg struct {
				ID any `json:"id"`
			}
			_ = json.Unmarshal(line, &reqMsg)
			if reqMsg.ID != nil {
				errResp, _ := json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      reqMsg.ID,
					"error": map[string]any{
						"code":    -32000,
						"message": fmt.Sprintf("server error: %s (status %d)", string(body), resp.StatusCode),
					},
				})
				os.Stdout.Write(errResp)
				os.Stdout.Write([]byte("\n"))
			}
			continue
		}

		if len(body) > 0 {
			os.Stdout.Write(body)
			os.Stdout.Write([]byte("\n"))
		}
	}
}
