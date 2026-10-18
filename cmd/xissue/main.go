package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

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
	flag.Parse()

	if *showVer || (flag.NArg() > 0 && flag.Arg(0) == "version") {
		fmt.Println("xissue v" + server.Version)
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
	log.Printf("xIssue v%s listening on http://%s (data: %s)", server.Version, cfg.Listen, cfg.DataDir)
	if err := http.ListenAndServe(cfg.Listen, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
