package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/xpier/xissue/internal/domain"
)

// github uses the local `gh` CLI so authentication is reused from `gh auth login`.
type github struct{}

func (github) Caps() Caps { return Caps{Issues: true, PRs: true} }

func gh(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

func requireOwnerRepo(repo domain.Repo) error {
	if repo.OwnerRepo == "" {
		return fmt.Errorf("repo %q has no ownerRepo configured", repo.Name)
	}
	return nil
}

func (github) ListOpenIssues(ctx context.Context, repo domain.Repo) ([]RemoteIssue, error) {
	if err := requireOwnerRepo(repo); err != nil {
		return nil, err
	}
	b, err := gh(ctx, "issue", "list", "-R", repo.OwnerRepo, "--state", "open", "--limit", "200",
		"--json", "number,title,body,labels,url")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number int64  `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		URL    string `json:"url"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]RemoteIssue, 0, len(raw))
	for _, r := range raw {
		labels := []string{}
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		out = append(out, RemoteIssue{Number: r.Number, Title: r.Title, Body: r.Body, URL: r.URL, Labels: labels})
	}
	return out, nil
}

func (github) CreateIssue(ctx context.Context, repo domain.Repo, title, body string) (*RemoteIssue, error) {
	if err := requireOwnerRepo(repo); err != nil {
		return nil, err
	}
	b, err := gh(ctx, "issue", "create", "-R", repo.OwnerRepo, "--title", title, "--body", body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimSpace(string(b))
	idx := strings.LastIndex(url, "/")
	n, err := strconv.ParseInt(url[idx+1:], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("unexpected gh output %q", url)
	}
	return &RemoteIssue{Number: n, Title: title, Body: body, URL: url}, nil
}

func (github) Comment(ctx context.Context, repo domain.Repo, number int64, body string) error {
	if err := requireOwnerRepo(repo); err != nil {
		return err
	}
	_, err := gh(ctx, "issue", "comment", strconv.FormatInt(number, 10), "-R", repo.OwnerRepo, "--body", body)
	return err
}

func (github) CreatePR(ctx context.Context, repo domain.Repo, head, base, title, body string) (string, error) {
	if err := requireOwnerRepo(repo); err != nil {
		return "", err
	}
	b, err := gh(ctx, "pr", "create", "-R", repo.OwnerRepo, "--head", head, "--base", base, "--title", title, "--body", body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// CheckPRStatus queries the current state ("open", "merged", "closed") of a PR by URL.
// Works even for PRs opened against upstream or third-party repositories.
func CheckPRStatus(ctx context.Context, prURL string) (state string, title string, err error) {
	prURL = strings.TrimSpace(prURL)
	if prURL == "" {
		return "", "", fmt.Errorf("empty pr url")
	}
	b, err := gh(ctx, "pr", "view", prURL, "--json", "state,mergedAt,title")
	if err != nil {
		return "", "", err
	}
	var res struct {
		State    string `json:"state"`
		MergedAt string `json:"mergedAt"`
		Title    string `json:"title"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		return "", "", err
	}
	st := strings.ToUpper(strings.TrimSpace(res.State))
	switch {
	case st == "MERGED" || res.MergedAt != "":
		return "merged", res.Title, nil
	case st == "CLOSED":
		return "closed", res.Title, nil
	default:
		return "open", res.Title, nil
	}
}

// GetPRDetails fetches detailed information of a PR including comments and reviews.
func GetPRDetails(ctx context.Context, prURL string) (*PRDetailInfo, error) {
	prURL = strings.TrimSpace(prURL)
	if prURL == "" {
		return nil, fmt.Errorf("empty pr url")
	}
	b, err := gh(ctx, "pr", "view", prURL, "--json", "number,title,state,mergedAt,url,headRefName,baseRefName,author,comments,latestReviews")
	if err != nil {
		return nil, err
	}
	var raw struct {
		Number      int64  `json:"number"`
		Title       string `json:"title"`
		State       string `json:"state"`
		MergedAt    string `json:"mergedAt"`
		URL         string `json:"url"`
		HeadRefName string `json:"headRefName"`
		BaseRefName string `json:"baseRefName"`
		Author      struct {
			Login string `json:"login"`
		} `json:"author"`
		Comments []struct {
			ID        string `json:"id"`
			Body      string `json:"body"`
			CreatedAt string `json:"createdAt"`
			Author    struct {
				Login string `json:"login"`
			} `json:"author"`
		} `json:"comments"`
		LatestReviews []struct {
			ID          string `json:"id"`
			Body        string `json:"body"`
			State       string `json:"state"`
			SubmittedAt string `json:"submittedAt"`
			Author      struct {
				Login string `json:"login"`
			} `json:"author"`
		} `json:"latestReviews"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}

	st := strings.ToUpper(strings.TrimSpace(raw.State))
	stateStr := "open"
	switch {
	case st == "MERGED" || raw.MergedAt != "":
		stateStr = "merged"
	case st == "CLOSED":
		stateStr = "closed"
	}

	detail := &PRDetailInfo{
		Number:      raw.Number,
		Title:       raw.Title,
		State:       stateStr,
		URL:         raw.URL,
		HeadRefName: raw.HeadRefName,
		BaseRefName: raw.BaseRefName,
		Author:      raw.Author.Login,
		Comments:    []PRComment{},
		LatestReviews: []PRReview{},
	}

	for _, c := range raw.Comments {
		detail.Comments = append(detail.Comments, PRComment{
			ID:     c.ID,
			Author: c.Author.Login,
			Body:   c.Body,
		})
	}

	for _, r := range raw.LatestReviews {
		detail.LatestReviews = append(detail.LatestReviews, PRReview{
			ID:     r.ID,
			Author: r.Author.Login,
			Body:   r.Body,
			State:  strings.ToLower(r.State),
		})
	}

	return detail, nil
}

func (github) ListPRs(ctx context.Context, repo domain.Repo, state string) ([]RemotePR, error) {
	if err := requireOwnerRepo(repo); err != nil {
		return nil, err
	}
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "" {
		state = "open"
	}
	args := []string{"pr", "list", "-R", repo.OwnerRepo, "--limit", "100",
		"--json", "number,title,body,state,url,headRefName,baseRefName,author,labels,createdAt,updatedAt"}
	if state != "all" {
		args = append(args, "--state", state)
	} else {
		args = append(args, "--state", "all")
	}
	b, err := gh(ctx, args...)
	if err != nil {
		return nil, err
	}
	var raw []struct {
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
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]RemotePR, 0, len(raw))
	for _, r := range raw {
		labels := []string{}
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		st := strings.ToLower(strings.TrimSpace(r.State))
		out = append(out, RemotePR{
			Number:     r.Number,
			Title:      r.Title,
			Body:       r.Body,
			State:      st,
			URL:        r.URL,
			HeadBranch: r.HeadRefName,
			BaseBranch: r.BaseRefName,
			Author:     r.Author.Login,
			Labels:     labels,
			CreatedAt:  r.CreatedAt,
			UpdatedAt:  r.UpdatedAt,
			RepoID:     repo.ID,
			RepoName:   repo.Name,
		})
	}
	return out, nil
}

func (github) MergePR(ctx context.Context, repo domain.Repo, number int64, method string) (string, error) {
	if err := requireOwnerRepo(repo); err != nil {
		return "", err
	}
	method = strings.ToLower(strings.TrimSpace(method))
	flag := "--squash"
	switch method {
	case "merge":
		flag = "--merge"
	case "rebase":
		flag = "--rebase"
	default:
		flag = "--squash"
	}
	b, err := gh(ctx, "pr", "merge", strconv.FormatInt(number, 10), "-R", repo.OwnerRepo, flag)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (github) ClosePR(ctx context.Context, repo domain.Repo, number int64) error {
	if err := requireOwnerRepo(repo); err != nil {
		return err
	}
	_, err := gh(ctx, "pr", "close", strconv.FormatInt(number, 10), "-R", repo.OwnerRepo)
	return err
}


