package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
)

// gitlab talks to the GitLab REST API v4. Settings come from the environment:
//
//	XISSUE_GITLAB_URL   (fallback: GITLAB_URL)   e.g. https://gitlab.com
//	XISSUE_GITLAB_TOKEN (fallback: GITLAB_TOKEN, GITLAB_PRIVATE_TOKEN)
//
// A self-hosted GitLab is supported; one instance per process is enough.
type gitlab struct{}

func (gitlab) CurrentUser(ctx context.Context) (string, error) {
	api, err := gitlabConfig()
	if err != nil {
		return "", err
	}
	var user struct {
		Username string `json:"username"`
	}
	if err := api.do(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return "", err
	}
	return strings.TrimSpace(user.Username), nil
}

func (gitlab) Caps() Caps {
	if _, err := gitlabConfig(); err != nil {
		return Caps{}
	}
	return Caps{Issues: true, PRs: true}
}

const gitlabPageSize = 50

type gitlabAPI struct {
	root    string
	apiBase string
	token   string
	client  *http.Client
}

func gitlabConfig() (*gitlabAPI, error) {
	root := firstEnv("XISSUE_GITLAB_URL", "GITLAB_URL")
	if root == "" {
		return nil, fmt.Errorf("gitlab: set XISSUE_GITLAB_URL (or GITLAB_URL) to your GitLab base URL")
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(root), "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("gitlab: invalid XISSUE_GITLAB_URL %q", root)
	}
	base := strings.TrimRight(u.String(), "/")
	if !strings.HasSuffix(base, "/api/v4") {
		base += "/api/v4"
	}
	return &gitlabAPI{
		root:    strings.TrimRight(u.String(), "/"),
		apiBase: base,
		token:   firstEnv("XISSUE_GITLAB_TOKEN", "GITLAB_TOKEN", "GITLAB_PRIVATE_TOKEN"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (g *gitlabAPI) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.apiBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.token != "" {
		req.Header.Set("PRIVATE-TOKEN", g.token)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return fmt.Errorf("gitlab %s %s: %d %s", method, path, resp.StatusCode, msg)
	}
	if out != nil && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("gitlab %s %s: decode response: %w", method, path, err)
		}
	}
	return nil
}

func gitlabRepoPath(repo domain.Repo) (string, error) {
	if err := requireOwnerRepo(repo); err != nil {
		return "", err
	}
	ownerRepo := strings.Trim(strings.TrimSpace(repo.OwnerRepo), "/")
	if ownerRepo == "" || !strings.Contains(ownerRepo, "/") {
		return "", fmt.Errorf("repo %q ownerRepo must be group/project", repo.Name)
	}
	return "/projects/" + url.PathEscape(ownerRepo), nil
}

type gitlabUser struct {
	Username string `json:"username"`
}

