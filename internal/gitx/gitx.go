// Package gitx wraps the git CLI: bare mirrors, worktrees, diffs, commits, pushes.
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var validSHARegex = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// ValidateBranchName ensures a branch name cannot be interpreted as a git CLI option or path traversal.
func ValidateBranchName(branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return fmt.Errorf("branch name is empty")
	}
	if strings.HasPrefix(branch, "-") || strings.Contains(branch, "..") || strings.ContainsAny(branch, " \t\r\n~^:?*[\\") {
		return fmt.Errorf("invalid branch name %q", branch)
	}
	return nil
}

// ValidateSHA ensures s is a valid hexadecimal commit SHA prefix.
func ValidateSHA(s string) error {
	if !validSHARegex.MatchString(strings.TrimSpace(s)) {
		return fmt.Errorf("invalid commit SHA %q", s)
	}
	return nil
}

// Run executes git with args in dir and returns trimmed stdout.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

var mirrorLocks sync.Map // path -> *sync.Mutex

func lockFor(path string) *sync.Mutex {
	m, _ := mirrorLocks.LoadOrStore(path, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// isLocalDir reports whether remote is an existing local directory.
func isLocalDir(remote string) bool {
	st, err := os.Stat(remote)
	return err == nil && st.IsDir()
}

// ensureLocalRepoHasCommit guarantees that an unborn local repository (just `git init` with no commits)
// has at least one empty commit so `git worktree add` can branch off it, without staging user working files.
func ensureLocalRepoHasCommit(ctx context.Context, dir, base string) error {
	if _, err := Run(ctx, dir, "rev-parse", "--verify", "HEAD"); err == nil {
		return nil
	}
	if base != "" && ValidateBranchName(base) == nil {
		_, _ = Run(ctx, dir, "symbolic-ref", "HEAD", "refs/heads/"+base)
	}
	args := []string{"commit", "--allow-empty", "--only", "-m", "Initial empty commit (xIssue baseline)"}
	if email, _ := Run(ctx, dir, "config", "user.email"); email == "" {
		args = append([]string{"-c", "user.name=xIssue", "-c", "user.email=xissue@localhost"}, args...)
	}
	_, err := Run(ctx, dir, args...)
	return err
}

// EnsureMirror makes sure a bare partial clone of remote exists at path and is up to date.
// All run worktrees are created from this mirror so they share one object store.
func EnsureMirror(ctx context.Context, remote, path string) error {
	remote = strings.TrimSpace(remote)
	if strings.HasPrefix(remote, "-") || strings.HasPrefix(remote, "ext::") {
		return fmt.Errorf("unsafe remote URL %q", remote)
	}
	l := lockFor(path)
	l.Lock()
	defer l.Unlock()
	if isLocalDir(remote) {
		if err := ensureLocalRepoHasCommit(ctx, remote, ""); err != nil {
			return fmt.Errorf("initialize local repo %s: %w", remote, err)
		}
	}
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		_ = os.RemoveAll(path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if _, err := Run(ctx, "", "clone", "--bare", "--filter=blob:none", "--", remote, path); err != nil {
			_ = os.RemoveAll(path)
			return err
		}
	}
	// Always ensure the fetch refspec is set so `fetch origin` populates refs/remotes/origin/*.
	if _, err := Run(ctx, path, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return err
	}
	_, err := Run(ctx, path, "fetch", "--prune", "origin")
	return err
}

// resolveBaseRef finds the best available base reference in a repository or mirror.
func resolveBaseRef(ctx context.Context, dir, base string) string {
	candidates := []string{
		"refs/remotes/origin/" + base,
		"refs/heads/" + base,
		"refs/remotes/origin/main",
		"refs/remotes/origin/master",
		"refs/heads/main",
		"refs/heads/master",
	}
	for _, ref := range candidates {
		if _, err := Run(ctx, dir, "rev-parse", "--verify", "--quiet", ref); err == nil {
			return ref
		}
	}
	// Fall back to the first available remote or local branch.
	if out, err := Run(ctx, dir, "for-each-ref", "--format=%(refname)", "--count=1", "refs/remotes/origin", "refs/heads"); err == nil && out != "" {
		return splitLines(out)[0]
	}
	return "HEAD"
}

// AddWorktree creates a new branch from the resolved base ref and checks it out at dir.
func AddWorktree(ctx context.Context, mirror, dir, branch, base string) error {
	if err := ValidateBranchName(branch); err != nil {
		return err
	}
	l := lockFor(mirror)
	l.Lock()
	defer l.Unlock()
	startPoint := resolveBaseRef(ctx, mirror, base)
	_, err := Run(ctx, mirror, "worktree", "add", "-B", branch, "--", dir, startPoint)
	return err
}

// RemoveWorktree removes the worktree and deletes its local branch.
func RemoveWorktree(ctx context.Context, mirror, dir, branch string) error {
	l := lockFor(mirror)
	l.Lock()
	defer l.Unlock()
	_, err := Run(ctx, mirror, "worktree", "remove", "--force", "--", dir)
	if err != nil && !strings.Contains(err.Error(), "is not a working tree") {
		return err
	}
	_, _ = Run(ctx, mirror, "worktree", "prune")
	if branch != "" && ValidateBranchName(branch) == nil {
		_, _ = Run(ctx, mirror, "branch", "-D", "--", branch)
	}
	return nil
}

// FileChange is one changed file in a worktree relative to its base.
type FileChange struct {
	Path      string `json:"path"`
	Status    string `json:"status"` // M A D R ?
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

func mergeBaseFor(ctx context.Context, dir, base string) string {
	ref := resolveBaseRef(ctx, dir, base)
	if mb, err := Run(ctx, dir, "merge-base", ref, "HEAD"); err == nil && mb != "" {
		return mb
	}
	return ref
}

// Changes lists committed + uncommitted + untracked changes vs base.
func Changes(ctx context.Context, dir, base string) ([]FileChange, error) {
	mergeBase := mergeBaseFor(ctx, dir, base)
	byPath := map[string]*FileChange{}
	order := []string{}
	add := func(fc FileChange) {
		if _, ok := byPath[fc.Path]; !ok {
			order = append(order, fc.Path)
		}
		byPath[fc.Path] = &fc
	}
	ns, err := Run(ctx, dir, "diff", "--name-status", "-M", mergeBase)
	if err != nil {
		return nil, err
	}
	for _, line := range splitLines(ns) {
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		st := parts[0][:1]
		add(FileChange{Path: parts[len(parts)-1], Status: st})
	}
	untracked, err := Run(ctx, dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	for _, p := range splitLines(untracked) {
		add(FileChange{Path: p, Status: "A"})
	}
	if num, err := Run(ctx, dir, "diff", "--numstat", "-M", mergeBase); err == nil {
		for _, line := range splitLines(num) {
			f := strings.Split(line, "\t")
			if len(f) < 3 {
				continue
			}
			p := f[len(f)-1]
			if fc, ok := byPath[p]; ok {
				fc.Additions, _ = strconv.Atoi(f[0])
				fc.Deletions, _ = strconv.Atoi(f[1])
			}
		}
	}
	for _, p := range splitLines(untracked) {
		if b, err := os.ReadFile(filepath.Join(dir, p)); err == nil {
			byPath[p].Additions = bytes.Count(b, []byte("\n"))
		}
	}
	out := make([]FileChange, 0, len(order))
	for _, p := range order {
		out = append(out, *byPath[p])
	}
	return out, nil
}

// ShowBase returns the file content at the merge base with origin/<base> ("" if absent).
func ShowBase(ctx context.Context, dir, base, path string) (string, error) {
	mb := mergeBaseFor(ctx, dir, base)
	out, err := Run(ctx, dir, "show", mb+":"+path)
	if err != nil {
		return "", nil // new file
	}
	return out + "\n", nil
}

// HasChanges reports whether the worktree differs from origin/<base>.
func HasChanges(ctx context.Context, dir, base string) (bool, error) {
	ch, err := Changes(ctx, dir, base)
	return len(ch) > 0, err
}

// CommitAll stages everything and commits if there is anything to commit. Returns HEAD sha.
func CommitAll(ctx context.Context, dir, message string) (string, error) {
	if _, err := Run(ctx, dir, "add", "-A"); err != nil {
		return "", err
	}
	st, err := Run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(st) != "" {
		args := []string{"commit", "-m", message}
		if email, _ := Run(ctx, dir, "config", "user.email"); email == "" {
			args = append([]string{"-c", "user.name=xIssue", "-c", "user.email=xissue@localhost"}, args...)
		}
		if _, err := Run(ctx, dir, args...); err != nil {
			return "", err
		}
	}
	return Run(ctx, dir, "rev-parse", "HEAD")
}

// Push pushes branch to origin.
func Push(ctx context.Context, dir, branch string) error {
	if err := ValidateBranchName(branch); err != nil {
		return err
	}
	_, err := Run(ctx, dir, "push", "-u", "origin", "--", branch+":"+branch)
	return err
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// CommitInfo represents a single git commit summary.
type CommitInfo struct {
	SHA      string `json:"sha"`
	ShortSHA string `json:"shortSha"`
	Subject  string `json:"subject"`
	Author   string `json:"author"`
	Date     string `json:"date"`
}

// ListBranches lists branch names available in mirror (both origin/* and local heads).
func ListBranches(ctx context.Context, mirror string) ([]string, error) {
	out, err := Run(ctx, mirror, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin", "refs/heads")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var branches []string
	for _, line := range splitLines(out) {
		name := strings.TrimPrefix(line, "origin/")
		if name == "" || name == "HEAD" || name == "origin" || strings.HasPrefix(name, "agent/") {
			continue
		}
		if !seen[name] {
			seen[name] = true
			branches = append(branches, name)
		}
	}
	return branches, nil
}

// BranchTip resolves a branch name in mirror and returns its latest commit info.
func BranchTip(ctx context.Context, mirror, branch string) (CommitInfo, error) {
	if err := ValidateBranchName(branch); err != nil {
		return CommitInfo{}, err
	}
	ref := resolveBaseRef(ctx, mirror, branch)
	out, err := Run(ctx, mirror, "log", "-n", "1", "--format=%H%x09%h%x09%s%x09%an%x09%cI", ref)
	if err != nil {
		return CommitInfo{}, err
	}
	commits := parseCommitLines(out)
	if len(commits) == 0 {
		return CommitInfo{}, fmt.Errorf("no commits on %s", branch)
	}
	return commits[0], nil
}

// ListCommits returns the most recent commits on branch in mirror.
func ListCommits(ctx context.Context, mirror, branch string, limit int) ([]CommitInfo, error) {
	if err := ValidateBranchName(branch); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	ref := resolveBaseRef(ctx, mirror, branch)
	out, err := Run(ctx, mirror, "log", "-n", strconv.Itoa(limit), "--format=%H%x09%h%x09%s%x09%an%x09%cI", ref)
	if err != nil {
		return nil, err
	}
	return parseCommitLines(out), nil
}

// ListCommitsRange returns commits in oldSHA..newSHA (newest first).
func ListCommitsRange(ctx context.Context, mirror, oldSHA, newSHA string, limit int) ([]CommitInfo, error) {
	if err := ValidateSHA(oldSHA); err != nil {
		return nil, err
	}
	if err := ValidateSHA(newSHA); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	out, err := Run(ctx, mirror, "log", "-n", strconv.Itoa(limit), "--format=%H%x09%h%x09%s%x09%an%x09%cI", oldSHA+".."+newSHA)
	if err != nil {
		return nil, err
	}
	return parseCommitLines(out), nil
}

func parseCommitLines(out string) []CommitInfo {
	var list []CommitInfo
	for _, line := range splitLines(out) {
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 5 {
			continue
		}
		list = append(list, CommitInfo{
			SHA:      parts[0],
			ShortSHA: parts[1],
			Subject:  parts[2],
			Author:   parts[3],
			Date:     parts[4],
		})
	}
	return list
}

// CherryPickAndPush creates a temporary worktree on targetBaseBranch, cherry-picks shas,
// pushes newBranch to origin, and cleans up the worktree.
func CherryPickAndPush(ctx context.Context, mirror, tempDir, newBranch, targetBaseBranch string, shas []string) (headSHA string, subjects []string, err error) {
	if len(shas) == 0 {
		return "", nil, fmt.Errorf("no commits selected for cherry-pick")
	}
	if err := ValidateBranchName(newBranch); err != nil {
		return "", nil, err
	}
	if err := ValidateBranchName(targetBaseBranch); err != nil {
		return "", nil, err
	}
	for _, sha := range shas {
		sha = strings.TrimSpace(sha)
		if sha == "" {
			continue
		}
		if err := ValidateSHA(sha); err != nil {
			return "", nil, err
		}
	}
	if err := AddWorktree(ctx, mirror, tempDir, newBranch, targetBaseBranch); err != nil {
		return "", nil, err
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = RemoveWorktree(cctx, mirror, tempDir, newBranch)
	}()

	// Configure fallback author if needed.
	if email, _ := Run(ctx, tempDir, "config", "user.email"); email == "" {
		_, _ = Run(ctx, tempDir, "config", "user.name", "xIssue")
		_, _ = Run(ctx, tempDir, "config", "user.email", "xissue@localhost")
	}

	for _, sha := range shas {
		sha = strings.TrimSpace(sha)
		if sha == "" {
			continue
		}
		if subj, err := Run(ctx, mirror, "log", "-n", "1", "--format=%s", sha); err == nil && subj != "" {
			subjects = append(subjects, subj)
		}
		if _, err := Run(ctx, tempDir, "cherry-pick", "--", sha); err != nil {
			_, _ = Run(ctx, tempDir, "cherry-pick", "--abort")
			return "", subjects, fmt.Errorf("cherry-pick %s failed: %w", sha, err)
		}
	}

	headSHA, err = Run(ctx, tempDir, "rev-parse", "HEAD")
	if err != nil {
		return "", subjects, err
	}
	if err := Push(ctx, tempDir, newBranch); err != nil {
		return headSHA, subjects, err
	}
	return headSHA, subjects, nil
}

