package runner

import (
	"context"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/provider"
)

// warmUserCache resolves provider logins once in the background. PR list
// requests must never wait for gh/GitHub identity lookup.
func (r *Runner) warmUserCache() {
	for _, prov := range []domain.Provider{domain.ProviderGitHub, domain.ProviderGitea, domain.ProviderGitLab} {
		p := provider.For(prov)
		if !p.Caps().PRs {
			continue
		}
		ctx, cancel := context.WithTimeout(r.ctx, 15*time.Second)
		login, err := p.CurrentUser(ctx)
		cancel()
		if err != nil {
			continue
		}
		login = strings.TrimSpace(login)
		if login == "" {
			continue
		}
		r.userCacheMu.Lock()
		r.userCache[prov] = login
		r.userCacheMu.Unlock()
	}
}

func (r *Runner) cachedUserLogin(prov domain.Provider) string {
	r.userCacheMu.Lock()
	defer r.userCacheMu.Unlock()
	return r.userCache[prov]
}