type gitlabIssue struct {
	IID         int64      `json:"iid"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	WebURL      string     `json:"web_url"`
	Labels      []string   `json:"labels"`
	Author      gitlabUser `json:"author"`
}

type gitlabNote struct {
	ID        int64      `json:"id"`
	Body      string     `json:"body"`
	System    bool       `json:"system"`
	CreatedAt string     `json:"created_at"`
	Author    gitlabUser `json:"author"`
}

type gitlabMR struct {
	IID          int64      `json:"iid"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	State        string     `json:"state"`
	WebURL       string     `json:"web_url"`
	SourceBranch string     `json:"source_branch"`
	TargetBranch string     `json:"target_branch"`
	Labels       []string   `json:"labels"`
	Author       gitlabUser `json:"author"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
	MergedAt     *string    `json:"merged_at"`
	SHA          string     `json:"sha"`
	DiffRefs     struct {
		HeadSHA string `json:"head_sha"`
	} `json:"diff_refs"`
}

type gitlabPipeline struct {
	Status string `json:"status"`
}

type gitlabApprovals struct {
	ApprovedBy []struct {
		User gitlabUser `json:"user"`
	} `json:"approved_by"`
}

func (gitlab) ListOpenIssues(ctx context.Context, repo domain.Repo) ([]RemoteIssue, error) {
	api, err := gitlabConfig()
	if err != nil {
		return nil, err
	}
	rp, err := gitlabRepoPath(repo)
	if err != nil {
		return nil, err
	}
	out := []RemoteIssue{}
	for page := 1; ; page++ {
		var batch []gitlabIssue
		path := fmt.Sprintf("%s/issues?state=opened&per_page=%d&page=%d", rp, gitlabPageSize, page)
		if err := api.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, issue := range batch {
			out = append(out, RemoteIssue{
				Number: issue.IID,
				Title:  issue.Title,
				Body:   issue.Description,
				URL:    issue.WebURL,
				Author: issue.Author.Username,
				Labels: issue.Labels,
			})
		}
		if len(batch) < gitlabPageSize {
			break
		}
	}
	return out, nil
}

func (gitlab) ListRepoIssueComments(ctx context.Context, repo domain.Repo) ([]RemoteIssueComment, error) {
	api, err := gitlabConfig()
	if err != nil {
		return nil, err
	}
	rp, err := gitlabRepoPath(repo)
	if err != nil {
		return nil, err
	}
	issues, err := (gitlab{}).ListOpenIssues(ctx, repo)
	if err != nil {
		return nil, err
	}
	out := []RemoteIssueComment{}
	for _, issue := range issues {
		for page := 1; ; page++ {
			var batch []gitlabNote
			path := fmt.Sprintf("%s/issues/%d/notes?per_page=100&page=%d&sort=asc", rp, issue.Number, page)
			if err := api.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
				return nil, err
			}
			for _, note := range batch {
				if note.System {
					continue
				}
				created, _ := time.Parse(time.RFC3339, note.CreatedAt)
				out = append(out, RemoteIssueComment{
					ID:          strconv.FormatInt(note.ID, 10),
					IssueNumber: issue.Number,
					Author:      note.Author.Username,
					Body:        note.Body,
					URL:         fmt.Sprintf("%s#note_%d", issue.URL, note.ID),
					CreatedAt:   created,
				})
			}
			if len(batch) < 100 {
				break
			}
		}
	}
	return out, nil
}

func (gitlab) CreateIssue(ctx context.Context, repo domain.Repo, title, body string) (*RemoteIssue, error) {
	api, err := gitlabConfig()
	if err != nil {
		return nil, err
	}
	rp, err := gitlabRepoPath(repo)
	if err != nil {
		return nil, err
	}
	var issue gitlabIssue
	if err := api.do(ctx, http.MethodPost, rp+"/issues", map[string]any{"title": title, "description": body}, &issue); err != nil {
		return nil, err
	}
	return &RemoteIssue{Number: issue.IID, Title: issue.Title, Body: issue.Description, URL: issue.WebURL, Author: issue.Author.Username, Labels: issue.Labels}, nil
}

func (gitlab) AddLabels(ctx context.Context, repo domain.Repo, number int64, labels []string) error {
	api, err := gitlabConfig()
	if err != nil {
		return err
	}
	rp, err := gitlabRepoPath(repo)
	if err != nil {
		return err
	}
	clean := make([]string, 0, len(labels))
	seen := map[string]bool{}
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || seen[strings.ToLower(label)] {
			continue
		}
		seen[strings.ToLower(label)] = true
		clean = append(clean, label)
	}
	if len(clean) == 0 {
		return nil
	}
	return api.do(ctx, http.MethodPut, fmt.Sprintf("%s/issues/%d", rp, number), map[string]any{"add_labels": strings.Join(clean, ",")}, nil)
}

func (gitlab) Comment(ctx context.Context, repo domain.Repo, number int64, body string) error {
	api, err := gitlabConfig()
	if err != nil {
		return err
	}
	rp, err := gitlabRepoPath(repo)
	if err != nil {
		return err
	}
	return api.do(ctx, http.MethodPost, fmt.Sprintf("%s/issues/%d/notes", rp, number), map[string]any{"body": body}, nil)
}

func (gitlab) CreatePR(ctx context.Context, repo domain.Repo, head, base, title, body string) (string, error) {
	api, err := gitlabConfig()
	if err != nil {
		return "", err
	}
	rp, err := gitlabRepoPath(repo)
	if err != nil {
		return "", err
	}
	var mr gitlabMR
	payload := map[string]any{"source_branch": head, "target_branch": base, "title": title, "description": body}
	if err := api.do(ctx, http.MethodPost, rp+"/merge_requests", payload, &mr); err != nil {
		return "", err
	}
	return mr.WebURL, nil
}

func (gitlab) ListPRs(ctx context.Context, repo domain.Repo, state string) ([]RemotePR, error) {
	api, err := gitlabConfig()
	if err != nil {
		return nil, err
	}
	rp, err := gitlabRepoPath(repo)
	if err != nil {
		return nil, err
	}
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "" || state == "open" {
		state = "opened"
	}
	if state == "merged" || state == "closed" || state == "all" {
		// GitLab accepts these directly.
	} else {
		state = "opened"
	}
	out := []RemotePR{}
	for page := 1; ; page++ {
		var batch []gitlabMR
		path := fmt.Sprintf("%s/merge_requests?state=%s&per_page=%d&page=%d", rp, url.QueryEscape(state), gitlabPageSize, page)
		if err := api.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, mr := range batch {
			st := strings.ToLower(strings.TrimSpace(mr.State))
			if st == "opened" {
				st = "open"
			}
			mergedAt := ""
			if mr.MergedAt != nil {
				mergedAt = *mr.MergedAt
			}
			item := RemotePR{
				Number: mr.IID, Title: mr.Title, Body: mr.Description, State: st, URL: mr.WebURL,
				HeadBranch: mr.SourceBranch, BaseBranch: mr.TargetBranch, Author: mr.Author.Username,
				Labels: mr.Labels, CreatedAt: mr.CreatedAt, UpdatedAt: mr.UpdatedAt, MergedAt: mergedAt,
				RepoID: repo.ID, RepoName: repo.Name,
			}
			if st == "open" {
				var pipelines []gitlabPipeline
				if err := api.do(ctx, http.MethodGet, fmt.Sprintf("%s/merge_requests/%d/pipelines", rp, mr.IID), nil, &pipelines); err == nil {
					states := make([]string, 0, len(pipelines))
					for _, pipeline := range pipelines {
						states = append(states, pipeline.Status)
					}
					item.CIState, item.CIPassed, item.CIFailed, item.CIPending = summarizeCheckStates(states)
				}
			}
			out = append(out, item)
		}
		if len(batch) < gitlabPageSize {
			break
		}
	}
	return out, nil
}

func (gitlab) MergePR(ctx context.Context, repo domain.Repo, number int64, method string) (string, error) {
	api, err := gitlabConfig()
	if err != nil {
		return "", err
	}
	rp, err := gitlabRepoPath(repo)
	if err != nil {
		return "", err
	}
	payload := map[string]any{"squash": strings.EqualFold(strings.TrimSpace(method), "squash")}
	if err := api.do(ctx, http.MethodPut, fmt.Sprintf("%s/merge_requests/%d/merge", rp, number), payload, nil); err != nil {
		return "", err
	}
	return "merged", nil
}

func (gitlab) ClosePR(ctx context.Context, repo domain.Repo, number int64) error {
	api, err := gitlabConfig()
	if err != nil {
		return err
	}
	rp, err := gitlabRepoPath(repo)
	if err != nil {
		return err
	}
	return api.do(ctx, http.MethodPut, fmt.Sprintf("%s/merge_requests/%d", rp, number), map[string]any{"state_event": "close"}, nil)
}

func gitlabPRFromURL(prURL string) (*gitlabAPI, string, int64, bool) {
	u, err := url.Parse(strings.TrimSpace(prURL))
	if err != nil || u.Host == "" {
		return nil, "", 0, false
	}
	const marker = "/-/merge_requests/"
	idx := strings.Index(u.Path, marker)
	if idx < 0 {
		return nil, "", 0, false
	}
	project := strings.Trim(u.Path[:idx], "/")
	if project == "" {
		return nil, "", 0, false
	}
	rest := strings.Trim(u.Path[idx+len(marker):], "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		return nil, "", 0, false
	}
	iid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || iid <= 0 {
		return nil, "", 0, false
	}
	api, err := gitlabConfig()
	if err != nil {
		return nil, "", 0, false
	}
	// Do not send tokens for a GitLab URL from a different host.
	rootURL, err := url.Parse(api.root)
	if err != nil || !strings.EqualFold(rootURL.Host, u.Host) {
		return nil, "", 0, false
	}
	return api, project, iid, true
}

func gitlabCheckPRStatus(ctx context.Context, prURL string) (state string, title string, err error) {
	api, project, iid, ok := gitlabPRFromURL(prURL)
	if !ok {
		return "", "", fmt.Errorf("not a configured GitLab merge request URL")
	}
	var mr gitlabMR
	if err := api.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(project)+"/merge_requests/"+strconv.FormatInt(iid, 10), nil, &mr); err != nil {
		return "", "", err
	}
	return gitlabMRState(&mr), mr.Title, nil
}

func gitlabMRState(mr *gitlabMR) string {
	switch strings.ToLower(strings.TrimSpace(mr.State)) {
	case "merged":
		return "merged"
	case "closed":
		return "closed"
	default:
		if mr.MergedAt != nil && *mr.MergedAt != "" {
			return "merged"
		}
		return "open"
	}
}

func gitlabPRDetails(ctx context.Context, prURL string) (*PRDetailInfo, error) {
	api, project, iid, ok := gitlabPRFromURL(prURL)
	if !ok {
		return nil, fmt.Errorf("not a configured GitLab merge request URL")
	}
	rp := "/projects/" + url.PathEscape(project)
	mrPath := fmt.Sprintf("%s/merge_requests/%d", rp, iid)
	var mr gitlabMR
	if err := api.do(ctx, http.MethodGet, mrPath, nil, &mr); err != nil {
		return nil, err
	}
	headSHA := mr.DiffRefs.HeadSHA
	if headSHA == "" {
		headSHA = mr.SHA
	}
	detail := &PRDetailInfo{
		Number: mr.IID, Title: mr.Title, State: gitlabMRState(&mr), URL: mr.WebURL,
		HeadRefName: mr.SourceBranch, HeadSHA: headSHA, BaseRefName: mr.TargetBranch,
		Author: mr.Author.Username, Comments: []PRComment{}, LatestReviews: []PRReview{},
	}
	for page := 1; ; page++ {
		var batch []gitlabNote
		path := fmt.Sprintf("%s/notes?per_page=100&page=%d&sort=asc", mrPath, page)
		if err := api.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, note := range batch {
			if note.System {
				continue
			}
			created, _ := time.Parse(time.RFC3339, note.CreatedAt)
			detail.Comments = append(detail.Comments, PRComment{
				ID: strconv.FormatInt(note.ID, 10), Author: note.Author.Username, Body: note.Body, CreatedAt: created,
			})
		}
		if len(batch) < 100 {
			break
		}
	}
	var approvals gitlabApprovals
	if err := api.do(ctx, http.MethodGet, mrPath+"/approvals", nil, &approvals); err == nil {
		for _, a := range approvals.ApprovedBy {
			detail.LatestReviews = append(detail.LatestReviews, PRReview{Author: a.User.Username, State: "approved"})
		}
	}
	var pipelines []gitlabPipeline
	if err := api.do(ctx, http.MethodGet, mrPath+"/pipelines", nil, &pipelines); err == nil {
		states := make([]string, 0, len(pipelines))
		for _, p := range pipelines {
			states = append(states, p.Status)
		}
		detail.CIState, detail.CIPassed, detail.CIFailed, detail.CIPending = summarizeCheckStates(states)
	}
	return detail, nil
}
