package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/store"
)

// A comment by an untrusted author must never reach provider MergePR, even if
// a rule's pattern matches the comment body.
func TestMergePRActionRequiresTrustedAuthor(t *testing.T) {
	ctx := context.Background()
	mergeCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/merge") {
			mergeCalled = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	t.Setenv("XISSUE_GITEA_URL", srv.URL)
	t.Setenv("XISSUE_GITEA_TOKEN", "test-token")

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	r := New(cfg, st, &dummyNotifier{})
	t.Cleanup(func() {
		r.Shutdown()
		_ = st.Close()
	})

	proj, err := st.CreateProject(ctx, "Gate", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "demo", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := &domain.Repo{ID: 7, ProjectID: proj.ID, Name: "demo", Provider: domain.ProviderGitea, OwnerRepo: "acme/demo"}
	prDetail := &provider.PRDetailInfo{Number: 9, URL: srv.URL + "/acme/demo/pulls/9", Author: "alice", State: "open"}
	actions := []domain.RuleAction{{Type: "merge_pr"}}

	// Untrusted commenter: the pattern matched a rule that names nobody.
	trig := prEventTrigger{Event: "pr_comment", Label: "评论", Author: "mallory",
		Rules: []domain.AutomationRule{{Conditions: domain.RuleConditions{Pattern: "LGTM"}}}}
	merged, err := r.executeActions(ctx, actions, proj, repo, issue, domain.Run{}, prDetail, trig)
	if err != nil {
		t.Fatalf("executeActions: %v", err)
	}
	if merged || mergeCalled {
		t.Fatalf("untrusted author merged the PR: merged=%v mergeCalled=%v", merged, mergeCalled)
	}

	// PR author (trusted) merges as before.
	trig.Author = "alice"
	merged, err = r.executeActions(ctx, actions, proj, repo, issue, domain.Run{}, prDetail, trig)
	if err != nil {
		t.Fatalf("executeActions: %v", err)
	}
	if !merged || !mergeCalled {
		t.Fatalf("trusted author did not merge: merged=%v mergeCalled=%v", merged, mergeCalled)
	}
}

// A queued or running DB record must block a second StartRun for the same
// issue, closing the check-then-create race that produced duplicate runs.
func TestStartRunRejectsIssueWithActiveRun(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Agents["fake"] = config.AgentConfig{Command: "/nonexistent/agent-binary", MemoryMB: 10}
	r := New(cfg, st, &dummyNotifier{})
	t.Cleanup(func() {
		r.Shutdown()
		_ = st.Close()
	})

	proj, err := st.CreateProject(ctx, "Dup", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "one", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRun(ctx, issue.ID, "fake", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartRun(ctx, issue.ID, "fake", "", ""); err == nil ||
		!strings.Contains(err.Error(), "active run") {
		t.Fatalf("second StartRun for same issue err = %v, want active-run rejection", err)
	}
}
