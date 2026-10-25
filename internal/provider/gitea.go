package provider

import (
	"bytes"
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

// gitea talks to the Gitea/Forgejo REST API. Connection settings come from the
// process environment so tokens never enter the SQLite database or Web UI:
//
//	XISSUE_GITEA_URL   (fallback: GITEA_URL)   e.g. https://gitea.example.com
//	XISSUE_GITEA_TOKEN (fallback: GITEA_TOKEN, GITEA_ACCESS_TOKEN)
//
// One instance per process is supported; use a Gitea instance that hosts the
// repositories you monitor.
type gitea struct{}

func (gitea) Caps() Caps { return Caps{Issues: true, PRs: true} }

const giteaPageSize = 50

type giteaAPI struct {
	root    string // https://host[/subpath]
	apiBase string // root or root/api/v1
	token   string
	client  *http.Client
}

func giteaConfig() (*giteaAPI, error) {
	root := firstEnv("XISSUE_GITEA_URL", "GITEA_URL")
	if root == "" {
		return nil, fmt.Errorf("gitea: set XISSUE_GITEA_URL (or GITEA_URL) to your Gitea/Forgejo base URL")
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(root), "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("gitea: invalid XISSUE_GITEA_URL %q", root)
	}
	apiBase := strings.TrimRight(u.String(), "/")
	if !strings.HasSuffix(apiBase, "/api/v1") {
		apiBase += "/api/v1"
	}
	return &giteaAPI{
		root:    strings.TrimRight(u.String(), "/"),
		apiBase: apiBase,
		token:   firstEnv("XISSUE_GITEA_TOKEN", "GITEA_TOKEN", "GITEA_ACCESS_TOKEN"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

func (g *giteaAPI) do(ctx context.Context, method, path string, body any, out any) error {
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
		req.Header.Set("Authorization", "token "+g.token)
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
		return fmt.Errorf("gitea %s %s: %d %s", method, path, resp.StatusCode, msg)
	}
	if out != nil && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("gitea %s %s: decode response: %w", method, path, err)
		}
	}
	return nil
}

func giteaRepoPath(repo domain.Repo) (string, error) {
	if err := requireOwnerRepo(repo); err != nil {
		return "", err
	}
	parts := strings.SplitN(strings.TrimSpace(repo.OwnerRepo), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("repo %q ownerRepo must be owner/repo", repo.Name)
	}
	return "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]), nil
}

type giteaUser struct {
	Login string `json:"login"`
}

type giteaLabel struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type giteaIssue struct {
	Number      int64        `json:"number"`
	Title       string       `json:"title"`
	Body        string       `json:"body"`
	HTMLURL     string       `json:"html_url"`
	Labels      []giteaLabel `json:"labels"`
	User        giteaUser    `json:"user"`
	PullRequest *struct{}    `json:"pull_request"`
}

type giteaComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	IssueURL  string    `json:"issue_url"`
	CreatedAt string    `json:"created_at"`
	User      giteaUser `json:"user"`
}

type giteaPRBranch struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type giteaPR struct {
	Number    int64         `json:"number"`
	Title     string        `json:"title"`
	Body      string        `json:"body"`
	State     string        `json:"state"`
	HTMLURL   string        `json:"html_url"`
	Merged    bool          `json:"merged"`
	MergedAt  *string       `json:"merged_at"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
	User      giteaUser     `json:"user"`
	Labels    []giteaLabel  `json:"labels"`
	Head      giteaPRBranch `json:"head"`
	Base      giteaPRBranch `json:"base"`
}

type giteaReview struct {
	ID          int64     `json:"id"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	SubmittedAt string    `json:"submitted_at"`
	User        giteaUser `json:"user"`
}

func (gitea) ListOpenIssues(ctx context.Context, repo domain.Repo) ([]RemoteIssue, error) {
	api, err := giteaConfig()
	if err != nil {
		return nil, err
	}
	rp, err := giteaRepoPath(repo)
	if err != nil {
		return nil, err
	}
	out := []RemoteIssue{}
	for page := 1; ; page++ {
		var batch []giteaIssue
		path := fmt.Sprintf("%s/issues?state=open&type=issues&limit=%d&page=%d", rp, giteaPageSize, page)
		if err := api.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, issue := range batch {
			if issue.PullRequest != nil {
				continue
			}
			labels := make([]string, 0, len(issue.Labels))
			for _, label := range issue.Labels {
				labels = append(labels, label.Name)
			}
			out = append(out, RemoteIssue{
				Number: issue.Number,
				Title:  issue.Title,
				Body:   issue.Body,
				URL:    issue.HTMLURL,
				Author: issue.User.Login,
				Labels: labels,
			})
		}
		if len(batch) < giteaPageSize {
			break
		}
	}
	return out, nil
}

