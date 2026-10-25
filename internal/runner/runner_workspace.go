package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
)

// repoWorktreePath joins a repository name to a run workspace after validating
// that the result stays a direct child of the run directory.
func repoWorktreePath(runDir, repoName string) (string, error) {
	if err := domain.ValidateRepoName(repoName); err != nil {
		return "", err
	}
	base := filepath.Clean(runDir)
	path := filepath.Clean(filepath.Join(base, strings.TrimSpace(repoName)))
	if path == base || !strings.HasPrefix(path, base+string(filepath.Separator)) {
		return "", fmt.Errorf("repository %q escapes run workspace", repoName)
	}
	return path, nil
}

func (r *Runner) prepareWorkspace(ctx context.Context, run *domain.Run, proj *domain.Project, issue *domain.Issue) error {
	if err := os.MkdirAll(run.Workdir, 0o755); err != nil {
		return err
	}
	for _, repo := range reposForIssue(proj, issue) {
		r.emitEvent(run.ID, issue.ID, "system", map[string]any{
			"text": fmt.Sprintf("Preparing repository %s (%s @ %s)…", repo.Name, repo.RemoteURL, repo.DefaultBranch),
		})
		mirror := filepath.Join(r.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repo.ID))
		if err := gitx.EnsureMirror(ctx, repo.RemoteURL, mirror); err != nil {
			return fmt.Errorf("mirror %s: %w", repo.Name, err)
		}
		wtDir, err := repoWorktreePath(run.Workdir, repo.Name)
		if err != nil {
			return fmt.Errorf("repository %s: %w", repo.Name, err)
		}
		if _, err := os.Stat(wtDir); err == nil {
			continue
		}
		if err := gitx.AddWorktree(ctx, mirror, wtDir, run.Branch, repo.DefaultBranch); err != nil {
			return fmt.Errorf("worktree %s: %w", repo.Name, err)
		}
	}
	agentsMD := buildWorkspaceAGENTS(proj, issue)
	_ = os.WriteFile(filepath.Join(run.Workdir, "AGENTS.md"), []byte(agentsMD), 0o644)
	return nil
}

func (r *Runner) Changes(ctx context.Context, runID int64) ([]RepoChanges, error) {
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return nil, err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return nil, err
	}
	repos := reposForIssue(proj, issue)
	out := make([]RepoChanges, 0, len(repos))
	for _, repo := range repos {
		wt, err := repoWorktreePath(run.Workdir, repo.Name)
		if err != nil {
			continue
		}
		files, err := gitx.Changes(ctx, wt, repo.DefaultBranch)
		if err != nil || files == nil {
			files = []gitx.FileChange{}
		}
		out = append(out, RepoChanges{
			RepoID:   repo.ID,
			RepoName: repo.Name,
			Branch:   run.Branch,
			Files:    files,
		})
	}
	return out, nil
}

func (r *Runner) ReadFile(ctx context.Context, runID, repoID int64, relPath, side string) (string, error) {
	run, repo, wt, _, err := r.resolveRepoFile(ctx, runID, repoID, relPath)
	if err != nil {
		return "", err
	}
	_ = run
	if side == "base" {
		return gitx.ShowBase(ctx, wt, repo.DefaultBranch, relPath)
	}
	root, err := os.OpenRoot(wt)
	if err != nil {
		return "", err
	}
	defer root.Close()
	b, err := root.ReadFile(filepath.FromSlash(relPath))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func (r *Runner) WriteFile(ctx context.Context, runID, repoID int64, relPath, content string) error {
	_, _, wt, _, err := r.resolveRepoFile(ctx, runID, repoID, relPath)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(wt)
	if err != nil {
		return err
	}
	defer root.Close()
	rel := filepath.FromSlash(relPath)
	if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return err
	}
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (r *Runner) resolveRepoFile(ctx context.Context, runID, repoID int64, relPath string) (*domain.Run, *domain.Repo, string, string, error) {
	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, "", "", err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return nil, nil, "", "", err
	}
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return nil, nil, "", "", err
	}
	if repo.ProjectID != issue.ProjectID {
		return nil, nil, "", "", errors.New("repository does not belong to this project")
	}
	if filepath.IsAbs(relPath) || filepath.VolumeName(relPath) != "" || strings.ContainsRune(relPath, 0) {
		return nil, nil, "", "", errors.New("invalid file path")
	}
	for _, seg := range strings.Split(filepath.ToSlash(relPath), "/") {
		if seg == ".git" || seg == ".." {
			return nil, nil, "", "", errors.New("invalid file path")
		}
	}
	wt, err := repoWorktreePath(run.Workdir, repo.Name)
	if err != nil {
		return nil, nil, "", "", err
	}
	abs := filepath.Clean(filepath.Join(wt, relPath))
	if abs != wt && !strings.HasPrefix(abs, wt+string(filepath.Separator)) {
		return nil, nil, "", "", errors.New("invalid file path")
	}
	return run, repo, wt, abs, nil
}

func sanitizeBranch(s string) string {
	s = strings.ToLower(nonBranchChars.ReplaceAllString(s, "-"))
	return strings.Trim(s, "-")
}

