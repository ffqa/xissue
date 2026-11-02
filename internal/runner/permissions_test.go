package runner

import (
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/store"
)

func TestAutoApproveAllBypassesAlwaysAsk(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := New(cfg, st, nil)
	t.Cleanup(r.Shutdown)

	opts := []acp.PermissionOption{
		{OptionID: "allow-once", Kind: "allow_once"},
		{OptionID: "reject", Kind: "reject_once"},
	}
	tc := map[string]any{"rawInput": "git push origin main"}
	if !cfg.Permissions.AutoApproveAll {
		t.Fatal("yolo/bypass should be the default")
	}
	if got := r.autoApproveOption(1, "execute", "git push origin main", tc, opts); got != "allow-once" {
		t.Fatalf("autoApproveAll option = %q, want allow-once", got)
	}

	cfg.Permissions.AutoApproveAll = false
	if got := r.autoApproveOption(1, "execute", "git push origin main", tc, opts); got != "" {
		t.Fatalf("with prompts enabled execute should ask, got %q", got)
	}
	if got := r.autoApproveOption(1, "read", "read file", map[string]any{}, opts); got != "allow-once" {
		t.Fatalf("read should stay auto-approved, got %q", got)
	}
}
