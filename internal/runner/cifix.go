package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/provider"
)

const maxCIFixAttempts = 3

// maybeAutoFixCI starts an agent turn on the PR's existing run when CI fails.
// It is opt-in (monitor.autoFixCIFailures) and bounded so a permanently broken
// pipeline cannot spawn fixes forever.
func (r *Runner) maybeAutoFixCI(ctx context.Context, issue *domain.Issue, run *domain.Run, pr *provider.PRDetailInfo) {
	cfg := r.cfg.MonitorSnapshot()
	if !cfg.AutoFixCIFailures || pr == nil || pr.State != "open" {
		return
	}
	if pr.CIState == "success" {
		r.clearCIFix(run.ID)
		return
	}
	if pr.CIState != "failure" || strings.TrimSpace(pr.HeadSHA) == "" {
		return
	}
	if r.issueHasActiveRun(issue.ID) {
		return
	}
	agentName := strings.TrimSpace(run.Agent)
	if agentName == "" {
		r.LogMonitor("WARN", "ci-fix", fmt.Sprintf("skip %s: run has no agent", issue.Key))
		return
	}
	ac, ok := r.cfg.Agent(agentName)
	if !ok {
		r.LogMonitor("WARN", "ci-fix", fmt.Sprintf("skip %s: agent %q is not configured", issue.Key, agentName))
		return
	}
	if strings.TrimSpace(run.RunnerID) == "" {
		if _, err := LookupAgentBinary(ac.Command); err != nil {
			r.LogMonitor("WARN", "ci-fix", fmt.Sprintf("skip %s: agent %q is unavailable: %v", issue.Key, agentName, err))
			return
		}
	}

	r.ciFixMu.Lock()
	prev, seen := r.ciFixAttempts[run.ID]
	if seen && prev.head == pr.HeadSHA {
		r.ciFixMu.Unlock()
		return
	}
	if seen && prev.attempts >= maxCIFixAttempts {
		r.ciFixMu.Unlock()
		return
	}
	next := ciFixAttempt{head: pr.HeadSHA, attempts: 1, at: time.Now()}
	if seen && prev.head != pr.HeadSHA {
		next.attempts = prev.attempts + 1
	}
	r.ciFixAttempts[run.ID] = next
	r.ciFixMu.Unlock()

	prompt := fmt.Sprintf(
		"CI checks failed for PR %s (commit %s). Investigate the failing checks in the current workspace and apply the smallest fix. Run the relevant tests. The runner will commit and push this branch automatically; do not create a new branch or PR and do not merge. This is CI fix attempt %d/%d.",
		pr.URL, pr.HeadSHA, next.attempts, maxCIFixAttempts,
	)
	if err := r.sendMessage(ctx, run.ID, prompt, true); err != nil {
		r.ciFixMu.Lock()
		if cur, ok := r.ciFixAttempts[run.ID]; ok && cur.head == next.head && cur.attempts == next.attempts {
			if seen {
				r.ciFixAttempts[run.ID] = prev
			} else {
				delete(r.ciFixAttempts, run.ID)
			}
		}
		r.ciFixMu.Unlock()
		r.LogMonitor("WARN", "ci-fix", fmt.Sprintf("failed to start CI fix for %s: %v", issue.Key, err))
		return
	}
	title := fmt.Sprintf("[%s] CI 失败，已自动开始修复", issue.Key)
	body := fmt.Sprintf("%s\n%s", issue.Title, pr.URL)
	r.LogMonitor("INFO", "ci-fix", fmt.Sprintf("started %s (attempt %d/%d, commit %s)", issue.Key, next.attempts, maxCIFixAttempts, pr.HeadSHA))
	r.emitEvent(run.ID, issue.ID, "system", map[string]any{
		"text": fmt.Sprintf("CI 失败，自动修复第 %d/%d 次（commit %s）", next.attempts, maxCIFixAttempts, pr.HeadSHA),
	})
	r.pushNotification("ci_fix", title, body, fmt.Sprintf("/issues/%d", issue.ID), "")
}

func (r *Runner) issueHasActiveRun(issueID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.active {
		if a.issueID == issueID {
			return true
		}
	}
	return false
}

func (r *Runner) clearCIFix(runID int64) {
	r.ciFixMu.Lock()
	delete(r.ciFixAttempts, runID)
	r.ciFixMu.Unlock()
}

// autoPushCIFix commits and pushes the repair turn to the existing PR branch.
// Submit is called after the active run slot has been released.
func (r *Runner) autoPushCIFix(runID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		r.LogMonitor("WARN", "ci-fix", fmt.Sprintf("load run %d failed: %v", runID, err))
		return
	}
	results, err := r.Submit(ctx, runID, SubmitRequest{
		Mode:          "push",
		CommitMessage: "fix: address CI failure",
		Remark:        "CI 自动修复",
	})
	if err != nil {
		r.LogMonitor("WARN", "ci-fix", fmt.Sprintf("submit run %d failed: %v", runID, err))
		r.emitEvent(runID, run.IssueID, "system", map[string]any{"text": "CI 自动修复推送失败: " + err.Error()})
		return
	}
	pushed := 0
	var errs []string
	for _, res := range results {
		if res.Error != "" {
			errs = append(errs, fmt.Sprintf("%s: %s", res.RepoName, res.Error))
			continue
		}
		if res.Commit != "" {
			pushed++
		}
	}
	if pushed == 0 {
		msg := "CI 自动修复未产生可推送的改动"
		if len(errs) > 0 {
			msg += ": " + strings.Join(errs, "; ")
		}
		r.LogMonitor("WARN", "ci-fix", fmt.Sprintf("run %d: %s", runID, msg))
		r.emitEvent(runID, run.IssueID, "system", map[string]any{"text": msg})
		return
	}
	msg := fmt.Sprintf("CI 自动修复已推送 %d 个仓库到原分支", pushed)
	if len(errs) > 0 {
		msg += "；部分失败: " + strings.Join(errs, "; ")
	}
	r.LogMonitor("INFO", "ci-fix", fmt.Sprintf("run %d: %s", runID, msg))
	r.emitEvent(runID, run.IssueID, "system", map[string]any{"text": msg})
	r.pushNotification("ci_fix", "CI 自动修复已推送", msg, fmt.Sprintf("/issues/%d", run.IssueID), "")
}
