package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xpier/xissue/internal/domain"
)

// githubListPRsAll walks every GraphQL page for one repository. There is no
// arbitrary --limit: large projects keep paging until hasNextPage is false.
func githubListPRsAll(ctx context.Context, repo domain.Repo, state string) ([]RemotePR, error) {
	if err := requireOwnerRepo(repo); err != nil {
		return nil, err
	}
	state = strings.ToLower(strings.TrimSpace(state))
	states := []string{"OPEN"}
	switch state {
	case "merged":
		states = []string{"MERGED"}
	case "closed":
		states = []string{"CLOSED"}
	case "all":
		states = []string{"OPEN", "MERGED", "CLOSED"}
	}
	owner, name, ok := strings.Cut(strings.TrimSpace(repo.OwnerRepo), "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("repo %q has invalid ownerRepo", repo.Name)
	}
	owner, name = gqlString(owner), gqlString(name)
	stateArg := "[" + strings.Join(states, ",") + "]"
	const queryTemplate = `query($cursor:String) { repository(owner:%s, name:%s) { pullRequests(first:100, states:%s, orderBy:{field:CREATED_AT, direction:DESC}, after:$cursor) { pageInfo { hasNextPage endCursor } nodes { number title body state url headRefName baseRefName author { login } labels(first:20) { nodes { name } } createdAt updatedAt commits(last:1) { nodes { commit { statusCheckRollup { state } } } } } } } }`
	query := fmt.Sprintf(queryTemplate, owner, name, stateArg)

	out := []RemotePR{}
	cursor := ""
	for {
		args := []string{"api", "graphql", "-f", "query=" + query}
		if cursor != "" {
			args = append(args, "-F", "cursor="+cursor)
		}
		raw, err := gh(ctx, args...)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Data struct {
				Repository struct {
					PullRequests struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
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
							Commits   struct {
								Nodes []struct {
									Commit struct {
										StatusCheckRollup *struct {
											State string `json:"state"`
										} `json:"statusCheckRollup"`
									} `json:"commit"`
								} `json:"nodes"`
							} `json:"commits"`
						} `json:"nodes"`
					} `json:"pullRequests"`
				} `json:"repository"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, err
		}
		if len(envelope.Errors) > 0 {
			return nil, fmt.Errorf("github graphql: %s", envelope.Errors[0].Message)
		}
		page := envelope.Data.Repository.PullRequests
		for _, n := range page.Nodes {
			labels := make([]string, 0, len(n.Labels.Nodes))
			for _, l := range n.Labels.Nodes {
				labels = append(labels, l.Name)
			}
			item := RemotePR{
				Number: n.Number, Title: n.Title, Body: n.Body,
				State: strings.ToLower(n.State), URL: n.URL,
				HeadBranch: n.HeadRefName, BaseBranch: n.BaseRefName,
				Author: n.Author.Login, Labels: labels,
				CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
				RepoID: repo.ID, RepoName: repo.Name,
			}
			states := []string{}
			for _, c := range n.Commits.Nodes {
				if c.Commit.StatusCheckRollup != nil {
					states = append(states, c.Commit.StatusCheckRollup.State)
				}
			}
			item.CIState, item.CIPassed, item.CIFailed, item.CIPending = summarizeCheckStates(states)
			out = append(out, item)
		}
		if !page.PageInfo.HasNextPage || page.PageInfo.EndCursor == "" || page.PageInfo.EndCursor == cursor {
			break
		}
		cursor = page.PageInfo.EndCursor
	}
	return out, nil
}
