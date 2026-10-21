//go:build windows

package automation

import "os/exec"

// setProcessGroup is a no-op on Windows: the child is killed directly.
func setProcessGroup(cmd *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
