package server

import (
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSPAGzipAssets(t *testing.T) {
	sub, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		t.Skipf("dist unavailable: %v", err)
	}
	var asset string
	_ = fs.WalkDir(sub, "assets", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".js") {
			asset = p
			return io.EOF
		}
		return nil
	})
	if asset == "" {
		t.Skip("no built assets available")
	}
	h := spaHandler()
	req := httptest.NewRequest(http.MethodGet, "/"+asset, nil)
	req.Host = "127.0.0.1:7789"
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("gzip asset: %d encoding=%q", w.Code, w.Header().Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip body: %v", err)
	}
	body, err := io.ReadAll(zr)
	_ = zr.Close()
	if err != nil || len(body) == 0 {
		t.Fatalf("decompress asset: %d bytes, %v", len(body), err)
	}
	if !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("missing Vary: %v", w.Header())
	}
}
