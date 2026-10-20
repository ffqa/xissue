// Package knowledge manages project knowledge bases: GitHub Wiki sync, local docs aggregation, and search.
package knowledge

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
)

var (
	syncMu      sync.Mutex
	ignoredDirs = map[string]bool{
		".git":         true,
		"node_modules": true,
		"dist":         true,
		"bin":          true,
		"vendor":       true,
		".next":        true,
		".cache":       true,
		"build":        true,
		".qoder":       true,
	}
)

// WikiDir returns the directory path for a repo's cloned wiki.
func WikiDir(dataDir string, repoID int64) string {
	return filepath.Join(dataDir, "wikis", fmt.Sprintf("repo-%d", repoID))
}

// SyncGitHubWiki clones or pulls the GitHub Wiki repository for a repo.
func SyncGitHubWiki(ctx context.Context, dataDir string, repo domain.Repo) error {
	syncMu.Lock()
	defer syncMu.Unlock()

	ownerRepo := strings.TrimSpace(repo.OwnerRepo)
	if ownerRepo == "" {
		return fmt.Errorf("仓库 %s 未配置 ownerRepo，无法确定 GitHub Wiki 地址", repo.Name)
	}

	wikiDir := WikiDir(dataDir, repo.ID)
	wikiURL := fmt.Sprintf("https://github.com/%s.wiki.git", ownerRepo)

	if err := os.MkdirAll(filepath.Dir(wikiDir), 0o755); err != nil {
		return fmt.Errorf("创建 wikis 目录失败: %w", err)
	}

	if _, err := os.Stat(filepath.Join(wikiDir, ".git")); err == nil {
		// Pull latest changes
		if _, err := gitx.Run(ctx, wikiDir, "fetch", "--depth=1", "origin"); err != nil {
			return fmt.Errorf("拉取 Wiki 失败: %w", err)
		}
		// Reset to origin/master or origin/main
		if _, err := gitx.Run(ctx, wikiDir, "reset", "--hard", "origin/HEAD"); err != nil {
			// Fallback: try reset to origin/master
			if _, err2 := gitx.Run(ctx, wikiDir, "reset", "--hard", "origin/master"); err2 != nil {
				_, _ = gitx.Run(ctx, wikiDir, "reset", "--hard", "origin/main")
			}
		}
		return nil
	}

	// First clone
	_ = os.RemoveAll(wikiDir)
	if _, err := gitx.Run(ctx, "", "clone", "--depth=1", wikiURL, wikiDir); err != nil {
		_ = os.RemoveAll(wikiDir)
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
			return fmt.Errorf("GitHub 仓库 %s 尚未创建或开启 Wiki 页面", ownerRepo)
		}
		return fmt.Errorf("克隆 GitHub Wiki 失败: %w", err)
	}

	return nil
}

// ScanProjectDocs scans all docs for a project from GitHub Wikis and local repository docs.
func ScanProjectDocs(ctx context.Context, dataDir string, project *domain.Project) ([]domain.DocItem, error) {
	if project == nil {
		return []domain.DocItem{}, nil
	}

	var items []domain.DocItem

	for _, repo := range project.Repos {
		// 1. Check GitHub Wiki
		wikiDir := WikiDir(dataDir, repo.ID)
		if st, err := os.Stat(wikiDir); err == nil && st.IsDir() {
			wikiDocs := scanDirForMarkdown(wikiDir, repo.ID, repo.Name, project.ID, domain.DocSourceGitHubWiki)
			items = append(items, wikiDocs...)
		}

		// 2. Check Local Repo Docs (via repo RemoteURL, project description, or mirror)
		localDir := getRepoLocalDir(repo, project.Description)
		if localDir != "" {
			localDocs := scanLocalRepoDocs(localDir, repo.ID, repo.Name, project.ID)
			items = append(items, localDocs...)
		} else {
			// Fallback: check mirror
			mirrorDir := filepath.Join(dataDir, "mirrors", fmt.Sprintf("repo-%d.git", repo.ID))
			if st, err := os.Stat(mirrorDir); err == nil && st.IsDir() {
				mirrorDocs := scanMirrorDocs(ctx, mirrorDir, repo.DefaultBranch, repo.ID, repo.Name, project.ID)
				items = append(items, mirrorDocs...)
			}
		}
	}

	// Sort: Home/README first, then by title
	sort.Slice(items, func(i, j int) bool {
		iPri := docPriority(items[i])
		jPri := docPriority(items[j])
		if iPri != jPri {
			return iPri < jPri
		}
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})

	return items, nil
}

