package runner

import (
	"fmt"
	"strings"

	"github.com/xpier/xissue/internal/provider"
)

// dedupePRs keeps one entry per pull request. Two repository entries can point
// at the same remote (for example a repo added twice under different names),
// which would otherwise list every PR of that remote twice.
func dedupePRs(prs []provider.RemotePR) []provider.RemotePR {
	if len(prs) < 2 {
		return prs
	}
	seen := make(map[string]struct{}, len(prs))
	out := make([]provider.RemotePR, 0, len(prs))
	for _, pr := range prs {
		key := strings.ToLower(strings.TrimRight(strings.TrimSpace(pr.URL), "/"))
		if key == "" {
			key = fmt.Sprintf("%d#%d", pr.RepoID, pr.Number)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, pr)
	}
	return out
}
