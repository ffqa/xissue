package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
)

// PRPage is one provider page plus the cursor for the next page.
type PRPage struct {
	PRs        []RemotePR `json:"prs"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

func githubToken(ctx context.Context) (string, error) {
	b, err := gh(ctx, "auth", "token")
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", fmt.Errorf("github token is empty")
	}
	return token, nil
}

func githubAPIBase() string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GITHUB_API_URL")), "/")
	if base == "" {
		base = "https://api.github.com"
	}
	return base
}

type githubRESTPR struct {
	Number    int64  `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	State     string `json:"state"`
	HTMLURL   string `json:"html_url"`
	MergedAt  string `json:"merged_at"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// githubListPRsPage uses the REST pulls endpoint instead of GraphQL. REST is
// materially faster for first paint and exposes page-number pagination; the
// UI only needs the newest 30 PRs before fetching older pages.
func githubListPRsPage(ctx context.Context, repo domain.Repo, state, cursor string) (PRPage, error) {
	if err := requireOwnerRepo(repo); err != nil {
		return PRPage{}, err
	}
	page, _ := strconv.Atoi(cursor)
	if page <= 0 {
		page = 1
	}
	state = strings.ToLower(strings.TrimSpace(state))
	restState := "open"
	switch state {
	case "merged", "closed":
		restState = "closed"
	case "all":
		restState = "all"
	}
	token, err := githubToken(ctx)
	if err != nil {
		return PRPage{}, err
	}
	endpoint := fmt.Sprintf("%s/repos/%s/pulls?state=%s&per_page=30&page=%d&sort=created&direction=desc",
		githubAPIBase(), repo.OwnerRepo, url.QueryEscape(restState), page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return PRPage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return PRPage{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PRPage{}, fmt.Errorf("github pulls: %s", strings.TrimSpace(string(body)))
	}
	var raw []githubRESTPR
	if err := json.Unmarshal(body, &raw); err != nil {
		return PRPage{}, err
	}
	out := make([]RemotePR, 0, len(raw))
	for _, item := range raw {
		st := "open"
		if strings.EqualFold(item.State, "closed") {
			if strings.TrimSpace(item.MergedAt) != "" {
				st = "merged"
			} else {
				st = "closed"
			}
		}
		if state == "merged" && st != "merged" {
			continue
		}
		if state == "closed" && st != "closed" {
			continue
		}
		labels := make([]string, 0, len(item.Labels))
		for _, l := range item.Labels {
			labels = append(labels, l.Name)
		}
		out = append(out, RemotePR{
			Number: item.Number, Title: item.Title, Body: item.Body,
			State: st, URL: item.HTMLURL,
			HeadBranch: item.Head.Ref, BaseBranch: item.Base.Ref,
			Author: item.User.Login, Labels: labels,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, MergedAt: item.MergedAt,
			RepoID: repo.ID, RepoName: repo.Name,
		})
	}
	next := ""
	if strings.Contains(resp.Header.Get("Link"), `rel="next"`) {
		next = strconv.Itoa(page + 1)
	}
	return PRPage{PRs: out, NextCursor: next}, nil
}

// GitHubListPRsPage exposes one page for the lazy project PR endpoint.
func GitHubListPRsPage(ctx context.Context, repo domain.Repo, state, cursor string) (PRPage, error) {
	return githubListPRsPage(ctx, repo, state, cursor)
}

// githubListPRsAll walks every page for callers that need full history.
func githubListPRsAll(ctx context.Context, repo domain.Repo, state string) ([]RemotePR, error) {
	out := []RemotePR{}
	cursor := ""
	for {
		page, err := githubListPRsPage(ctx, repo, state, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, page.PRs...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return out, nil
}
