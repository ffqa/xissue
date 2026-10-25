package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
)

func sanitizeFileName(s string) string {
	s = strings.TrimSpace(s)
	repl := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"..", "_",
		" ", "_",
	)
	res := repl.Replace(s)
	if res == "" {
		return "default"
	}
	return res
}

// RunArchiveDir returns the path to the text archive directory for a run:
// ~/.xissue/issue/{workspace}/{task}/{runId}
func (r *Runner) RunArchiveDir(ctx context.Context, runID int64) (string, error) {
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return "", err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return "", err
	}
	workspace := sanitizeFileName(proj.Name)
	if workspace == "" {
		workspace = fmt.Sprintf("project-%d", proj.ID)
	}
	task := sanitizeFileName(issue.Key)
	if task == "" {
		task = fmt.Sprintf("task-%d", issue.ID)
	}
	runName := fmt.Sprintf("run-%d", run.ID)
	return filepath.Join(r.cfg.DataDir, "issue", workspace, task, runName), nil
}

// ArchiveRun exports text analysis (analysis.md) and plain-text event logs (events.log)
// to ~/.xissue/issue/{workspace}/{task}/{runId}/ so that full debugging & review records
// are kept in lightweight text files even after the worktree is deleted.
func (r *Runner) ArchiveRun(ctx context.Context, runID int64) (string, error) {
	run, proj, err := r.runAndProject(ctx, runID)
	if err != nil {
		return "", err
	}
	issue, err := r.st.GetIssue(ctx, run.IssueID)
	if err != nil {
		return "", err
	}

	workspace := sanitizeFileName(proj.Name)
	if workspace == "" {
		workspace = fmt.Sprintf("project-%d", proj.ID)
	}
	task := sanitizeFileName(issue.Key)
	if task == "" {
		task = fmt.Sprintf("task-%d", issue.ID)
	}
	taskDir := filepath.Join(r.cfg.DataDir, "issue", workspace, task)
	runDir := filepath.Join(taskDir, fmt.Sprintf("run-%d", run.ID))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", err
	}

	events, err := r.st.ListEvents(ctx, runID, 0)
	if err != nil {
		return "", err
	}
	analysisFile := filepath.Join(runDir, "analysis.md")
	eventsFile := filepath.Join(runDir, "events.log")
	if len(events) == 0 {
		// Event rows may already have been pruned by retention. Keep the richer
		// archive created earlier instead of overwriting it with empty files.
		if _, aerr := os.Stat(analysisFile); aerr == nil {
			if _, eerr := os.Stat(eventsFile); eerr == nil {
				r.updateTaskArchiveIndex(ctx, taskDir, issue, proj)
				return runDir, nil
			}
		}
	}

	// 1. Build analysis.md
	var b strings.Builder
	fmt.Fprintf(&b, "# [%s] 任务执行与分析记录 (Run #%d)\n\n", issue.Key, run.ID)
	fmt.Fprintf(&b, "- **工作空间 (Workspace)**: %s\n", proj.Name)
	fmt.Fprintf(&b, "- **任务 (Task)**: [%s] %s\n", issue.Key, issue.Title)
	fmt.Fprintf(&b, "- **执行状态**: `%s`\n", run.Status)
	fmt.Fprintf(&b, "- **Agent / 模型**: `%s`", run.Agent)
	if run.Model != "" {
		fmt.Fprintf(&b, " (Model: `%s`)", run.Model)
	}
	b.WriteString("\n")
	if run.Branch != "" {
		fmt.Fprintf(&b, "- **分支**: `%s`", run.Branch)
		if run.TargetBranch != "" {
			fmt.Fprintf(&b, " -> 目标基准 `%s`", run.TargetBranch)
		}
		b.WriteString("\n")
	}
	if run.PRURL != "" {
		fmt.Fprintf(&b, "- **PR 链接**: %s (State: `%s`)\n", run.PRURL, run.PRState)
	}
	fmt.Fprintf(&b, "- **创建时间**: %s\n", run.CreatedAt.Format(time.RFC3339))
	if run.StartedAt != nil {
		fmt.Fprintf(&b, "- **开始时间**: %s\n", run.StartedAt.Format(time.RFC3339))
	}
	if run.EndedAt != nil {
		fmt.Fprintf(&b, "- **结束时间**: %s\n", run.EndedAt.Format(time.RFC3339))
		if run.StartedAt != nil {
			fmt.Fprintf(&b, "- **耗时**: %s\n", run.EndedAt.Sub(*run.StartedAt).Round(time.Second))
		}
	}
	b.WriteString("\n---\n\n")

	if run.Error != "" {
		fmt.Fprintf(&b, "## ⚠️ 执行错误信息\n\n```text\n%s\n```\n\n", run.Error)
	}

	if run.ChangeRecord != "" {
		fmt.Fprintf(&b, "## 📋 标准改动记录\n\n```text\n%s\n```\n\n", run.ChangeRecord)
	}

	// Extract thoughts & assistant summaries from events
	var thoughts []string
	var assistantMsgs []string
	var toolCalls []string
	for _, ev := range events {
		var m map[string]any
		_ = json.Unmarshal(ev.Data, &m)
		switch ev.Type {
		case "thought":
			if text, ok := m["text"].(string); ok && strings.TrimSpace(text) != "" {
				thoughts = append(thoughts, strings.TrimSpace(text))
			}
		case "message":
			role, _ := m["role"].(string)
			text, _ := m["text"].(string)
			if role == "assistant" && strings.TrimSpace(text) != "" {
				assistantMsgs = append(assistantMsgs, strings.TrimSpace(text))
			}
		case "tool_call":
			title, _ := m["title"].(string)
			kind, _ := m["kind"].(string)
			if title != "" {
				toolCalls = append(toolCalls, fmt.Sprintf("- [%s] %s", kind, title))
			}
		}
	}

	if len(thoughts) > 0 {
		b.WriteString("## 🧠 Agent 思考过程 (Thought Summary)\n\n")
		for i, t := range thoughts {
			fmt.Fprintf(&b, "### 思考片段 %d\n\n%s\n\n", i+1, t)
		}
	}

	if len(toolCalls) > 0 {
		b.WriteString("## 🛠️ 调用的工具清单\n\n")
		for _, tc := range toolCalls {
			b.WriteString(tc + "\n")
		}
		b.WriteString("\n")
	}

	if len(assistantMsgs) > 0 {
		b.WriteString("## 💬 Agent 最终回复与分析\n\n")
		for _, msg := range assistantMsgs {
			b.WriteString(msg + "\n\n")
		}
	}

	// Write analysis.md atomically
	if err := writeAtomic(analysisFile, []byte(b.String())); err != nil {
		return "", err
	}

	// 2. Build events.log (Plain text event stream)
	var el strings.Builder
	fmt.Fprintf(&el, "# [%s] Event Log for Run #%d\n", issue.Key, run.ID)
	fmt.Fprintf(&el, "# Generated at: %s\n\n", time.Now().Format(time.RFC3339))
	for _, ev := range events {
		var m map[string]any
		_ = json.Unmarshal(ev.Data, &m)
		line := formatEventLine(ev, m)
		el.WriteString(line + "\n")
	}
	if err := writeAtomic(eventsFile, []byte(el.String())); err != nil {
		return "", err
	}

	// 3. Update task README.md index
	r.updateTaskArchiveIndex(ctx, taskDir, issue, proj)

	return runDir, nil
}