func (gitea) ListRepoIssueComments(ctx context.Context, repo domain.Repo) ([]RemoteIssueComment, error) {
	api, err := giteaConfig()
	if err != nil {
		return nil, err
	}
	rp, err := giteaRepoPath(repo)
	if err != nil {
		return nil, err
	}
	out := []RemoteIssueComment{}
	for page := 1; ; page++ {
		var batch []giteaComment
		path := fmt.Sprintf("%s/issues/comments?limit=%d&page=%d", rp, giteaPageSize, page)
		if err := api.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, c := range batch {
			number := giteaIssueNumberFromURL(c.IssueURL)
			if number <= 0 {
				continue
			}
			created, _ := time.Parse(time.RFC3339, c.CreatedAt)
			out = append(out, RemoteIssueComment{
				ID:          strconv.FormatInt(c.ID, 10),
				IssueNumber: number,
				Author:      c.User.Login,
				Body:        c.Body,
				URL:         c.HTMLURL,
				CreatedAt:   created,
			})
		}
		if len(batch) < giteaPageSize {
			break
		}
	}
	return out, nil
}

func giteaIssueNumberFromURL(raw string) int64 {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return 0
	}
	idx := strings.LastIndex(raw, "/")
	if idx < 0 {
		return 0
	}
	n, _ := strconv.ParseInt(raw[idx+1:], 10, 64)
	return n
}

func (gitea) CreateIssue(ctx context.Context, repo domain.Repo, title, body string) (*RemoteIssue, error) {
	api, err := giteaConfig()
	if err != nil {
		return nil, err
	}
	rp, err := giteaRepoPath(repo)
	if err != nil {
		return nil, err
	}
	var issue giteaIssue
	if err := api.do(ctx, http.MethodPost, rp+"/issues", map[string]any{"title": title, "body": body}, &issue); err != nil {
		return nil, err
	}
	return &RemoteIssue{Number: issue.Number, Title: issue.Title, Body: issue.Body, URL: issue.HTMLURL}, nil
}

func giteaLabelColor(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bug":
		return "d73a4a"
	case "enhancement":
		return "a2eeef"
	case "suggestion":
		return "d4c5f9"
	default:
		return "ededed"
	}
}

func (g *giteaAPI) allLabels(ctx context.Context, rp string) (map[string]int64, error) {
	out := map[string]int64{}
	for page := 1; ; page++ {
		var batch []giteaLabel
		path := fmt.Sprintf("%s/labels?limit=%d&page=%d", rp, giteaPageSize, page)
		if err := g.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, label := range batch {
			out[strings.ToLower(strings.TrimSpace(label.Name))] = label.ID
		}
		if len(batch) < giteaPageSize {
			break
		}
	}
	return out, nil
}

func (g *giteaAPI) ensureLabelIDs(ctx context.Context, rp string, names []string) ([]int64, error) {
	existing, err := g.allLabels(ctx, rp)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(names))
	for _, name := range names {
		key := strings.ToLower(name)
		if id, ok := existing[key]; ok {
			ids = append(ids, id)
			continue
		}
		var created giteaLabel
		body := map[string]any{"name": name, "color": giteaLabelColor(name), "description": "Managed by xIssue"}
		if err := g.do(ctx, http.MethodPost, rp+"/labels", body, &created); err != nil {
			// A concurrent sync may have created it; refresh once.
			if refreshed, rerr := g.allLabels(ctx, rp); rerr == nil {
				if id, ok := refreshed[key]; ok {
					ids = append(ids, id)
					continue
				}
			}
			return nil, err
		}
		ids = append(ids, created.ID)
		existing[key] = created.ID
	}
	return ids, nil
}

