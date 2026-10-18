// Package gitx wraps the git CLI: bare mirrors, worktrees, diffs, commits, pushes.
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

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
// has at least one commit so `git worktree add` can branch off it.
func ensureLocalRepoHasCommit(ctx context.Context, dir, base string) error {
	if _, err := Run(ctx, dir, "rev-parse", "--verify", "HEAD"); err == nil {
		return nil
	}
	if base != "" {
		_, _ = Run(ctx, dir, "symbolic-ref", "HEAD", "refs/heads/"+base)
	}
	_, _ = Run(ctx, dir, "add", "-A")
	args := []string{"commit", "--allow-empty", "-m", "Initial commit"}
	if email, _ := Run(ctx, dir, "config", "user.email"); email == "" {
		args = append([]string{"-c", "user.name=xIssue", "-c", "user.email=xissue@localhost"}, args...)
	}
	_, err := Run(ctx, dir, args...)
	return err
}

// EnsureMirror makes sure a bare partial clone of remote exists at path and is up to date.
// All run worktrees are created from this mirror so they share one object store.
func EnsureMirror(ctx context.Context, remote, path string) error {
	l := lockFor(path)
	l.Lock()
	defer l.Unlock()
	if isLocalDir(remote) {
		if err := ensureLocalRepoHasCommit(ctx, remote, ""); err != nil {
			return fmt.Errorf("initialize local repo %s: %w", remote, err)
		}
	}
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if _, err := Run(ctx, "", "clone", "--bare", "--filter=blob:none", remote, path); err != nil {
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
	l := lockFor(mirror)
	l.Lock()
	defer l.Unlock()
	startPoint := resolveBaseRef(ctx, mirror, base)
	_, err := Run(ctx, mirror, "worktree", "add", "-B", branch, dir, startPoint)
	return err
}

// RemoveWorktree removes the worktree and deletes its local branch.
func RemoveWorktree(ctx context.Context, mirror, dir, branch string) error {
	l := lockFor(mirror)
	l.Lock()
	defer l.Unlock()
	_, err := Run(ctx, mirror, "worktree", "remove", "--force", dir)
	if err != nil && !strings.Contains(err.Error(), "is not a working tree") {
		return err
	}
	_, _ = Run(ctx, mirror, "worktree", "prune")
	if branch != "" {
		_, _ = Run(ctx, mirror, "branch", "-D", branch)
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
	_, err := Run(ctx, dir, "push", "-u", "origin", branch+":"+branch)
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
