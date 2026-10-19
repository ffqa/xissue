//go:build !windows

package server

import (
	"os"
	"syscall"
)

func execSelf(exe string) {
	_ = syscall.Exec(exe, os.Args, os.Environ())
}