func docPriority(d domain.DocItem) int {
	low := strings.ToLower(d.Path)
	if low == "home.md" || strings.HasPrefix(low, "readme") {
		return 1
	}
	if d.Source == domain.DocSourceGitHubWiki {
		return 2
	}
	if strings.HasPrefix(low, "docs/") || strings.HasPrefix(low, "doc/") {
		return 3
	}
	return 4
}

func scanDirForMarkdown(dir string, repoID int64, repoName string, projectID int64, source domain.DocSource) []domain.DocItem {
	var list []domain.DocItem

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if ignoredDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		info, err := d.Info()
		if err != nil {
			return nil
		}

		title, excerpt := extractTitleAndExcerpt(path)
		docID := fmt.Sprintf("wiki:%d:%s", repoID, rel)

		list = append(list, domain.DocItem{
			ID:        docID,
			ProjectID: projectID,
			RepoID:    repoID,
			RepoName:  repoName,
			Source:    source,
			Title:     title,
			Path:      rel,
			Size:      info.Size(),
			Excerpt:   excerpt,
			UpdatedAt: info.ModTime(),
		})
		return nil
	})

	return list
}

func scanLocalRepoDocs(repoDir string, repoID int64, repoName string, projectID int64) []domain.DocItem {
	var list []domain.DocItem

	_ = filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if ignoredDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		rel, rerr := filepath.Rel(repoDir, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		lowRel := strings.ToLower(rel)

		// Filter relevant docs:
		// 1. Root level markdown files: README.md, ARCHITECTURE.md, etc.
		// 2. Any markdown files under docs/, doc/, spec/, specs/, wiki/
		isRelevant := !strings.Contains(rel, "/") ||
			strings.HasPrefix(lowRel, "docs/") ||
			strings.HasPrefix(lowRel, "doc/") ||
			strings.HasPrefix(lowRel, "wiki/") ||
			strings.HasPrefix(lowRel, "specs/") ||
			strings.HasPrefix(lowRel, "spec/") ||
			strings.HasPrefix(lowRel, "rfc/")

		if !isRelevant {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		title, excerpt := extractTitleAndExcerpt(path)
		docID := fmt.Sprintf("local:%d:%s", repoID, rel)

		list = append(list, domain.DocItem{
			ID:        docID,
			ProjectID: projectID,
			RepoID:    repoID,
			RepoName:  repoName,
			Source:    domain.DocSourceLocalDoc,
			Title:     title,
			Path:      rel,
			Size:      info.Size(),
			Excerpt:   excerpt,
			UpdatedAt: info.ModTime(),
		})
		return nil
	})

	return list
}

func extractTitleAndExcerpt(filePath string) (string, string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		base := filepath.Base(filePath)
		return strings.TrimSuffix(base, filepath.Ext(base)), ""
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	title := ""
	var bodyLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if title == "" && strings.HasPrefix(trimmed, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			bodyLines = append(bodyLines, trimmed)
			if len(bodyLines) >= 3 {
				break
			}
		}
	}

	if title == "" {
		base := filepath.Base(filePath)
		title = strings.TrimSuffix(base, filepath.Ext(base))
	}

	excerpt := strings.Join(bodyLines, " ")
	runes := []rune(excerpt)
	if len(runes) > 160 {
		excerpt = string(runes[:160]) + "…"
	}

	return title, excerpt
}

func getRepoLocalDir(repo domain.Repo, projectDescription string) string {
	if gitx.IsLocalDir(repo.RemoteURL) {
		return repo.RemoteURL
	}
	if gitx.IsLocalDir(projectDescription) {
		return projectDescription
	}
	return ""
}

