package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
)

// IsDevelopableIssue reports whether a synced remote issue looks like an
// actionable bug or feature request based on its labels/title/body.
func IsDevelopableIssue(issue domain.Issue, keywords []string) bool {
	if len(keywords) == 0 {
		keywords = domain.DefaultAutoDevelopLabels()
	}
	haystack := issue.Title + "\n" + issue.Body
	for _, raw := range keywords {
		keyword := strings.TrimSpace(raw)
		if keyword == "" {
			continue
		}
		for _, label := range issue.Labels {
			if strings.EqualFold(strings.TrimSpace(label), keyword) {
				return true
			}
		}
		if keywordInText(haystack, keyword) {
			return true
		}
	}
	return false
}

func keywordInText(text, keyword string) bool {
	text = strings.ToLower(text)
	keyword = strings.ToLower(keyword)
	if keyword == "" {
		return false
	}
	if !isASCII(keyword) {
		return strings.Contains(text, keyword)
	}
	for offset := 0; ; {
		idx := strings.Index(text[offset:], keyword)
		if idx < 0 {
			return false
		}
		start := offset + idx
		end := start + len(keyword)
		if (start == 0 || !isWordByte(text[start-1])) && (end == len(text) || !isWordByte(text[end])) {
			return true
		}
		offset = start + 1
	}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_'
}

// AutoDevelopPrompt instructs the agent to assess the issue and prepare a
// reviewable change without merging or opening the PR itself.
func AutoDevelopPrompt(issue *domain.Issue) string {
	return fmt.Sprintf(`This issue (%s) was synced from a remote tracker and looks like a bug or feature request.

First verify it is genuinely actionable. If it is unclear, incomplete, not a real bug/feature, or cannot be implemented safely in this repository, DO NOT modify code. Explain the reason and what information is missing in your final summary.

If it is actionable:
1. Inspect the repository structure and follow existing conventions.
2. Implement the smallest correct fix or feature, including tests where the project has a test setup.
3. Run the relevant tests/build and report the result.
4. Do NOT merge, do NOT push the target branch, and do NOT create the PR yourself. Leave the changes committed on the current branch worktree; the harness will open a PR for an administrator to review, merge, or discard.
5. Do NOT close or change the status of the remote issue.

Issue title: %s`, issue.Key, issue.Title)
}

// DefaultAgentName returns the preferred configured agent for automatic runs.
func (r *Runner) DefaultAgentName() string {
	agents := r.cfg.AgentsSnapshot()
	if _, ok := agents["opencode"]; ok {
		return "opencode"
	}
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// autoSubmitPR submits an automatic pre-development run as a PR. It never
// merges: the administrator decides whether to merge or close the PR.
func (r *Runner) autoSubmitPR(runID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil || run.PRURL != "" || run.Status != domain.RunCompleted {
		return
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return
	}

	hasChanges := false
	for _, repo := range proj.Repos {
		wt := filepath.Join(run.Workdir, repo.Name)
		files, err := gitx.Changes(ctx, wt, repo.DefaultBranch)
		if err == nil && len(files) > 0 {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		note := "🤖 自动预开发执行完成，但未检测到代码改动（可能不是可实施的需求/Bug，或信息不足）。未创建 PR，请人工确认。"
		_, _ = r.st.AddComment(ctx, issue.ID, "system", note)
		r.ExportIssueMarkdown(ctx, issue.ID)
		return
	}

	results, err := r.Submit(ctx, runID, SubmitRequest{
		Mode:   "pr",
		Remark: "由自动预开发生成，尚未合并；请管理员审核后决定合并或关闭。",
	})
	if err != nil {
		_, _ = r.st.AddComment(ctx, issue.ID, "system", "🤖 自动预开发提交 PR 失败: "+err.Error())
		return
	}
	var failures []string
	for _, res := range results {
		if res.Error != "" {
			failures = append(failures, fmt.Sprintf("%s: %s", res.RepoName, res.Error))
		}
	}
	if len(failures) > 0 {
		_, _ = r.st.AddComment(ctx, issue.ID, "system", "🤖 自动预开发部分仓库提交失败:\n```text\n"+strings.Join(failures, "\n")+"\n```")
	}
}
