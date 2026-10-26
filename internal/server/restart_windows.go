//go:build windows

package server

func execSelf(_ string) {}

func serviceManaged() bool { return false }

func requestSupervisorRestart() bool { return false }
