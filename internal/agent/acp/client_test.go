package acp

import (
	"errors"
	"strings"
	"testing"
)

func TestExitErrorIncludesStderrTail(t *testing.T) {
	c := &Client{exitErr: errors.New("exit status 1")}
	c.rememberStderr("npm error code E404")
	c.rememberStderr("npm error 404 Not Found")

	err := c.exitError()
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "exit status 1") || !strings.Contains(msg, "npm error code E404") {
		t.Fatalf("error lacks exit/stderr detail: %q", msg)
	}
}

func TestRememberStderrIsBounded(t *testing.T) {
	c := &Client{}
	for i := 0; i < 100; i++ {
		c.rememberStderr(strings.Repeat("x", 1024))
	}
	if len(c.stderrTail) > 12 || c.stderrSize > 8*1024 {
		t.Fatalf("stderr tail not bounded: lines=%d bytes=%d", len(c.stderrTail), c.stderrSize)
	}
}
