package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/provider"
)

// ProjectPRPage is one lazy page of project PRs. Cursor is opaque.
type ProjectPRPage struct {
	PRs        []provider.RemotePR `json:"prs"`
	NextCursor string              `json:"nextCursor,omitempty"`
	MineKnown  bool                `json:"mineKnown"`
}

// ListProjectPRPage returns the newest page per repository (up to 100 each) and
// a cursor for older GitHub pages. Non-GitHub providers are loaded fully on the
// first page because their APIs do not share the same cursor shape.
func (r *Runner) ListProjectPRPage(ctx context.Context, projectID int64, state, cursor string) (*ProjectPRPage, error) {
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "" {
		state = "open"
	}
	cursors := map[int64]string{}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid PR cursor")
		}
		if err := json.Unmarshal(raw, &cursors); err != nil {
			return nil, fmt.Errorf("invalid PR cursor")
		}
	}
	repos, err := r.st.ListRepos(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	all := []provider.RemotePR{}
	var errs []error
	next := map[int64]string{}
	for _, repo := range repos {
		p := provider.For(repo.Provider)
		if !p.Caps().PRs {
			continue
		}
		repo := repo
		wg.Add(1)
		go func() {
			defer wg.Done()
			var prs []provider.RemotePR
			var nextCursor string
			var err error
			if repo.Provider == domain.ProviderGitHub {
				repoCursor, more := cursors[repo.ID]
				if cursor != "" && !more {
					return
				}
				page, perr := provider.GitHubListPRsPage(ctx, repo, state, repoCursor)
				prs, nextCursor, err = page.PRs, page.NextCursor, perr
			} else if cursor == "" {
				prs, err = p.ListPRs(ctx, repo, state)
			} else {
				return // already loaded on the first page
			}
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", repo.Name, err))
				mu.Unlock()
				return
			}
			if login := r.cachedUserLogin(repo.Provider); login != "" {
				markPRsMine(prs, login)
			}
			mu.Lock()
			all = append(all, prs...)
			if nextCursor != "" {
				next[repo.ID] = nextCursor
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(all) == 0 && len(errs) > 0 {
		return nil, errs[0]
	}
	sort.SliceStable(all, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, all[i].CreatedAt)
		tj, _ := time.Parse(time.RFC3339, all[j].CreatedAt)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return all[i].Number > all[j].Number
	})
	nextCursor := ""
	if len(next) > 0 {
		raw, _ := json.Marshal(next)
		nextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	mineKnown := false
	for _, repo := range repos {
		if r.cachedUserLogin(repo.Provider) != "" {
			mineKnown = true
		}
	}
	return &ProjectPRPage{PRs: all, NextCursor: nextCursor, MineKnown: mineKnown}, nil
}