func (gitea) AddLabels(ctx context.Context, repo domain.Repo, number int64, labels []string) error {
	api, err := giteaConfig()
	if err != nil {
		return err
	}
	rp, err := giteaRepoPath(repo)
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
	addIDs, err := api.ensureLabelIDs(ctx, rp, clean)
	if err != nil {
		return err
	}
	var issue giteaIssue
	if err := api.do(ctx, http.MethodGet, fmt.Sprintf("%s/issues/%d", rp, number), nil, &issue); err != nil {
		return err
	}
	all := map[int64]bool{}
	for _, label := range issue.Labels {
		all[label.ID] = true
	}
	for _, id := range addIDs {
		all[id] = true
	}
	ids := make([]int64, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	return api.do(ctx, http.MethodPatch, fmt.Sprintf("%s/issues/%d", rp, number), map[string]any{"labels": ids}, nil)
}

func (gitea) Comment(ctx context.Context, repo domain.Repo, number int64, body string) error {
	api, err := giteaConfig()
	if err != nil {
		return err
	}
	rp, err := giteaRepoPath(repo)
	if err != nil {
		return err
	}
	return api.do(ctx, http.MethodPost, fmt.Sprintf("%s/issues/%d/comments", rp, number), map[string]any{"body": body}, nil)
}

func (gitea) CreatePR(ctx context.Context, repo domain.Repo, head, base, title, body string) (string, error) {
	api, err := giteaConfig()
	if err != nil {
		return "", err
	}
	rp, err := giteaRepoPath(repo)
	if err != nil {
		return "", err
	}
	var pr giteaPR
	payload := map[string]any{"head": head, "base": base, "title": title, "body": body}
	if err := api.do(ctx, http.MethodPost, rp+"/pulls", payload, &pr); err != nil {
		return "", err
	}
	return pr.HTMLURL, nil
}

func (gitea) ListPRs(ctx context.Context, repo domain.Repo, state string) ([]RemotePR, error) {
	api, err := giteaConfig()
	if err != nil {
		return nil, err
	}
	rp, err := giteaRepoPath(repo)
	if err != nil {
		return nil, err
	}
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "" {
		state = "open"
	}
	if state == "merged" {
		state = "closed"
	}
	out := []RemotePR{}
	for page := 1; ; page++ {
		var batch []giteaPR
		path := fmt.Sprintf("%s/pulls?state=%s&limit=%d&page=%d", rp, url.QueryEscape(state), giteaPageSize, page)
		if err := api.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, pr := range batch {
			st := strings.ToLower(strings.TrimSpace(pr.State))
			if giteaPRMerged(&pr) {
				st = "merged"
			}
			labels := make([]string, 0, len(pr.Labels))
			for _, label := range pr.Labels {
				labels = append(labels, label.Name)
			}
			ciState, ciPassed, ciFailed, ciPending := "", 0, 0, 0
			if st == "open" && pr.Head.SHA != "" {
				ciState, ciPassed, ciFailed, ciPending = giteaCIStatus(ctx, api, rp, pr.Head.SHA)
			}
			out = append(out, RemotePR{
				Number:     pr.Number,
				Title:      pr.Title,
				Body:       pr.Body,
				State:      st,
				URL:        pr.HTMLURL,
				HeadBranch: pr.Head.Ref,
				BaseBranch: pr.Base.Ref,
				Author:     pr.User.Login,
				Labels:     labels,
				CreatedAt:  pr.CreatedAt,
				UpdatedAt:  pr.UpdatedAt,
				RepoID:     repo.ID,
				RepoName:   repo.Name,
				CIState:    ciState,
				CIPassed:   ciPassed,
				CIFailed:   ciFailed,
				CIPending:  ciPending,
			})
		}
		if len(batch) < giteaPageSize {
			break
		}
	}
	return out, nil
}

func giteaCIStatus(ctx context.Context, api *giteaAPI, rp, sha string) (string, int, int, int) {
	var res struct {
		State    string `json:"state"`
		Statuses []struct {
			Status string `json:"status"`
		} `json:"statuses"`
	}
	path := fmt.Sprintf("%s/commits/%s/status", rp, url.PathEscape(sha))
	if err := api.do(ctx, http.MethodGet, path, nil, &res); err != nil {
		return "", 0, 0, 0
	}
	states := make([]string, 0, len(res.Statuses))
	for _, status := range res.Statuses {
		states = append(states, status.Status)
	}
	state, passed, failed, pending := summarizeCheckStates(states)
	if len(states) == 0 && strings.TrimSpace(res.State) != "" {
		state, passed, failed, pending = summarizeCheckStates([]string{res.State})
	}
	return state, passed, failed, pending
}

func (gitea) MergePR(ctx context.Context, repo domain.Repo, number int64, method string) (string, error) {
	api, err := giteaConfig()
	if err != nil {
		return "", err
	}
	rp, err := giteaRepoPath(repo)
	if err != nil {
		return "", err
	}
	do := "squash"
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "merge":
		do = "merge"
	case "rebase":
		do = "rebase"
	}
	if err := api.do(ctx, http.MethodPost, fmt.Sprintf("%s/pulls/%d/merge", rp, number), map[string]any{"Do": do}, nil); err != nil {
		return "", err
	}
	return "merged", nil
}

func (gitea) ClosePR(ctx context.Context, repo domain.Repo, number int64) error {
	api, err := giteaConfig()
	if err != nil {
		return err
	}
	rp, err := giteaRepoPath(repo)
	if err != nil {
		return err
	}
	return api.do(ctx, http.MethodPatch, fmt.Sprintf("%s/pulls/%d", rp, number), map[string]any{"state": "closed"}, nil)
}

