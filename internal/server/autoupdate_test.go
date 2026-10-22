package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/updater"
)

func TestAutoUpdateNotifiesOnNewVersionAndHandlesFailure(t *testing.T) {
	srv, st, _ := newTestServer(t)
	srv.cfg.SetAutoUpdate(true)
	oldFetch, oldRun, oldEligible := fetchLatestRelease, runUpdateCommand, canAutoUpdate
	t.Cleanup(func() { fetchLatestRelease, runUpdateCommand, canAutoUpdate = oldFetch, oldRun, oldEligible })
	canAutoUpdate = func(string) bool { return true }

	fetchLatestRelease = func(context.Context) (*updater.Release, error) {
		return &updater.Release{TagName: "v99.0.0"}, nil
	}
	called := false
	runUpdateCommand = func(context.Context, string) ([]byte, error) {
		called = true
		return []byte("boom"), errors.New("download failed")
	}

	srv.checkAutoUpdate()
	if !called {
		t.Fatal("expected update command to run")
	}
	list, err := st.ListNotifications(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range list {
		if n.Kind == "update" && strings.Contains(n.Title, "发现新版本") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing update notification: %+v", list)
	}

	// Same version within the retry window must not run again.
	called = false
	srv.checkAutoUpdate()
	if called {
		t.Fatal("update retried inside retry window")
	}
}

func TestAutoUpdateDisabledDoesNothing(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.cfg.SetAutoUpdate(false)
	oldFetch := fetchLatestRelease
	t.Cleanup(func() { fetchLatestRelease = oldFetch })
	fetchLatestRelease = func(context.Context) (*updater.Release, error) {
		t.Fatal("fetch should not be called when auto update is disabled")
		return nil, nil
	}
	srv.checkAutoUpdate()
}

func TestAutoUpdateEligibility(t *testing.T) {
	home, _ := os.UserHomeDir()
	if !autoUpdateEligible(filepath.Join(home, ".local", "bin", "xissue")) {
		t.Fatal("persistent binary should be eligible")
	}
	if autoUpdateEligible("/tmp/xissue-dev") {
		t.Fatal("temporary binary must not auto-update the installed service")
	}
}
