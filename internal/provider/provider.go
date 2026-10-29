// Package provider adapts git hosting platforms (issues, pull requests).
package provider

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
)

var ErrUnsupported = errors.New("operation not supported by this provider")

// RemoteIssue is an issue fetched from a platform.
type RemoteIssue struct {
	Number int64
	Title  string
	Body   string
	Labels []string
	URL    string
	Author string
}

// RemoteIssueComment is a comment/reply attached to a remote issue.
type RemoteIssueComment struct {
	ID          string    `json:"id"`
	IssueNumber int64     `json:"issueNumber"`
	Author      string    `json:"author"`
	Body        string    `json:"body"`
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"createdAt"`
}

// RemotePR is a pull request fetched from a platform.
type RemotePR struct {
	Number     int64    `json:"number"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	State      string   `json:"state"` // "open", "merged", "closed"
	URL        string   `json:"url"`
	HeadBranch string   `json:"headBranch"`
	BaseBranch string   `json:"baseBranch"`
	Author     string   `json:"author"`
	Labels     []string `json:"labels"`
	CreatedAt  string   `json:"createdAt"`
	UpdatedAt  string   `json:"updatedAt"`
	MergedAt   string   `json:"mergedAt,omitempty"`
	Mine       bool     `json:"mine,omitempty"`
	RepoID     int64    `json:"repoId"`
	RepoName   string   `json:"repoName"`
	// CI rollup for the PR head. Empty means the provider did not report checks.
	CIState   string `json:"ciState,omitempty"` // success | failure | pending | unknown
	CIPassed  int    `json:"ciPassed,omitempty"`
	CIFailed  int    `json:"ciFailed,omitempty"`
	CIPending int    `json:"ciPending,omitempty"`
}

type PRComment struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type PRReview struct {
	ID          string    `json:"id"`
	Author      string    `json:"author"`
	Body        string    `json:"body"`
	State       string    `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED, etc.
	SubmittedAt time.Time `json:"submittedAt"`
}

type PRDetailInfo struct {
	Number        int64       `json:"number"`
	Title         string      `json:"title"`
	State         string      `json:"state"` // "open", "merged", "closed"
	URL           string      `json:"url"`
	HeadRefName   string      `json:"headRefName"`
	HeadSHA       string      `json:"headSha"`
	BaseRefName   string      `json:"baseRefName"`
	Author        string      `json:"author"`
	Comments      []PRComment `json:"comments"`
	LatestReviews []PRReview  `json:"latestReviews"`
	CIState       string      `json:"ciState,omitempty"`
	CIPassed      int         `json:"ciPassed,omitempty"`
	CIFailed      int         `json:"ciFailed,omitempty"`
	CIPending     int         `json:"ciPending,omitempty"`
}

// Caps describes what a provider can do; missing features fall back to local handling.
type Caps struct {
	Issues bool
	PRs    bool
}

// Provider is implemented per platform.
type Provider interface {
	Caps() Caps
	CurrentUser(ctx context.Context) (string, error)
	ListOpenIssues(ctx context.Context, repo domain.Repo) ([]RemoteIssue, error)
	ListRepoIssueComments(ctx context.Context, repo domain.Repo) ([]RemoteIssueComment, error)
	CreateIssue(ctx context.Context, repo domain.Repo, title, body string) (*RemoteIssue, error)
	AddLabels(ctx context.Context, repo domain.Repo, number int64, labels []string) error
	Comment(ctx context.Context, repo domain.Repo, number int64, body string) error
	CreatePR(ctx context.Context, repo domain.Repo, head, base, title, body string) (url string, err error)
	ListPRs(ctx context.Context, repo domain.Repo, state string) ([]RemotePR, error)
	MergePR(ctx context.Context, repo domain.Repo, number int64, method string) (string, error)
	ClosePR(ctx context.Context, repo domain.Repo, number int64) error
}

// For returns the provider implementation for a repo.
func For(p domain.Provider) Provider {
	switch p {
	case domain.ProviderGitHub:
		return github{}
	case domain.ProviderGitea:
		return gitea{}
	case domain.ProviderGitLab:
		return gitlab{}
	default:
		return plain{}
	}
}

// IssueKey formats the display key for a remote issue.
// summarizeCheckStates folds provider-specific CI states into one rollup.
// Unknown/neutral values are handled conservatively: only explicit failures or
// pending states override success.
func summarizeCheckStates(states []string) (state string, passed, failed, pending int) {
	seen := false
	for _, raw := range states {
		st := strings.ToLower(strings.TrimSpace(raw))
		switch st {
		case "success", "successful", "passed", "neutral", "skipped":
			passed++
			seen = true
		case "failure", "failed", "error", "timed_out", "cancelled", "canceled", "action_required", "stale":
			failed++
			seen = true
		case "pending", "queued", "in_progress", "expected", "waiting", "requested":
			pending++
			seen = true
		}
	}
	switch {
	case failed > 0:
		return "failure", passed, failed, pending
	case pending > 0:
		return "pending", passed, failed, pending
	case seen:
		return "success", passed, failed, pending
	default:
		return "unknown", 0, 0, 0
	}
}

func IssueKey(repo domain.Repo, number int64) string {
	name := repo.OwnerRepo
	if name == "" {
		name = repo.Name
	}
	return name + "#" + strconv.FormatInt(number, 10)
}

type plain struct{}

func (plain) Caps() Caps                                  { return Caps{} }
func (plain) CurrentUser(context.Context) (string, error) { return "", nil }
func (plain) ListOpenIssues(context.Context, domain.Repo) ([]RemoteIssue, error) {
	return nil, nil
}
func (plain) ListRepoIssueComments(context.Context, domain.Repo) ([]RemoteIssueComment, error) {
	return nil, nil
}
func (plain) CreateIssue(context.Context, domain.Repo, string, string) (*RemoteIssue, error) {
	return nil, ErrUnsupported
}
func (plain) AddLabels(context.Context, domain.Repo, int64, []string) error { return nil }
func (plain) Comment(context.Context, domain.Repo, int64, string) error     { return ErrUnsupported }
func (plain) CreatePR(context.Context, domain.Repo, string, string, string, string) (string, error) {
	return "", ErrUnsupported
}
func (plain) ListPRs(context.Context, domain.Repo, string) ([]RemotePR, error) {
	return nil, nil
}
func (plain) MergePR(context.Context, domain.Repo, int64, string) (string, error) {
	return "", ErrUnsupported
}
func (plain) ClosePR(context.Context, domain.Repo, int64) error {
	return ErrUnsupported
}
