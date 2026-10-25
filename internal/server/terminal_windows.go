//go:build windows

package server

import (
	"errors"
	"os"
	"os/exec"
)

func startRunPTY(string, int, int) (*os.File, *exec.Cmd, error) {
	return nil, nil, errors.New("interactive terminal is not supported on windows")
}

func resizePTY(*os.File, int, int) error {
	return errors.New("interactive terminal is not supported on windows")
}
