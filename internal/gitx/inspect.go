package gitx

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xpier/xissue/internal/domain"
)

// DirEntry represents one directory in the server's filesystem browser.
type DirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsGit bool   `json:"isGit"`
}

// BrowseResult is returned by BrowseDir.
type BrowseResult struct {
	Path    string     `json:"path"`
	Parent  string     `json:"parent"`
	IsGit   bool       `json:"isGit"`
	Entries []DirEntry `json:"entries"`
}

// InspectedRepo describes a Git repository found on disk.
type InspectedRepo struct {
	Path          string `json:"path"`
	Name          string `json:"name"`
	Provider      string `json:"provider"`      // github | gitlab | gitea | plain
	RemoteURL     string `json:"remoteUrl"`     // origin URL, or Path when no remote exists
	OwnerRepo     string `json:"ownerRepo"`     // owner/repo when parsed from remote
	DefaultBranch string `json:"defaultBranch"` // main / master / current branch
	TestCommand   string `json:"testCommand"`   // inferred from project files
}

// InspectResult describes a directory and all Git repositories discovered in it (itself or 1-level subdirectories).
type InspectResult struct {
	Path          string          `json:"path"`
	SuggestedName string          `json:"suggestedName"`
	Repos         []InspectedRepo `json:"repos"`
}

// ResolveDir expands ~ or empty path (defaults to user home) to a clean absolute path.
func ResolveDir(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return abs, nil
}

// IsGitRepo checks whether dir is a Git working tree or bare repository root.
func IsGitRepo(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		if _, err := os.Stat(filepath.Join(dir, "objects")); err == nil {
			return true
		}
	}
	return false
}

