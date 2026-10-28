package gitx

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MergedBranch is a remote branch whose tip is already merged into the base
// branch. Author identity is used to show only the operator's own branches.
type MergedBranch struct {
	Name    string    `json:"name"`
	SHA     string    `json:"sha"`
	Author  string    `json:"author"`
	Email   string    `json:"email"`
	Subject string    `json:"subject"`
	Date    time.Time `json:"date"`
}

// GitIdentity resolves the identity configured for a local repository, then
// falls back to the global Git config.
func GitIdentity(ctx context.Context, localRepo string) (name, email string, err error) {
	if strings.TrimSpace(localRepo) != "" && IsLocalDir(localRepo) {
		name, _ = Run(ctx, localRepo, "config", "--get", "user.name")
		email, _ = Run(ctx, localRepo, "config", "--get", "user.email")
	}
	if name == "" {
		name, _ = Run(ctx, "", "config", "--global", "--get", "user.name")
	}
	if email == "" {
		email, _ = Run(ctx, "", "config", "--global", "--get", "user.email")
	}
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	if name == "" && email == "" {
		return "", "", fmt.Errorf("git user.name/user.email is not configured")
	}
	return name, email, nil
}

// ListMergedBranches returns remote branches merged into base and authored by
// name/email. base may be a bare branch name or origin/<name>.
func ListMergedBranches(ctx context.Context, mirror, base, name, email string) ([]MergedBranch, error) {
	base = strings.TrimPrefix(strings.TrimSpace(base), "origin/")
	if base == "" {
		return nil, fmt.Errorf("base branch is required")
	}
	if err := ValidateBranchName(base); err != nil {
		return nil, err
	}
	if _, err := Run(ctx, mirror, "rev-parse", "--verify", "refs/remotes/origin/"+base); err != nil {
		return nil, fmt.Errorf("base branch origin/%s not found", base)
	}
	format := "%(refname:short)%00%(objectname)%00%(authorname)%00%(authoremail)%00%(subject)%00%(authordate:iso-strict)"
	out, err := Run(ctx, mirror, "for-each-ref", "--format="+format, "refs/remotes/origin")
	if err != nil {
		return nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.ToLower(strings.TrimSpace(name))
	var result []MergedBranch
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 6 {
			continue
		}
		ref := strings.TrimPrefix(parts[0], "origin/")
		if ref == "" || ref == "HEAD" || ref == base {
			continue
		}
		author := strings.TrimSpace(parts[2])
		authorEmail := strings.Trim(strings.TrimSpace(parts[3]), "<>")
		if email != "" {
			if !strings.EqualFold(authorEmail, email) {
				continue
			}
		} else if name == "" || !strings.EqualFold(author, name) {
			continue
		}
		if _, err := Run(ctx, mirror, "merge-base", "--is-ancestor", "refs/remotes/origin/"+ref, "refs/remotes/origin/"+base); err != nil {
			continue // not merged
		}
		date, _ := time.Parse(time.RFC3339, strings.TrimSpace(parts[5]))
		result = append(result, MergedBranch{
			Name: ref, SHA: parts[1], Author: author, Email: authorEmail,
			Subject: strings.TrimSpace(parts[4]), Date: date,
		})
	}
	return result, nil
}

// DeleteRemoteBranch deletes a branch on origin and the cached remote ref.
func DeleteRemoteBranch(ctx context.Context, mirror, branch string) error {
	if err := ValidateBranchName(branch); err != nil {
		return err
	}
	if _, err := Run(ctx, mirror, "push", "origin", "--delete", branch); err != nil {
		return err
	}
	_, _ = Run(ctx, mirror, "update-ref", "-d", "refs/remotes/origin/"+branch)
	return nil
}
