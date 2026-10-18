// Package provider adapts git hosting platforms (issues, pull requests).
package provider

import (
	"context"
	"errors"
	"strconv"

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
}

// Caps describes what a provider can do; missing features fall back to local handling.
type Caps struct {
	Issues bool
	PRs    bool
}

// Provider is implemented per platform.
type Provider interface {
	Caps() Caps
	ListOpenIssues(ctx context.Context, repo domain.Repo) ([]RemoteIssue, error)
	CreateIssue(ctx context.Context, repo domain.Repo, title, body string) (*RemoteIssue, error)
	Comment(ctx context.Context, repo domain.Repo, number int64, body string) error
	CreatePR(ctx context.Context, repo domain.Repo, head, base, title, body string) (url string, err error)
}

// For returns the provider implementation for a repo.
func For(p domain.Provider) Provider {
	switch p {
	case domain.ProviderGitHub:
		return github{}
	default:
		// gitlab / gitea adapters land in M2; until then they behave like plain git.
		return plain{}
	}
}

// IssueKey formats the display key for a remote issue.
func IssueKey(repo domain.Repo, number int64) string {
	name := repo.OwnerRepo
	if name == "" {
		name = repo.Name
	}
	return name + "#" + strconv.FormatInt(number, 10)
}

type plain struct{}

func (plain) Caps() Caps { return Caps{} }
func (plain) ListOpenIssues(context.Context, domain.Repo) ([]RemoteIssue, error) {
	return nil, nil
}
func (plain) CreateIssue(context.Context, domain.Repo, string, string) (*RemoteIssue, error) {
	return nil, ErrUnsupported
}
func (plain) Comment(context.Context, domain.Repo, int64, string) error { return ErrUnsupported }
func (plain) CreatePR(context.Context, domain.Repo, string, string, string, string) (string, error) {
	return "", ErrUnsupported
}
