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
