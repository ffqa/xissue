package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xpier/xissue/internal/domain"
)

const githubBatchSize = 20

func gqlString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func githubBatches(repos []domain.Repo) [][]domain.Repo {
	var out [][]domain.Repo
	for len(repos) > 0 {
		n := githubBatchSize
		if len(repos) < n {
			n = len(repos)
		}
		out = append(out, repos[:n])
		repos = repos[n:]
	}
	return out
}

func githubBatchQuery(repos []domain.Repo, fields string) string {
	var b strings.Builder
	b.WriteString("{")
	for i, repo := range repos {
		owner, name, ok := strings.Cut(strings.TrimSpace(repo.OwnerRepo), "/")
		if !ok || owner == "" || name == "" {
			continue
		}
		fmt.Fprintf(&b, " r%d: repository(owner:%s, name:%s) %s", i, gqlString(owner), gqlString(name), fields)
	}
	b.WriteString(" }")
	return b.String()
}

type githubGraphQLResponse struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func githubGraphQL(ctx context.Context, query string, out any) error {
	b, err := gh(ctx, "api", "graphql", "-f", "query="+query)
	if err != nil {
		return err
	}
	var envelope githubGraphQLResponse
	if err := json.Unmarshal(b, &envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("github graphql: %s", envelope.Errors[0].Message)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// GitHubBatchIssues lists open issues for several repositories in one GraphQL
// request. The caller must fall back to per-repo calls on error.
func GitHubBatchIssues(ctx context.Context, repos []domain.Repo) (map[int64][]RemoteIssue, error) {
	out := map[int64][]RemoteIssue{}
	for _, batch := range githubBatches(repos) {
		query := githubBatchQuery(batch, `{ issues(first:100, states:OPEN, orderBy:{field:CREATED_AT, direction:DESC}) { nodes { number title body url author { login } labels(first:20) { nodes { name } } } } }`)
		var data map[string]struct {
			Issues struct {
				Nodes []struct {
					Number int64  `json:"number"`
					Title  string `json:"title"`
					Body   string `json:"body"`
					URL    string `json:"url"`
					Author struct {
						Login string `json:"login"`
					} `json:"author"`
					Labels struct {
						Nodes []struct {
							Name string `json:"name"`
						} `json:"nodes"`
					} `json:"labels"`
				} `json:"nodes"`
			} `json:"issues"`
		}
		if err := githubGraphQL(ctx, query, &data); err != nil {
			return nil, err
		}
		for i, repo := range batch {
			item, ok := data[fmt.Sprintf("r%d", i)]
			if !ok {
				return nil, fmt.Errorf("github graphql: missing result for %s", repo.OwnerRepo)
			}
			for _, n := range item.Issues.Nodes {
				labels := make([]string, 0, len(n.Labels.Nodes))
				for _, l := range n.Labels.Nodes {
					labels = append(labels, l.Name)
				}
				out[repo.ID] = append(out[repo.ID], RemoteIssue{
					Number: n.Number, Title: n.Title, Body: n.Body, URL: n.URL,
					Author: n.Author.Login, Labels: labels,
				})
			}
		}
	}
	return out, nil
}

// GitHubBatchPRs lists PRs for several repositories in one GraphQL request.
// CI rollups are intentionally left to the per-PR detail path; list rendering
// remains fast and one repo failure falls back at the caller.
func GitHubBatchPRs(ctx context.Context, repos []domain.Repo, state string) (map[int64][]RemotePR, error) {
	states := []string{"OPEN"}
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "merged":
		states = []string{"MERGED"}
	case "closed":
		states = []string{"CLOSED"}
	case "all":
		states = []string{"OPEN", "MERGED", "CLOSED"}
	}
	stateArg := "[" + strings.Join(states, ",") + "]"
	out := map[int64][]RemotePR{}
	for _, batch := range githubBatches(repos) {
		query := githubBatchQuery(batch, fmt.Sprintf(`{ pullRequests(first:100, states:%s, orderBy:{field:CREATED_AT, direction:DESC}) { pageInfo { hasNextPage } nodes { number title state url headRefName baseRefName author { login } labels(first:20) { nodes { name } } createdAt updatedAt } } }`, stateArg))
		var data map[string]struct {
			PullRequests struct {
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
				Nodes []struct {
					Number      int64  `json:"number"`
					Title       string `json:"title"`
					Body        string `json:"body"`
					State       string `json:"state"`
					URL         string `json:"url"`
					HeadRefName string `json:"headRefName"`
					BaseRefName string `json:"baseRefName"`
					Author      struct {
						Login string `json:"login"`
					} `json:"author"`
					Labels struct {
						Nodes []struct {
							Name string `json:"name"`
						} `json:"nodes"`
					} `json:"labels"`
					CreatedAt string `json:"createdAt"`
					UpdatedAt string `json:"updatedAt"`
				} `json:"nodes"`
			} `json:"pullRequests"`
		}
		if err := githubGraphQL(ctx, query, &data); err != nil {
			return nil, err
		}
		for i, repo := range batch {
			item, ok := data[fmt.Sprintf("r%d", i)]
			if !ok {
				return nil, fmt.Errorf("github graphql: missing result for %s", repo.OwnerRepo)
			}
			if item.PullRequests.PageInfo.HasNextPage {
				// The batch query is capped at 100. Fall back to the per-repo
				// list so large repositories are not silently truncated.
				return nil, fmt.Errorf("github graphql: %s has more than 100 PRs", repo.OwnerRepo)
			}
			for _, n := range item.PullRequests.Nodes {
				labels := make([]string, 0, len(n.Labels.Nodes))
				for _, l := range n.Labels.Nodes {
					labels = append(labels, l.Name)
				}
				out[repo.ID] = append(out[repo.ID], RemotePR{
					Number: n.Number, Title: n.Title,
					State: strings.ToLower(n.State), URL: n.URL,
					HeadBranch: n.HeadRefName, BaseBranch: n.BaseRefName,
					Author: n.Author.Login, Labels: labels,
					CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
					RepoID: repo.ID, RepoName: repo.Name,
				})
			}
		}
	}
	return out, nil
}
