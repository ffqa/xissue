package server

import (
	"bytes"
	"compress/gzip"
	"embed"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

//go:embed all:dist
var embeddedDist embed.FS

var gzipCache sync.Map // asset path -> []byte

// spaHandler serves the embedded SPA and gzip-compresses text assets on the
// first request. Hashed assets are cached immutably, so recompression only
// happens once per process for each build artifact.
func spaHandler() http.Handler {
	sub, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(sub))
	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		_, _ = w.Write(b)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeErr(w, http.StatusNotFound, errors.New("api endpoint not found"))
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" || p == "index.html" {
			serveIndex(w, r)
			return
		}
		if _, err := fs.Stat(sub, p); err == nil {
			if strings.HasPrefix(p, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			if acceptsGzip(r) && compressibleAsset(p) {
				if gz, gerr := gzippedAsset(sub, p); gerr == nil {
					if ct := assetContentType(p); ct != "" {
						w.Header().Set("Content-Type", ct)
					}
					w.Header().Set("Content-Encoding", "gzip")
					w.Header().Set("Vary", "Accept-Encoding")
					w.Header().Set("Content-Length", strconv.Itoa(len(gz)))
					if r.Method != http.MethodHead {
						_, _ = w.Write(gz)
					}
					return
				}
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback to index.html for client-side routes.
		serveIndex(w, r)
	})
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		fields := strings.Split(part, ";")
		if strings.TrimSpace(fields[0]) != "gzip" {
			continue
		}
		enabled := true
		for _, f := range fields[1:] {
			if strings.TrimSpace(f) == "q=0" {
				enabled = false
			}
		}
		if enabled {
			return true
		}
	}
	return false
}

func assetContentType(p string) string {
	ext := strings.ToLower(path.Ext(p))
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	switch ext {
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".html":
		return "text/html; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".wasm":
		return "application/wasm"
	}
	return ""
}

func compressibleAsset(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".js", ".css", ".html", ".json", ".svg", ".map", ".txt", ".wasm", ".xml":
		return true
	}
	return false
}

func gzippedAsset(sub fs.FS, p string) ([]byte, error) {
	if v, ok := gzipCache.Load(p); ok {
		return v.([]byte), nil
	}
	raw, err := fs.ReadFile(sub, p)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return raw, nil
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	gzipCache.Store(p, out)
	return out, nil
}