// reposForIssue narrows the workspace to the issue's selected repository.
// A nil repoID keeps the historical project-wide behavior.
func reposForIssue(proj *domain.Project, issue *domain.Issue) []domain.Repo {
	if issue == nil || issue.RepoID == nil {
		return proj.Repos
	}
	for _, repo := range proj.Repos {
		if repo.ID == *issue.RepoID {
			return []domain.Repo{repo}
		}
	}
	return proj.Repos
}

func buildInitialPrompt(proj *domain.Project, issue *domain.Issue, extra string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Please resolve issue [%s]: %s\n\n", issue.Key, issue.Title)
	if strings.TrimSpace(issue.Body) != "" {
		b.WriteString("## Issue Description\n\n")
		b.WriteString(issue.Body + "\n\n")
	}
	b.WriteString("## Workspace Repositories\n\n")
	repos := reposForIssue(proj, issue)
	for _, repo := range repos {
		fmt.Fprintf(&b, "- `./%s/` (base branch `%s`", repo.Name, repo.DefaultBranch)
		if repo.TestCommand != "" {
			fmt.Fprintf(&b, ", test command: `%s`", repo.TestCommand)
		}
		b.WriteString(")\n")
	}
	if issue.RepoID != nil && len(repos) == 1 {
		fmt.Fprintf(&b, "\nThis issue is scoped to `./%s/`; do not modify other repositories.\n", repos[0].Name)
	}
	if strings.TrimSpace(extra) != "" {
		b.WriteString("\n## Additional Instructions\n\n" + extra + "\n")
	}
	b.WriteString("\nMake the required changes inside the repository directory/directories above, run relevant tests, and summarize what you changed.\n")
	b.WriteString("IMPORTANT SAFETY RULES:\n")
	b.WriteString("- Do NOT run `git push`; xIssue handles committing, pushing, and opening PRs after review.\n")
	b.WriteString("- Do NOT run `pkill xissue`, `killall xissue`, `make stop`, `make restart`, `make deploy`, or `install-service.sh`, and do NOT bind to port 7789 — xIssue itself is supervising your process on port 7789.\n")
	return b.String()
}

// CleanupProject terminates all active and warm processes for the project and removes all
// associated disk resources (worktrees, git mirrors, issue markdown archives, and run logs).
func (r *Runner) CleanupProject(ctx context.Context, projectID int64) error {
	proj, err := r.st.GetProject(ctx, projectID)
	if err != nil {
		return nil
	}
	return r.CleanupProjectResources(ctx, proj)
}

// CleanupProjectResources removes on-disk resources for an already-loaded
// project.
func (r *Runner) CleanupProjectResources(ctx context.Context, proj *domain.Project) error {
	if proj == nil {
		return nil
	}
	issues, _ := r.st.ListIssues(ctx, proj.ID)
	runs := make(map[int64][]domain.Run, len(issues))
	for _, issue := range issues {
		rs, _ := r.st.ListRuns(ctx, issue.ID)
		runs[issue.ID] = rs
	}
	return r.CleanupProjectData(ctx, proj, issues, runs)
}

// CleanupProjectData removes on-disk resources using an in-memory snapshot.
// Callers deleting the project row first must snapshot issues/runs beforehand.
func (r *Runner) CleanupProjectData(ctx context.Context, proj *domain.Project, issues []domain.Issue, runsByIssue map[int64][]domain.Run) error {
	if proj == nil {
		return nil
	}
	projectID := proj.ID
	// 1. Find all issues and runs for this project
	for _, iss := range issues {
		for _, run := range runsByIssue[iss.ID] {
			// Stop active agent processes
			_ = r.Cancel(run.ID)
			// Close warm session
			r.closeWarmClient(run.ID)
			r.mu.Lock()
			delete(r.allowSet, run.ID)
			r.mu.Unlock()

			// Remove worktrees and workdir
			if run.Workdir != "" {
				r.cleanupRunWorkdir(ctx, &run, proj)
			}
		}
	}

	// 2. Remove all git mirror directories and prune local repo worktrees
	for _, repo := range proj.Repos {
		mirrorPath := filepath.Join(r.cfg.MirrorsDir(), fmt.Sprintf("repo-%d.git", repo.ID))
		_ = os.RemoveAll(mirrorPath)

		// If it's a local directory repository, prune orphaned worktree references
		if gitx.IsLocalDir(repo.RemoteURL) {
			pctx, pcancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = gitx.Run(pctx, repo.RemoteURL, "worktree", "prune")
			pcancel()
		}
	}

	// 3. Remove issues markdown directory: ~/.xissue/issues/project-<id>
	issuesDir := filepath.Join(r.cfg.IssuesDir(), fmt.Sprintf("project-%d", projectID))
	_ = os.RemoveAll(issuesDir)

	// 4. Remove archive directory: ~/.xissue/issue/<sanitized projectName>
	workspace := sanitizeFileName(proj.Name)
	if workspace == "" {
		workspace = fmt.Sprintf("project-%d", projectID)
	}
	archiveDir := filepath.Join(r.cfg.DataDir, "issue", workspace)
	_ = os.RemoveAll(archiveDir)

	return nil
}
