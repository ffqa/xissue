package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/xpier/xissue/internal/gitx"
)

// ListRepoMergedBranches lists merged remote branches authored by the
// operator. refresh=true fetches the remote first; false reads the mirror.
func (r *Runner) ListRepoMergedBranches(ctx context.Context, repoID int64, refresh bool) ([]gitx.MergedBranch, error) {
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	mirror := r.mirrorPath(repo.ID)
	if refresh {
		mirror, err = r.ensureRepoMirror(ctx, *repo)
	} else {
		err = gitx.EnsureMirrorCached(ctx, repo.RemoteURL, mirror)
	}
	if err != nil {
		return nil, err
	}
	name, email, err := gitx.GitIdentity(ctx, repo.RemoteURL)
	if err != nil {
		return nil, err
	}
	return gitx.ListMergedBranches(ctx, mirror, repo.DefaultBranch, name, email)
}

// DeleteRepoMergedBranches deletes only branches that are still merged and
// authored by the configured identity. It validates the complete selection
// before deleting anything.
func (r *Runner) DeleteRepoMergedBranches(ctx context.Context, repoID int64, branches []string) ([]string, error) {
	if len(branches) == 0 {
		return nil, fmt.Errorf("no branches selected")
	}
	if len(branches) > 100 {
		return nil, fmt.Errorf("too many branches selected")
	}
	repo, err := r.st.GetRepo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	mirror, err := r.ensureRepoMirror(ctx, *repo)
	if err != nil {
		return nil, err
	}
	name, email, err := gitx.GitIdentity(ctx, repo.RemoteURL)
	if err != nil {
		return nil, err
	}
	merged, err := gitx.ListMergedBranches(ctx, mirror, repo.DefaultBranch, name, email)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(merged))
	for _, branch := range merged {
		allowed[branch.Name] = true
	}
	seen := map[string]bool{}
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if branch == "" || seen[branch] {
			continue
		}
		seen[branch] = true
		if !allowed[branch] {
			return nil, fmt.Errorf("branch %q is not a merged branch authored by you", branch)
		}
	}
	var deleted []string
	for branch := range seen {
		if err := gitx.DeleteRemoteBranch(ctx, mirror, branch); err != nil {
			return deleted, fmt.Errorf("delete %s: %w", branch, err)
		}
		deleted = append(deleted, branch)
	}
	return deleted, nil
}
