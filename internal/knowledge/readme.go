package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
)

const readmeMaxBytes = 512 * 1024

var readmeCandidates = map[string]bool{
	"readme.md":       true,
	"readme.markdown": true,
	"readme.mdown":    true,
	"readme.mkdn":     true,
}

// ProjectReadmes returns the root README of every repository in a project.
// Missing READMEs are returned with Found=false so the UI can show every repo.
func ProjectReadmes(ctx context.Context, dataDir string, project *domain.Project) ([]domain.ProjectReadme, error) {
	if project == nil {
		return []domain.ProjectReadme{}, nil
	}
	out := make([]domain.ProjectReadme, 0, len(project.Repos))
	for _, repo := range project.Repos {
		item := domain.ProjectReadme{RepoID: repo.ID, RepoName: repo.Name, Branch: repo.DefaultBranch}
		if gitx.IsLocalDir(repo.RemoteURL) {
			readLocalREADME(repo.RemoteURL, &item)
		} else {
			mirror := filepath.Join(dataDir, "mirrors", fmt.Sprintf("repo-%d.git", repo.ID))
			if st, err := os.Stat(mirror); err != nil || !st.IsDir() {
				// Read-only UI path: clone once if the mirror does not exist yet.
				// Do not block page loads forever on an unreachable remote.
				cloneCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				_ = gitx.EnsureMirrorCached(cloneCtx, repo.RemoteURL, mirror)
				cancel()
			}
			if st, err := os.Stat(mirror); err == nil && st.IsDir() {
				readMirrorREADME(ctx, mirror, &item)
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func readLocalREADME(repoDir string, item *domain.ProjectReadme) {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return
	}
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() || !readmeCandidates[strings.ToLower(entry.Name())] {
			continue
		}
		matches = append(matches, entry.Name())
	}
	if len(matches) == 0 {
		return
	}
	sort.Slice(matches, func(i, j int) bool {
		return readmeRank(matches[i]) < readmeRank(matches[j])
	})
	name := matches[0]
	path := filepath.Join(repoDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	item.Path = name
	item.Size = int64(len(data))
	item.Truncated = len(data) > readmeMaxBytes
	item.Content = truncateUTF8(data, readmeMaxBytes)
	item.Found = true
	title, _ := extractTitleAndExcerpt(path)
	item.Title = title
	if item.Title == "" {
		item.Title = item.RepoName
	}
}

func readMirrorREADME(ctx context.Context, mirror string, item *domain.ProjectReadme) {
	branch := strings.TrimSpace(item.Branch)
	if branch == "" {
		branch = "HEAD"
	}
	if err := gitx.ValidateBranchName(branch); err != nil {
		return
	}
	out, err := gitx.Run(ctx, mirror, "ls-tree", "--name-only", branch)
	if err != nil {
		return
	}
	var matches []string
	for _, name := range strings.Split(out, "\n") {
		name = strings.TrimSpace(name)
		if name == "" || strings.Contains(name, "/") {
			continue
		}
		if readmeCandidates[strings.ToLower(name)] {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return
	}
	sort.Slice(matches, func(i, j int) bool {
		return readmeRank(matches[i]) < readmeRank(matches[j])
	})
	name := matches[0]
	content, err := gitx.Run(ctx, mirror, "show", branch+":"+name)
	if err != nil {
		return
	}
	item.Path = name
	item.Size = int64(len(content))
	item.Truncated = len(content) > readmeMaxBytes
	item.Content = truncateUTF8([]byte(content), readmeMaxBytes)
	item.Found = true
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			item.Title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			break
		}
	}
	if item.Title == "" {
		item.Title = strings.TrimSuffix(name, filepath.Ext(name))
	}
}

func readmeRank(name string) int {
	switch strings.ToLower(name) {
	case "readme.md":
		return 0
	case "readme.markdown":
		return 1
	case "readme.mdown":
		return 2
	default:
		return 3
	}
}

func truncateUTF8(data []byte, limit int) string {
	if len(data) <= limit {
		return string(data)
	}
	data = data[:limit]
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	return string(data)
}