func scanMirrorDocs(ctx context.Context, mirrorDir, branch string, repoID int64, repoName string, projectID int64) []domain.DocItem {
	var list []domain.DocItem
	if branch == "" {
		branch = "HEAD"
	}
	out, err := gitx.Run(ctx, mirrorDir, "ls-tree", "-r", "--name-only", branch)
	if err != nil {
		return list
	}

	lines := strings.Split(out, "\n")
	for _, rel := range lines {
		rel = strings.TrimSpace(rel)
		if rel == "" || !strings.HasSuffix(strings.ToLower(rel), ".md") {
			continue
		}
		lowRel := strings.ToLower(rel)
		isRelevant := !strings.Contains(rel, "/") ||
			strings.HasPrefix(lowRel, "docs/") ||
			strings.HasPrefix(lowRel, "doc/") ||
			strings.HasPrefix(lowRel, "wiki/") ||
			strings.HasPrefix(lowRel, "specs/") ||
			strings.HasPrefix(lowRel, "spec/") ||
			strings.HasPrefix(lowRel, "rfc/")

		if !isRelevant {
			continue
		}

		// Read excerpt via git show
		content, _ := gitx.Run(ctx, mirrorDir, "show", branch+":"+rel)
		title := ""
		for _, l := range strings.Split(content, "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "# ") {
				title = strings.TrimSpace(strings.TrimPrefix(l, "# "))
				break
			}
		}
		if title == "" {
			base := filepath.Base(rel)
			title = strings.TrimSuffix(base, filepath.Ext(base))
		}

		excerpt := ""
		runes := []rune(content)
		if len(runes) > 160 {
			excerpt = string(runes[:160]) + "…"
		} else {
			excerpt = string(runes)
		}

		docID := fmt.Sprintf("mirror:%d:%s", repoID, rel)
		list = append(list, domain.DocItem{
			ID:        docID,
			ProjectID: projectID,
			RepoID:    repoID,
			RepoName:  repoName,
			Source:    domain.DocSourceLocalDoc,
			Title:     title,
			Path:      rel,
			Size:      int64(len(content)),
			Excerpt:   excerpt,
			UpdatedAt: time.Now(),
		})
	}
	return list
}

// ResolveDocFilePath resolves a docID to its absolute filesystem path.
func ResolveDocFilePath(dataDir string, project *domain.Project, docID string) (string, *domain.Repo, error) {
	parts := strings.SplitN(docID, ":", 3)
	if len(parts) < 3 {
		return "", nil, fmt.Errorf("无效的文档 ID: %s", docID)
	}

	sourceKind := parts[0]
	repoIDStr := parts[1]
	relPath := parts[2]

	var repo *domain.Repo
	for i := range project.Repos {
		if fmt.Sprintf("%d", project.Repos[i].ID) == repoIDStr {
			repo = &project.Repos[i]
			break
		}
	}

	if repo == nil {
		return "", nil, fmt.Errorf("未找到关联仓库 ID: %s", repoIDStr)
	}

	if sourceKind == "wiki" {
		wikiDir := WikiDir(dataDir, repo.ID)
		fullPath := filepath.Join(wikiDir, relPath)
		return fullPath, repo, nil
	}

	if sourceKind == "local" {
		localDir := getRepoLocalDir(*repo, project.Description)
		if localDir == "" {
			return "", nil, fmt.Errorf("仓库 %s 没有可访问的本地目录", repo.Name)
		}
		fullPath := filepath.Join(localDir, relPath)
		return fullPath, repo, nil
	}

	if sourceKind == "mirror" {
		mirrorDir := filepath.Join(dataDir, "mirrors", fmt.Sprintf("repo-%d.git", repo.ID))
		return mirrorDir, repo, nil
	}

	return "", nil, fmt.Errorf("未知的文档来源类型: %s", sourceKind)
}

