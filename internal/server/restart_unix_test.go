//go:build !windows

package server

import (
	"os"
	"testing"
)

func TestUnitFromCgroup(t *testing.T) {
	cases := map[string]string{
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/xissue.service\n": "xissue.service",
		"12:devices:/system.slice/xissue.service\n":                                   "xissue.service",
		"0::/user.slice/user-1000.slice/session-1.scope\n":                            "",
		"": "",
	}
	for raw, want := range cases {
		if got := unitFromCgroup(raw); got != want {
			t.Errorf("unitFromCgroup(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestServiceManagedFromEnv(t *testing.T) {
	t.Setenv("XISSUE_SERVICE", "1")
	if !serviceManaged() {
		t.Fatal("XISSUE_SERVICE=1 should mark the process as supervised")
	}
	t.Setenv("XISSUE_SERVICE", "")
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("SYSTEMD_EXEC_PID", "")
	if os.Getppid() == 1 {
		t.Skip("test runner is itself supervised")
	}
	if serviceManaged() {
		t.Fatal("plain test process should not be reported as supervised")
	}
}
