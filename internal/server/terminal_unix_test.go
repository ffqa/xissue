//go:build !windows

package server

import (
	"strings"
	"testing"
	"time"
)

func TestStartRunPTYRunsInWorkspace(t *testing.T) {
	dir := t.TempDir()
	ptmx, cmd, err := startRunPTY(dir, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	if _, err := ptmx.Write([]byte("printf 'PTY_OK\\n'; pwd; exit\n")); err != nil {
		t.Fatal(err)
	}
	out := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 2048)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
				if strings.Contains(b.String(), "PTY_OK") && strings.Contains(b.String(), dir) {
					out <- b.String()
					return
				}
			}
			if err != nil {
				out <- b.String()
				return
			}
		}
	}()
	select {
	case got := <-out:
		if !strings.Contains(got, "PTY_OK") || !strings.Contains(got, dir) {
			t.Fatalf("pty output = %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for pty output")
	}
}