// giteaPRFromURL recognizes PR URLs on the configured Gitea instance.
func giteaPRFromURL(prURL string) (*giteaAPI, string, string, int64, bool) {
	api, err := giteaConfig()
	if err != nil {
		return nil, "", "", 0, false
	}
	u, err := url.Parse(strings.TrimSpace(prURL))
	if err != nil || u.Host == "" {
		return nil, "", "", 0, false
	}
	root, err := url.Parse(api.root)
	if err != nil || !strings.EqualFold(u.Host, root.Host) {
		return nil, "", "", 0, false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] != "pulls" || i < 2 {
			continue
		}
		number, err := strconv.ParseInt(segs[i+1], 10, 64)
		if err != nil || number <= 0 {
			return nil, "", "", 0, false
		}
		return api, segs[i-2], segs[i-1], number, true
	}
	return nil, "", "", 0, false
}

func (g *giteaAPI) pr(ctx context.Context, owner, repo string, number int64) (*giteaPR, error) {
	var pr giteaPR
	rp := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	if err := g.do(ctx, http.MethodGet, fmt.Sprintf("%s/pulls/%d", rp, number), nil, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func giteaPRMerged(pr *giteaPR) bool {
	if pr.Merged {
		return true
	}
	if pr.MergedAt == nil || strings.TrimSpace(*pr.MergedAt) == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, *pr.MergedAt)
	return err == nil && t.Year() > 1
}

func giteaPRState(pr *giteaPR) string {
	if giteaPRMerged(pr) {
		return "merged"
	}
	if strings.EqualFold(strings.TrimSpace(pr.State), "closed") {
		return "closed"
	}
	return "open"
}

func giteaCheckPRStatus(ctx context.Context, prURL string) (string, string, error) {
	api, owner, repo, number, ok := giteaPRFromURL(prURL)
	if !ok {
		return "", "", fmt.Errorf("not a configured gitea pull request URL")
	}
	pr, err := api.pr(ctx, owner, repo, number)
	if err != nil {
		return "", "", err
	}
	return giteaPRState(pr), pr.Title, nil
}

func giteaPRDetails(ctx context.Context, prURL string) (*PRDetailInfo, error) {
	api, owner, repo, number, ok := giteaPRFromURL(prURL)
	if !ok {
		return nil, fmt.Errorf("not a configured gitea pull request URL")
	}
	pr, err := api.pr(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	rp := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	ciState, ciPassed, ciFailed, ciPending := "", 0, 0, 0
	if pr.Head.SHA != "" {
		ciState, ciPassed, ciFailed, ciPending = giteaCIStatus(ctx, api, rp, pr.Head.SHA)
	}
	detail := &PRDetailInfo{
		Number:        pr.Number,
		Title:         pr.Title,
		State:         giteaPRState(pr),
		URL:           pr.HTMLURL,
		HeadRefName:   pr.Head.Ref,
		HeadSHA:       pr.Head.SHA,
		BaseRefName:   pr.Base.Ref,
		Author:        pr.User.Login,
		Comments:      []PRComment{},
		LatestReviews: []PRReview{},
		CIState:       ciState,
		CIPassed:      ciPassed,
		CIFailed:      ciFailed,
		CIPending:     ciPending,
	}
	for page := 1; ; page++ {
		var batch []giteaComment
		path := fmt.Sprintf("%s/issues/%d/comments?limit=%d&page=%d", rp, number, giteaPageSize, page)
		if err := api.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, c := range batch {
			created, _ := time.Parse(time.RFC3339, c.CreatedAt)
			detail.Comments = append(detail.Comments, PRComment{
				ID:        strconv.FormatInt(c.ID, 10),
				Author:    c.User.Login,
				Body:      c.Body,
				CreatedAt: created,
			})
		}
		if len(batch) < giteaPageSize {
			break
		}
	}
	for page := 1; ; page++ {
		var batch []giteaReview
		path := fmt.Sprintf("%s/pulls/%d/reviews?limit=%d&page=%d", rp, number, giteaPageSize, page)
		if err := api.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			// Older Gitea versions may not expose reviews; keep comments useful.
			break
		}
		for _, r := range batch {
			submitted, _ := time.Parse(time.RFC3339, r.SubmittedAt)
			detail.LatestReviews = append(detail.LatestReviews, PRReview{
				ID:          strconv.FormatInt(r.ID, 10),
				Author:      r.User.Login,
				Body:        r.Body,
				State:       strings.ToLower(strings.TrimSpace(r.State)),
				SubmittedAt: submitted,
			})
		}
		if len(batch) < giteaPageSize {
			break
		}
	}
	return detail, nil
}
