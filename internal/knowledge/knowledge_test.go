package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xpier/xissue/internal/domain"
)

func TestKnowledgeScanAndSearch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xissue-knowledge-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dataDir := filepath.Join(tmpDir, "data")
	repoDir := filepath.Join(tmpDir, "myrepo")
	_ = os.MkdirAll(filepath.Join(repoDir, "docs"), 0o755)

	// Create a README
	readmeContent := "# My Cool Project\n\nThis project provides high performance knowledge base for AI agents.\n"
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte(readmeContent), 0o644)

	// Create a doc
	docContent := "# Architecture & Design\n\nDetailed breakdown of modules and MCP interfaces.\n"
	_ = os.WriteFile(filepath.Join(repoDir, "docs", "design.md"), []byte(docContent), 0o644)

	// Create a fake wiki
	wikiDir := WikiDir(dataDir, 100)
	_ = os.MkdirAll(filepath.Join(wikiDir, "guides", "advanced"), 0o755)
	wikiContent := "# Home Page\n\nWelcome to GitHub Wiki documentation.\n"
	_ = os.WriteFile(filepath.Join(wikiDir, "Home.md"), []byte(wikiContent), 0o644)
	subWikiContent := "# Advanced Guide\n\nDeep nested wiki document.\n"
	_ = os.WriteFile(filepath.Join(wikiDir, "guides", "advanced", "setup.md"), []byte(subWikiContent), 0o644)

	proj := &domain.Project{
		ID:   1,
		Name: "Test Project",
		Repos: []domain.Repo{
			{
				ID:        100,
				ProjectID: 1,
				Name:      "myrepo",
				RemoteURL: repoDir,
				OwnerRepo: "test/myrepo",
			},
		},
	}

	ctx := context.Background()

	// 1. Scan docs
	docs, err := ScanProjectDocs(ctx, dataDir, proj)
	if err != nil {
		t.Fatalf("ScanProjectDocs failed: %v", err)
	}
	if len(docs) < 4 {
		t.Fatalf("expected at least 4 docs (including nested wiki), got %d", len(docs))
	}

	// Verify nested wiki doc exists
	foundNested := false
	var nestedDocID string
	for _, d := range docs {
		if d.Path == "guides/advanced/setup.md" {
			foundNested = true
			nestedDocID = d.ID
			break
		}
	}
	if !foundNested {
		t.Fatalf("expected nested wiki doc 'guides/advanced/setup.md' to be scanned")
	}

	nestedDetail, err := GetDocDetail(ctx, dataDir, proj, nestedDocID)
	if err != nil {
		t.Fatalf("GetDocDetail for nested wiki failed: %v", err)
	}
	if nestedDetail.Title != "Advanced Guide" {
		t.Errorf("expected nested wiki title 'Advanced Guide', got %s", nestedDetail.Title)
	}

	// 2. Search docs
	results, err := SearchDocs(ctx, dataDir, proj, "architecture", 10)
	if err != nil {
		t.Fatalf("SearchDocs failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected search hits for 'architecture', got 0")
	}
	if results[0].Title != "Architecture & Design" {
		t.Errorf("expected first hit title 'Architecture & Design', got %s", results[0].Title)
	}

	// 3. Get detail
	detail, err := GetDocDetail(ctx, dataDir, proj, docs[0].ID)
	if err != nil {
		t.Fatalf("GetDocDetail failed: %v", err)
	}
	if detail.Content == "" {
		t.Errorf("expected non-empty doc content")
	}

	// 4. Save doc
	newContent := detail.Content + "\n\n## Updates\nAdded some extra info.\n"
	if err := SaveDoc(ctx, dataDir, proj, docs[0].ID, newContent); err != nil {
		t.Fatalf("SaveDoc failed: %v", err)
	}
	detail2, _ := GetDocDetail(ctx, dataDir, proj, docs[0].ID)
	if detail2.Content != newContent {
		t.Errorf("saved content mismatch")
	}
}