// BrowseDir lists immediate subdirectories of path and marks which ones are Git repos.
func BrowseDir(path string) (*BrowseResult, error) {
	dir, err := ResolveDir(path)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		parent = ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []DirEntry{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		st, err := os.Stat(full)
		if err != nil || !st.IsDir() {
			continue
		}
		out = append(out, DirEntry{
			Name:  name,
			Path:  full,
			IsGit: IsGitRepo(full),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsGit != out[j].IsGit {
			return out[i].IsGit
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return &BrowseResult{
		Path:    dir,
		Parent:  parent,
		IsGit:   IsGitRepo(dir),
		Entries: out,
	}, nil
}

// InspectDir inspects a directory: if it is a Git repo, returns that repo;
// otherwise scans its immediate subdirectories for Git repos (multi-repo project).
func InspectDir(ctx context.Context, path string) (*InspectResult, error) {
	dir, err := ResolveDir(path)
	if err != nil {
		return nil, err
	}
	res := &InspectResult{
		Path:          dir,
		SuggestedName: filepath.Base(dir),
		Repos:         []InspectedRepo{},
	}
	// Resolve git top-level in case user selected a subdirectory inside a repo.
	if top, err := Run(ctx, dir, "rev-parse", "--show-toplevel"); err == nil && top != "" {
		res.Path = top
		res.SuggestedName = filepath.Base(top)
		res.Repos = append(res.Repos, inspectSingleRepo(ctx, top))
		return res, nil
	}
	if IsGitRepo(dir) {
		res.Repos = append(res.Repos, inspectSingleRepo(ctx, dir))
		return res, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		if st, err := os.Stat(sub); err == nil && st.IsDir() && IsGitRepo(sub) {
			res.Repos = append(res.Repos, inspectSingleRepo(ctx, sub))
		}
	}
	return res, nil
}

func inspectSingleRepo(ctx context.Context, dir string) InspectedRepo {
	name := strings.TrimSuffix(filepath.Base(dir), ".git")
	remote := ""
	if u, err := Run(ctx, dir, "remote", "get-url", "origin"); err == nil && u != "" {
		remote = u
	} else if remList, err := Run(ctx, dir, "remote"); err == nil {
		first := splitLines(remList)
		if len(first) > 0 {
			if u, err := Run(ctx, dir, "remote", "get-url", first[0]); err == nil {
				remote = u
			}
		}
	}

	provider, ownerRepo := parseRemote(remote)
	// When the local repo has no remote configured, use its local path as the clone source.
	if remote == "" {
		remote = dir
		provider = "plain"
	}

	branch := detectDefaultBranch(ctx, dir)
	testCmd := detectTestCommand(dir)

	return InspectedRepo{
		Path:          dir,
		Name:          name,
		Provider:      provider,
		RemoteURL:     remote,
		OwnerRepo:     ownerRepo,
		DefaultBranch: branch,
		TestCommand:   testCmd,
	}
}

func detectDefaultBranch(ctx context.Context, dir string) string {
	if ref, err := Run(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if b := strings.TrimPrefix(ref, "origin/"); b != "" && b != "HEAD" {
			return b
		}
	}
	if cur, err := Run(ctx, dir, "symbolic-ref", "--short", "HEAD"); err == nil && cur != "" {
		return cur
	}
	for _, cand := range []string{"main", "master", "develop", "trunk"} {
		if _, err := Run(ctx, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+cand); err == nil {
			return cand
		}
		if _, err := Run(ctx, dir, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+cand); err == nil {
			return cand
		}
	}
	return "main"
}

func detectTestCommand(dir string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case exists("go.mod"):
		return "go test ./..."
	case exists("Cargo.toml"):
		return "cargo test"
	case exists("pnpm-lock.yaml") && exists("package.json"):
		return "pnpm test"
	case exists("yarn.lock") && exists("package.json"):
		return "yarn test"
	case exists("bun.lockb") || exists("bun.lock"):
		return "bun test"
	case exists("package.json"):
		return "npm test"
	case exists("pyproject.toml") || exists("pytest.ini"):
		return "pytest"
	case exists("composer.json"):
		return "composer test"
	default:
		return ""
	}
}

// parseRemote extracts (provider, owner/repo) from SSH or HTTPS remote URLs.
func parseRemote(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "plain", ""
	}
	var host, path string
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil {
			host = strings.ToLower(u.Hostname())
			path = strings.Trim(u.Path, "/")
		}
	} else if i := strings.Index(raw, ":"); i > 0 && !strings.Contains(raw[:i], "/") {
		// SCP-like syntax: git@github.com:owner/repo.git
		hostPart := raw[:i]
		if at := strings.LastIndex(hostPart, "@"); at >= 0 {
			hostPart = hostPart[at+1:]
		}
		host = strings.ToLower(hostPart)
		path = strings.Trim(raw[i+1:], "/")
	}
	path = strings.TrimSuffix(path, ".git")
	if host == "" || path == "" {
		return "plain", ""
	}
	switch {
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return "github", path
	case strings.Contains(host, "gitlab"):
		return "gitlab", path
	case strings.Contains(host, "gitea") || strings.Contains(host, "forgejo") || host == "codeberg.org":
		return "gitea", path
	default:
		return "plain", path
	}
}

// DetectRepoRemote inspects an existing Repo and checks if its remote or local directory
// has configured or changed remote URLs (e.g. after user pushes a plain local repo to GitHub).
// It returns true if any field on repo was updated.
func DetectRepoRemote(ctx context.Context, repo *domain.Repo) bool {
	changed := false
	remoteCandidate := repo.RemoteURL

	if isLocalDir(repo.RemoteURL) {
		// Check local repository's remotes (origin first)
		if u, err := Run(ctx, repo.RemoteURL, "remote", "get-url", "origin"); err == nil && strings.TrimSpace(u) != "" {
			remoteCandidate = strings.TrimSpace(u)
		} else if remList, err := Run(ctx, repo.RemoteURL, "remote"); err == nil {
			first := splitLines(remList)
			if len(first) > 0 {
				if u, err := Run(ctx, repo.RemoteURL, "remote", "get-url", first[0]); err == nil && strings.TrimSpace(u) != "" {
					remoteCandidate = strings.TrimSpace(u)
				}
			}
		}
	}

	if remoteCandidate != "" && remoteCandidate != repo.RemoteURL {
		prov, ownerRepo := parseRemote(remoteCandidate)
		if prov != "plain" || repo.Provider == domain.ProviderPlain {
			if repo.RemoteURL != remoteCandidate {
				repo.RemoteURL = remoteCandidate
				changed = true
			}
			if repo.Provider != domain.Provider(prov) {
				repo.Provider = domain.Provider(prov)
				changed = true
			}
			if repo.OwnerRepo != ownerRepo && ownerRepo != "" {
				repo.OwnerRepo = ownerRepo
				changed = true
			}
		}
	} else if repo.RemoteURL != "" {
		prov, ownerRepo := parseRemote(repo.RemoteURL)
		if prov != string(repo.Provider) && prov != "plain" {
			repo.Provider = domain.Provider(prov)
			changed = true
		}
		if ownerRepo != repo.OwnerRepo && ownerRepo != "" {
			repo.OwnerRepo = ownerRepo
			changed = true
		}
	}

	return changed
}

