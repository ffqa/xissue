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

