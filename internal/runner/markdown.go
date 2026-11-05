package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
)

// ExportIssueMarkdown writes a complete Markdown archive of an issue (body, runs, comments)
// to <issuesDir>/project-<id>/<key>.md so local issues are easy to grep and back up.
func (r *Runner) ExportIssueMarkdown(ctx context.Context, issueID int64) {
	if !r.cfg.ExportMarkdownEnabled() {
		return
	}
	issue, err := r.st.GetIssue(ctx, issueID)
	if err != nil {
		return
	}
	runs, _ := r.st.ListRuns(ctx, issueID)
	comments, _ := r.st.ListComments(ctx, issueID)

	var b strings.Builder
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b, "id: %d\n", issue.ID)
	fmt.Fprintf(&b, "key: %q\n", issue.Key)
	fmt.Fprintf(&b, "source: %s\n", issue.Source)
	fmt.Fprintf(&b, "status: %s\n", issue.Status)
	if issue.RemoteURL != "" {
		fmt.Fprintf(&b, "remote_url: %q\n", issue.RemoteURL)
	}
	fmt.Fprintf(&b, "created_at: %s\n", issue.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "updated_at: %s\n", issue.UpdatedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "---\n\n")
	fmt.Fprintf(&b, "# [%s] %s\n\n", issue.Key, issue.Title)
	if strings.TrimSpace(issue.Body) != "" {
		b.WriteString(issue.Body)
		b.WriteString("\n\n")
	}

	if len(runs) > 0 {
		b.WriteString("## Runs\n\n")
		for _, run := range runs {
			fmt.Fprintf(&b, "- **Run #%d** (`%s`, `%s`, branch `%s`) - %s\n",
				run.ID, run.Agent, run.Status, run.Branch, run.CreatedAt.Format(time.RFC3339))
			if run.Error != "" {
				fmt.Fprintf(&b, "  - Error: `%s`\n", run.Error)
			}
		}
		b.WriteString("\n")
	}

	if len(comments) > 0 {
		b.WriteString("## Comments & Review Log\n\n")
		for _, c := range comments {
			fmt.Fprintf(&b, "### %s (%s)\n\n%s\n\n", c.Author, c.CreatedAt.Format(time.RFC3339), c.Body)
		}
	}

	dir := filepath.Join(r.cfg.IssuesDir(), fmt.Sprintf("project-%d", issue.ProjectID))
	_ = os.MkdirAll(dir, 0o755)
	safeKey := strings.NewReplacer("/", "_", "#", "-", "..", "_").Replace(issue.Key)
	target := filepath.Join(dir, safeKey+".md")
	tmp := fmt.Sprintf("%s.tmp.%d", target, time.Now().UnixNano())
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err == nil {
		_ = os.Rename(tmp, target)
	}
}

// workspaceDescription drops filesystem paths from project descriptions.
// Local imports often store the original directory in Description; feeding that
// to an agent makes it operate on the user's main checkout instead of the
// isolated run worktree.
func workspaceDescription(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "~/") {
			continue
		}
		if len(trimmed) >= 3 && trimmed[1] == ':' && (trimmed[2] == '\\' || trimmed[2] == '/') {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func buildWorkspaceAGENTS(proj *domain.Project, issue *domain.Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Project: %s\n\n", proj.Name)
	if desc := workspaceDescription(proj.Description); desc != "" {
		b.WriteString("## Project Description\n\n")
		b.WriteString(desc + "\n\n")
	}
	b.WriteString("## Repositories in this Workspace\n\n")
	repos := reposForIssue(proj, issue)
	for _, repo := range repos {
		fmt.Fprintf(&b, "- `./%s/` (default branch `%s`", repo.Name, repo.DefaultBranch)
		if repo.TestCommand != "" {
			fmt.Fprintf(&b, ", test command: `%s`", repo.TestCommand)
		}
		b.WriteString(")\n")
	}
	b.WriteString("\nYour current working directory is the workspace root. Work only inside the repository subdirectories listed above (for example `./ide/`). The project description is free-form text, not a filesystem path; do not `cd` to a path found there.\n")
	fmt.Fprintf(&b, "\n## Active Issue: [%s] %s\n\n", issue.Key, issue.Title)
	if issue.RepoID != nil && len(repos) == 1 {
		fmt.Fprintf(&b, "This issue is scoped to `./%s/`; do not modify other repositories.\n\n", repos[0].Name)
	}
	if strings.TrimSpace(issue.Body) != "" {
		b.WriteString(issue.Body + "\n\n")
	}
	writeWorkflowGuidelines(&b, proj.Workflow)
	b.WriteString("## Guidelines & Safety Rules\n")
	b.WriteString("- Make changes directly inside the repository subdirectories above.\n")
	b.WriteString("- Do NOT run `git push`; xIssue handles committing, pushing, and opening PRs after human review.\n")
	b.WriteString("- Do NOT run `pkill xissue`, `killall xissue`, `make stop`, `make restart`, `make deploy`, or `install-service.sh`, and do NOT bind to port 7789 — xIssue itself is supervising your process on port 7789.\n")
	b.WriteString("- Run the configured test command(s) before finishing and summarize your changes clearly.\n")
	return b.String()
}

// writeWorkflowGuidelines appends the opt-in engineering workflow to AGENTS.md.
// It is intentionally compact: repo-local instructions always win, and the
// section only appears when the project enabled at least one switch.
func writeWorkflowGuidelines(b *strings.Builder, w domain.ProjectWorkflow) {
	if !w.Enabled() {
		return
	}
	b.WriteString("\n## Engineering Workflow (project setting)\n\n")
	b.WriteString("Repo-local AGENTS.md, README and skills take precedence over this section.\n")
	if w.DDD {
		b.WriteString("\n### Domain-Driven Design\n")
		b.WriteString("- Before changing code: state the bounded context, the domain terms you will use (ubiquitous language), and the acceptance checks; keep code names aligned with that glossary.\n")
		b.WriteString("- Validate data where it enters, keep business rules in pure functions, and make illegal states unrepresentable.\n")
	}
	if w.TDD {
		b.WriteString("\n### TDD\n")
		b.WriteString("- Write or change a failing test first; run it and confirm it fails for the expected reason.\n")
		b.WriteString("- Then make the smallest change that passes; test observable behavior, not private implementation.\n")
	}
	if w.UBMode {
		b.WriteString("\n### ub-mode (unclebob/ub-mode)\n")
		b.WriteString("- First make it work: smallest edit that solves the problem, one decision in one place, get the data shape right, subtract complexity before adding.\n")
		b.WriteString("- Then make it right: reproduce and fix root causes, work in small verifiable units, fix CRAP/duplication/dead code, pin behavior before refactoring.\n")
		b.WriteString("- Keep a short log of actions and reasons, and prefer small ordered commits. If the project already provides crapper/dryer/mutator, use them; do not install new tooling.\n")
	}
	if w.STE {
		b.WriteString("\n### STE summary\n")
		b.WriteString("- Write the final summary and PR description in Simplified Technical English: short sentences, active voice, one idea per sentence, no marketing adjectives. State what changed, why, and how it was verified.\n")
	}
	if w.TestLoop {
		iterations := w.MaxIterations
		if iterations <= 0 {
			iterations = 2
		}
		b.WriteString("\n### Test loop\n")
		fmt.Fprintf(b, "- Run the repository test command before finishing. On failure, fix and run again, at most %d retries. If it still fails, stop and report the remaining failure with the exact command and output.\n", iterations)
	}
}
