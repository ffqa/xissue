package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasNewListener(t *testing.T) {
	if hasNewListener([]int{100}, []int{100}) {
		t.Fatal("same listener PID must not count as restarted")
	}
	if !hasNewListener([]int{100}, []int{100, 200}) {
		t.Fatal("a new listener PID must count as restarted")
	}
	if !hasNewListener(nil, []int{200}) {
		t.Fatal("first listener must count as restarted")
	}
	if hasNewListener([]int{100}, nil) {
		t.Fatal("no listener must not count as restarted")
	}
}

func TestLaunchdOutputHasPID(t *testing.T) {
	if launchdOutputHasPID("state = running\n\tpid = 123\n") != true {
		t.Fatal("running launchd job should expose a PID")
	}
	if launchdOutputHasPID("state = not running\n") {
		t.Fatal("stopped launchd job must not count as running")
	}
}

func TestParsePlistProgram(t *testing.T) {
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>dev.xissue</string>
  <key>ProgramArguments</key>
  <array>
    <string>/Users/x/.local/bin/xissue</string>
  </array>
  <key>WorkingDirectory</key>
  <string>/Users/x</string>
</dict>
</plist>`
	if got := parsePlistProgram(plist); got != "/Users/x/.local/bin/xissue" {
		t.Fatalf("parsePlistProgram = %q", got)
	}
	if got := parsePlistProgram("<dict><key>Label</key><string>x</string></dict>"); got != "" {
		t.Fatalf("plist without ProgramArguments should yield empty, got %q", got)
	}
}

func TestParseSystemdExecStart(t *testing.T) {
	unit := "[Service]\nType=simple\nExecStart=/home/x/.local/bin/xissue\nRestart=always\n"
	if got := parseSystemdExecStart(unit); got != "/home/x/.local/bin/xissue" {
		t.Fatalf("parseSystemdExecStart = %q", got)
	}
	if got := parseSystemdExecStart("[Service]\nType=simple\n"); got != "" {
		t.Fatalf("unit without ExecStart should yield empty, got %q", got)
	}
}

func TestSameExecutableFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !sameExecutableFile(a, a) {
		t.Fatal("same path must be the same file")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(a, link); err == nil {
		if !sameExecutableFile(link, a) {
			t.Fatal("symlink to a must resolve to the same file as a")
		}
	}
	if sameExecutableFile(a, b) {
		t.Fatal("different files must not be the same file")
	}
	if sameExecutableFile(a, filepath.Join(dir, "missing")) {
		t.Fatal("missing file must not be the same file")
	}
}
