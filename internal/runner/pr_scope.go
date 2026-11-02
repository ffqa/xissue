package runner

import (
	"strings"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/provider"
)

// repoCanListPRs reports whether pull requests can be queried for a repo: the
// provider supports PRs, the platform owner/repo is known, and the working copy
// is not a local-only directory. A local-only repo has no PRs of its own, and a
// stale platform config on it would otherwise surface another repo's PRs.
func repoCanListPRs(repo domain.Repo) bool {
	if !provider.For(repo.Provider).Caps().PRs {
		return false
	}
	if strings.TrimSpace(repo.OwnerRepo) == "" {
		return false
	}
	// Local paths (including ones that no longer exist) carry no PRs.
	return gitx.LooksRemoteURL(repo.RemoteURL)
}