// GetDocDetail retrieves full content and metadata for a single document.
func GetDocDetail(ctx context.Context, dataDir string, project *domain.Project, docID string) (*domain.DocDetail, error) {
	parts := strings.SplitN(docID, ":", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("无效的文档 ID: %s", docID)
	}

	sourceKind := parts[0]
	relPath := parts[2]

	pathOrMirror, repo, err := ResolveDocFilePath(dataDir, project, docID)
	if err != nil {
		return nil, err
	}

	var data []byte
	var modTime time.Time
	var size int64
	var title, excerpt string

	if sourceKind == "mirror" {
		branch := repo.DefaultBranch
		if branch == "" {
			branch = "HEAD"
		}
		content, gerr := gitx.Run(ctx, pathOrMirror, "show", branch+":"+relPath)
		if gerr != nil {
			return nil, fmt.Errorf("从镜像读取文档失败: %w", gerr)
		}
		data = []byte(content)
		modTime = time.Now()
		size = int64(len(data))
		for _, l := range strings.Split(content, "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "# ") {
				title = strings.TrimSpace(strings.TrimPrefix(l, "# "))
				break
			}
		}
		if title == "" {
			base := filepath.Base(relPath)
			title = strings.TrimSuffix(base, filepath.Ext(base))
		}
		runes := []rune(content)
		if len(runes) > 160 {
			excerpt = string(runes[:160]) + "…"
		} else {
			excerpt = string(runes)
		}
	} else {
		d, err := os.ReadFile(pathOrMirror)
		if err != nil {
			return nil, fmt.Errorf("读取文档失败: %w", err)
		}
		data = d
		info, _ := os.Stat(pathOrMirror)
		title, excerpt = extractTitleAndExcerpt(pathOrMirror)
		modTime = time.Now()
		size = int64(len(data))
		if info != nil {
			modTime = info.ModTime()
			size = info.Size()
		}
	}

	source := domain.DocSourceLocalDoc
	if sourceKind == "wiki" {
		source = domain.DocSourceGitHubWiki
	}

	return &domain.DocDetail{
		DocItem: domain.DocItem{
			ID:        docID,
			ProjectID: project.ID,
			RepoID:    repo.ID,
			RepoName:  repo.Name,
			Source:    source,
			Title:     title,
			Path:      relPath,
			Size:      size,
			Excerpt:   excerpt,
			UpdatedAt: modTime,
		},
		Content: string(data),
	}, nil
}

// SaveDoc updates or creates a markdown document.
func SaveDoc(ctx context.Context, dataDir string, project *domain.Project, docID, content string) error {
	fullPath, _, err := ResolveDocFilePath(dataDir, project, docID)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return err
	}

	return os.WriteFile(fullPath, []byte(content), 0o644)
}

// SearchDocs performs full-text search across all docs in the project.
func SearchDocs(ctx context.Context, dataDir string, project *domain.Project, query string, limit int) ([]domain.DocSearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []domain.DocSearchResult{}, nil
	}

	if limit <= 0 {
		limit = 20
	}

	docs, err := ScanProjectDocs(ctx, dataDir, project)
	if err != nil {
		return nil, err
	}

	terms := strings.Fields(strings.ToLower(q))
	var results []domain.DocSearchResult

	for _, doc := range docs {
		detail, derr := GetDocDetail(ctx, dataDir, project, doc.ID)
		if derr != nil {
			continue
		}

		content := detail.Content
		lowContent := strings.ToLower(content)
		lowTitle := strings.ToLower(doc.Title)
		lowPath := strings.ToLower(doc.Path)

		score := 0
		for _, t := range terms {
			if strings.Contains(lowTitle, t) {
				score += 25
			}
			if strings.Contains(lowPath, t) {
				score += 15
			}
			cnt := strings.Count(lowContent, t)
			if cnt > 0 {
				score += 5 + (cnt * 2)
			}
		}

		if score > 0 {
			snippet := extractSnippet(content, terms)
			results = append(results, domain.DocSearchResult{
				DocItem: doc,
				Snippet: snippet,
				Score:   score,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func extractSnippet(content string, terms []string) string {
	low := strings.ToLower(content)
	bestIdx := -1
	for _, t := range terms {
		if idx := strings.Index(low, t); idx >= 0 {
			if bestIdx == -1 || idx < bestIdx {
				bestIdx = idx
			}
		}
	}

	if bestIdx == -1 {
		runes := []rune(content)
		if len(runes) > 150 {
			return string(runes[:150]) + "…"
		}
		return content
	}

	start := bestIdx - 60
	if start < 0 {
		start = 0
	}
	end := bestIdx + 120
	if end > len(content) {
		end = len(content)
	}

	snippet := strings.TrimSpace(content[start:end])
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(content) {
		snippet = snippet + "…"
	}
	return snippet
}