func TestDocPathTraversalRejected(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(WikiDir(dataDir, 7), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(outside, []byte("do-not-touch"), 0o644); err != nil {
		t.Fatal(err)
	}

	proj := &domain.Project{
		ID:   1,
		Name: "traversal",
		Repos: []domain.Repo{{
			ID:        7,
			ProjectID: 1,
			Name:      "repo",
			RemoteURL: repoDir,
		}},
	}
	ctx := context.Background()

	badIDs := []string{
		"local:7:../secret.txt",
		"local:7:../../etc/passwd",
		"local:7:a/../../secret.txt",
		"local:7:.",
		"local:7:..",
		"local:7:",
		"wiki:7:../secret.txt",
		"local:7:/etc/passwd",
		"wiki:7:" + outside,
		"local:7:bad\x00name",
	}
	for _, id := range badIDs {
		if _, err := GetDocDetail(ctx, dataDir, proj, id); err == nil {
			t.Errorf("GetDocDetail(%q) should be rejected", id)
		}
		if err := SaveDoc(ctx, dataDir, proj, id, "pwned"); err == nil {
			t.Errorf("SaveDoc(%q) should be rejected", id)
		}
	}

	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("outside file disappeared: %v", err)
	}
	if string(got) != "do-not-touch" {
		t.Fatalf("outside file was modified: %q", got)
	}
}

func TestDocPathSymlinkEscapeRejected(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tmpDir, "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repoDir, "leak.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	proj := &domain.Project{
		ID:   1,
		Name: "symlink",
		Repos: []domain.Repo{{
			ID:        7,
			ProjectID: 1,
			Name:      "repo",
			RemoteURL: repoDir,
		}},
	}
	ctx := context.Background()
	if _, err := GetDocDetail(ctx, dataDir, proj, "local:7:leak.md"); err == nil {
		t.Fatal("expected symlink escape read to be rejected")
	}
	if err := SaveDoc(ctx, dataDir, proj, "local:7:leak.md", "pwned"); err == nil {
		t.Fatal("expected symlink escape write to be rejected")
	}
	got, _ := os.ReadFile(outside)
	if string(got) != "outside" {
		t.Fatalf("outside file was modified: %q", got)
	}
}

func TestSaveDocAllowsNestedPath(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	proj := &domain.Project{
		ID:   1,
		Name: "nested",
		Repos: []domain.Repo{{
			ID:        7,
			ProjectID: 1,
			Name:      "repo",
			RemoteURL: repoDir,
		}},
	}
	ctx := context.Background()
	if err := SaveDoc(ctx, dataDir, proj, "local:7:guides/deep/new.md", "# New\n"); err != nil {
		t.Fatalf("SaveDoc nested path failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "guides", "deep", "new.md")); err != nil {
		t.Fatalf("nested file not written: %v", err)
	}
	detail, err := GetDocDetail(ctx, dataDir, proj, "local:7:guides/deep/new.md")
	if err != nil {
		t.Fatalf("GetDocDetail nested path failed: %v", err)
	}
	if detail.Content != "# New\n" {
		t.Fatalf("unexpected content: %q", detail.Content)
	}
}

func TestCategorizeDoc(t *testing.T) {
	cases := map[string]string{
		"docs/requirements/login.md":    "requirements",
		"docs/prd/checkout.md":          "requirements",
		"docs/adr/0001-storage.md":      "adr",
		"docs/decisions/queue.md":       "adr",
		"reports/weekly-2026-10.md":     "reports",
		"docs/reviews/pr-42.md":         "reports",
		"docs/tasks/todo.md":            "work",
		"docs/plan/2026-roadmap.md":     "work",
		"docs/architecture/overview.md": "other",
		"README.md":                     "other",
		"guides/advanced/setup.md":      "other",
		"notes/my-report.md":            "reports",
	}
	for path, want := range cases {
		if got := categorizeDoc(path); got != want {
			t.Errorf("categorizeDoc(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestScanLocalRepoDocsIncludesScatteredMarkdown(t *testing.T) {
	repoDir := t.TempDir()
	files := map[string]string{
		"README.md":                  "# Root\n",
		"notes/meeting.md":           "# Meeting\n",
		"guides/advanced/setup.md":   "# Setup\n",
		"docs/adr/0001-storage.md":   "# ADR\n",
		"node_modules/pkg/readme.md": "# Ignored\n",
		".git/hooks/readme.md":       "# Ignored\n",
	}
	for rel, content := range files {
		full := filepath.Join(repoDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	docs := scanLocalRepoDocs(repoDir, 1, "repo", 1)
	found := make(map[string]bool, len(docs))
	for _, doc := range docs {
		found[doc.Path] = true
	}
	for _, want := range []string{"README.md", "notes/meeting.md", "guides/advanced/setup.md", "docs/adr/0001-storage.md"} {
		if !found[want] {
			t.Errorf("expected scattered doc %q to be scanned, got %v", want, found)
		}
	}
	for _, unwanted := range []string{"node_modules/pkg/readme.md", ".git/hooks/readme.md"} {
		if found[unwanted] {
			t.Errorf("ignored path %q should not be scanned", unwanted)
		}
	}
}