func formatEventLine(ev domain.RunEvent, m map[string]any) string {
	ts := ev.CreatedAt.Format("2006-01-02 15:04:05")
	seq := ev.Seq
	switch ev.Type {
	case "user":
		txt, _ := m["text"].(string)
		return fmt.Sprintf("[%s] [#%d] [USER] %s", ts, seq, txt)
	case "thought":
		txt, _ := m["text"].(string)
		return fmt.Sprintf("[%s] [#%d] [THOUGHT] %s", ts, seq, strings.ReplaceAll(txt, "\n", " "))
	case "message":
		role, _ := m["role"].(string)
		txt, _ := m["text"].(string)
		return fmt.Sprintf("[%s] [#%d] [%s] %s", ts, seq, strings.ToUpper(role), strings.ReplaceAll(txt, "\n", " "))
	case "tool_call":
		title, _ := m["title"].(string)
		kind, _ := m["kind"].(string)
		raw, _ := json.Marshal(m["input"])
		return fmt.Sprintf("[%s] [#%d] [TOOL_CALL:%s] %s args=%s", ts, seq, kind, title, string(raw))
	case "tool_call_update":
		id, _ := m["toolCallId"].(string)
		st, _ := m["status"].(string)
		return fmt.Sprintf("[%s] [#%d] [TOOL_UPDATE] id=%s status=%s", ts, seq, id, st)
	case "status":
		st, _ := m["status"].(string)
		return fmt.Sprintf("[%s] [#%d] [STATUS] %s", ts, seq, st)
	case "system":
		txt, _ := m["text"].(string)
		return fmt.Sprintf("[%s] [#%d] [SYSTEM] %s", ts, seq, txt)
	default:
		raw, _ := json.Marshal(m)
		return fmt.Sprintf("[%s] [#%d] [%s] %s", ts, seq, strings.ToUpper(ev.Type), string(raw))
	}
}

func (r *Runner) updateTaskArchiveIndex(ctx context.Context, taskDir string, issue *domain.Issue, proj *domain.Project) {
	runs, err := r.st.ListRuns(ctx, issue.ID)
	if err != nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# 任务 [%s] %s\n\n", issue.Key, issue.Title)
	fmt.Fprintf(&b, "- **工作空间**: %s\n", proj.Name)
	fmt.Fprintf(&b, "- **当前状态**: `%s`\n", issue.Status)
	fmt.Fprintf(&b, "- **更新时间**: %s\n\n", time.Now().Format(time.RFC3339))
	b.WriteString("## 执行与复盘历史 (Run History)\n\n")
	b.WriteString("| Run ID | Agent | Model | 状态 | 创建时间 | 文本分析 | 完整事件流 |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, run := range runs {
		relDir := fmt.Sprintf("./run-%d", run.ID)
		fmt.Fprintf(&b, "| #%d | `%s` | `%s` | `%s` | %s | [analysis.md](%s/analysis.md) | [events.log](%s/events.log) |\n",
			run.ID, run.Agent, run.Model, run.Status, run.CreatedAt.Format("01-02 15:04"), relDir, relDir)
	}
	b.WriteString("\n> 说明：Run 完成后归档至此目录；`failed`/`cancelled`/`discarded` 的工作区会按保留策略清理，`completed`（待审核）与 `submitted`（可能部分推送失败）工作区保留以免丢失未提交的 diff。\n")
	readmePath := filepath.Join(taskDir, "README.md")
	_ = writeAtomic(readmePath, []byte(b.String()))
}

func writeAtomic(target string, data []byte) error {
	tmp := fmt.Sprintf("%s.tmp.%d", target, time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
