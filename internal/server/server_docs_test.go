package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/knowledge"
	"github.com/xpier/xissue/internal/store"
)

func TestDocsAPIAndMCP(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xissue-docs-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := &config.Config{
		DataDir: tmpDir,
		Listen:  "127.0.0.1:0",
	}

	srv := New(cfg, st)
	defer srv.Shutdown()

	ctx := context.Background()
	proj, err := st.CreateProject(ctx, "DocProj", "Project for docs test")
	if err != nil {
		t.Fatal(err)
	}

	repoDir := filepath.Join(tmpDir, "localrepo")
	_ = os.MkdirAll(filepath.Join(repoDir, "docs"), 0o755)
	_ = os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# DocProj Readme\n\nWelcome to DocProj knowledge base.\n"), 0o644)
	_ = os.WriteFile(filepath.Join(repoDir, "docs", "api.md"), []byte("# API Reference\n\nAll endpoints for REST and MCP.\n"), 0o644)

	repo, err := st.CreateRepo(ctx, domain.Repo{
		ProjectID:     proj.ID,
		Name:          "localrepo",
		Provider:      domain.ProviderGitHub,
		RemoteURL:     repoDir,
		OwnerRepo:     "org/localrepo",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = repo

	// 1. Test GET /api/projects/{id}/docs
	req := httptest.NewRequest("GET", "/api/projects/1/docs", nil)
	req.Host = "127.0.0.1:7789"
	req.Header.Set("X-XIssue", "1")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/projects/1/docs returned %d: %s", w.Code, w.Body.String())
	}
	var docs []domain.DocItem
	if err := json.Unmarshal(w.Body.Bytes(), &docs); err != nil {
		t.Fatalf("unmarshal docs: %v", err)
	}
	if len(docs) < 2 {
		t.Fatalf("expected at least 2 docs, got %d", len(docs))
	}

	// 1b. Test GET /api/projects/{id}/readmes
	readmeReq := httptest.NewRequest("GET", "/api/projects/1/readmes", nil)
	readmeReq.Host = "127.0.0.1:7789"
	readmeReq.Header.Set("X-XIssue", "1")
	readmeW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(readmeW, readmeReq)
	if readmeW.Code != http.StatusOK {
		t.Fatalf("GET readmes returned %d: %s", readmeW.Code, readmeW.Body.String())
	}
	var readmes []domain.ProjectReadme
	if err := json.Unmarshal(readmeW.Body.Bytes(), &readmes); err != nil {
		t.Fatalf("unmarshal readmes: %v", err)
	}
	if len(readmes) != 1 || !readmes[0].Found || !strings.Contains(readmes[0].Content, "DocProj Readme") {
		t.Fatalf("readmes = %+v", readmes)
	}

	// 2. Test GET /api/projects/{id}/docs/detail
	detailReq := httptest.NewRequest("GET", "/api/projects/1/docs/detail?docId="+docs[0].ID, nil)
	detailReq.Host = "127.0.0.1:7789"
	detailReq.Header.Set("X-XIssue", "1")
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, detailReq)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET detail returned %d: %s", w2.Code, w2.Body.String())
	}
	var detail domain.DocDetail
	if err := json.Unmarshal(w2.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail.Content == "" {
		t.Errorf("detail content is empty")
	}

	// 3. Test POST /api/projects/{id}/docs/search
	searchBody := `{"query":"endpoints","limit":5}`
	searchReq := httptest.NewRequest("POST", "/api/projects/1/docs/search", strings.NewReader(searchBody))
	searchReq.Host = "127.0.0.1:7789"
	searchReq.Header.Set("Content-Type", "application/json")
	searchReq.Header.Set("X-XIssue", "1")
	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, searchReq)
	if w3.Code != http.StatusOK {
		t.Fatalf("POST search returned %d: %s", w3.Code, w3.Body.String())
	}
	var hits []domain.DocSearchResult
	if err := json.Unmarshal(w3.Body.Bytes(), &hits); err != nil {
		t.Fatalf("unmarshal search: %v", err)
	}
	if len(hits) == 0 {
		t.Errorf("expected search hits for 'endpoints', got 0")
	}

	// 4. Test MCP tool xissue_list_docs
	mcpResp := srv.ProcessMCPRequest(ctx, mcpRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"xissue_list_docs","arguments":{"projectId":1}}`),
	})
	if mcpResp == nil || mcpResp.Error != nil {
		t.Fatalf("mcp xissue_list_docs failed: %+v", mcpResp)
	}

	// 5. Test MCP tool xissue_search_docs
	mcpSearchResp := srv.ProcessMCPRequest(ctx, mcpRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"xissue_search_docs","arguments":{"projectId":1,"query":"endpoints"}}`),
	})
	if mcpSearchResp == nil || mcpSearchResp.Error != nil {
		t.Fatalf("mcp xissue_search_docs failed: %+v", mcpSearchResp)
	}
	_ = knowledge.WikiDir(tmpDir, 1)
}
