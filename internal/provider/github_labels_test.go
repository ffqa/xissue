package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestGitHubAddLabels(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "gh.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$GH_LOG\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("GH_LOG", logPath)

	repo := domain.Repo{Provider: domain.ProviderGitHub, OwnerRepo: "acme/app", Name: "app"}
	if err := (github{}).AddLabels(context.Background(), repo, 42, []string{"bug"}); err != nil {
		t.Fatalf("AddLabels: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	if !strings.Contains(log, "label create bug") || !strings.Contains(log, "issue edit -R acme/app 42 --add-label bug") {
		t.Fatalf("unexpected gh calls:\n%s", log)
	}
}
