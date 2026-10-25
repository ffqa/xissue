//go:build !windows

package server

import (
	"os"
	"os/exec"
	"strings"

	"github.com/creack/pty"
)

func startRunPTY(cwd string, cols, rows int) (*os.File, *exec.Cmd, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Dir = cwd
	cmd.Env = append(terminalEnv(os.Environ()), "TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, nil, err
	}
	return ptmx, cmd, nil
}

// terminalEnv keeps the operator's shell usable without handing out the
// server's platform tokens. Agent processes already strip these names.
func terminalEnv(environ []string) []string {
	drop := map[string]bool{
		"XISSUE_TOKEN":       true,
		"XISSUE_GITEA_TOKEN": true,
		"GITEA_TOKEN":        true,
		"GITEA_ACCESS_TOKEN": true,
	}
	out := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, ok := strings.Cut(entry, "=")
		if ok && drop[key] {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func resizePTY(f *os.File, cols, rows int) error {
	return pty.Setsize(f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
