//go:build !darwin && !linux && !windows

package server

func processCPUSeconds() float64 { return 0 }

func diskUsage(string) (total, free uint64, err error) { return 0, 0, nil }
