package remoterunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebsocketURL(t *testing.T) {
	cases := []struct {
		in, want string
		bad      bool
	}{
		{"http://127.0.0.1:7789", "ws://127.0.0.1:7789/api/runners/ws", false},
		{"https://example.com/xissue/", "wss://example.com/xissue/api/runners/ws", false},
		{"ws://localhost:7789", "ws://localhost:7789/api/runners/ws", false},
		{"ftp://example.com", "", true},
	}
	for _, c := range cases {
		got, err := websocketURL(c.in)
		if c.bad {
			if err == nil {
				t.Fatalf("websocketURL(%q) succeeded: %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("websocketURL(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("websocketURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSafeJoinRejectsEscape(t *testing.T) {
	if _, err := safeJoin("/tmp/run-1", "../escape"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected escape error, got %v", err)
	}
	if got, err := safeJoin("/tmp/run-1", "repo"); err != nil || got != "/tmp/run-1/repo" {
		t.Fatalf("safeJoin valid = %q, %v", got, err)
	}
}

func TestCleanupPathScopedToDataDir(t *testing.T) {
	root := t.TempDir()
	r := &Runner{opts: Options{DataDir: root}}
	inside := filepath.Join(root, "runs", "run-1")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.cleanupPath(context.Background(), inside); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Fatalf("inside workdir still exists: %v", err)
	}
	if err := r.cleanupPath(context.Background(), filepath.Join(t.TempDir(), "outside")); err == nil {
		t.Fatal("expected outside path to be rejected")
	}
}
