// Package domain holds core xIssue entities shared across packages.
package domain

import (
	"encoding/json"
	"time"
)

type Provider string

const (
	ProviderGitHub Provider = "github"
	ProviderGitLab Provider = "gitlab"
	ProviderGitea  Provider = "gitea"
	ProviderPlain  Provider = "plain"
)

type Repo struct {
	ID            int64    `json:"id"`
	ProjectID     int64    `json:"projectId"`
	Name          string   `json:"name"`
	Provider      Provider `json:"provider"`
	RemoteURL     string   `json:"remoteUrl"`
	OwnerRepo     string   `json:"ownerRepo"`
	DefaultBranch string   `json:"defaultBranch"`
	TestCommand   string   `json:"testCommand"`
}

type DevMode string

const (
	DevModeSingleMain DevMode = "single_main" // 单一 main 模式（默认直接提到本地 main，可提 PR 到 main）
	DevModeMainDev    DevMode = "main_dev"    // main 和 dev 模式（默认提 PR 到 dev，可提 PR 到 main）
)

type Project struct {
	ID              int64            `json:"id"`
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	DevMode         DevMode          `json:"devMode"`
	TargetBranch    string           `json:"targetBranch"`
	AutoPROnConfirm bool             `json:"autoPrOnConfirm"`
	AutomationRules []AutomationRule `json:"automationRules"`
	Repos           []Repo           `json:"repos"`
	CreatedAt       time.Time        `json:"createdAt"`
	UpdatedAt       time.Time        `json:"updatedAt"`
}

type RuleConditions struct {
	Author      string `json:"author,omitempty"`      // 指定提交人/作者 (如 release-bot, qa-team，留空匹配任意)
	Pattern     string `json:"pattern,omitempty"`     // 正则表达式匹配内容 (如 (?i)status:\s*passed)
	ReviewState string `json:"reviewState,omitempty"` // 审核状态 (approved / changes_requested / commented)
}

type RuleAction struct {
	Type     string `json:"type"`               // merge_pr | set_issue_status | pull_local | notify
	Status   string `json:"status,omitempty"`   // for set_issue_status, e.g. "done"
	Strategy string `json:"strategy,omitempty"` // for merge_pr: "squash" | "merge" | "rebase"
	Message  string `json:"message,omitempty"`  // for notify
}

type AutomationRule struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Enabled    bool           `json:"enabled"`
	Trigger    string         `json:"trigger"` // pr_comment | pr_review
	Conditions RuleConditions `json:"conditions"`
	Actions    []RuleAction   `json:"actions"`
}

type HookStatus struct {
	Name        string `json:"name"`        // e.g. "on-pr-comment"
	Event       string `json:"event"`       // e.g. "pr_comment"
	Path        string `json:"path"`        // 脚本路径
	Exists      bool   `json:"exists"`      // 是否存在
	Executable  bool   `json:"executable"`  // 是否具有执行权限
	Description string `json:"description"` // 说明
}

type PluginInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Events      []string `json:"events"`
	Path        string   `json:"path"`
	Enabled     bool     `json:"enabled"`
}

type IssueStatus string

const (
	IssueBacklog IssueStatus = "backlog"
	IssueTodo    IssueStatus = "todo"
	IssueRunning IssueStatus = "running"
	IssueReview  IssueStatus = "review"
	IssuePROpen  IssueStatus = "pr_open"
	IssueDone    IssueStatus = "done"
	IssueFailed  IssueStatus = "failed"
)

func ValidIssueStatus(s IssueStatus) bool {
	switch s {
	case IssueBacklog, IssueTodo, IssueRunning, IssueReview, IssuePROpen, IssueDone, IssueFailed:
		return true
	}
	return false
}

type Issue struct {
	ID           int64       `json:"id"`
	ProjectID    int64       `json:"projectId"`
	Key          string      `json:"key"`
	Source       string      `json:"source"` // local | remote
	RepoID       *int64      `json:"repoId"`
	RemoteNumber *int64      `json:"remoteNumber"`
	RemoteURL    string      `json:"remoteUrl"`
	Title        string      `json:"title"`
	Body         string      `json:"body"`
	Labels       []string    `json:"labels"`
	Status       IssueStatus `json:"status"`
	ActiveRunID  *int64      `json:"activeRunId"`
	Awaiting     bool        `json:"awaiting"`
	PRURL        string      `json:"prUrl"`
	PRState      string      `json:"prState"` // "" | "open" | "merged" | "closed"
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

type Comment struct {
	ID        int64     `json:"id"`
	IssueID   int64     `json:"issueId"`
	Author    string    `json:"author"` // me | agent | system
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunStarting  RunStatus = "starting"
	RunRunning   RunStatus = "running"
	RunAwaiting  RunStatus = "awaiting_permission"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
	RunSubmitted RunStatus = "submitted"
	RunDiscarded RunStatus = "discarded"
)

// Active reports whether an agent process may currently be working on the run.
func (s RunStatus) Active() bool {
	return s == RunQueued || s == RunStarting || s == RunRunning || s == RunAwaiting
}

type Run struct {
	ID           int64      `json:"id"`
	IssueID      int64      `json:"issueId"`
	Agent        string     `json:"agent"`
	Model        string     `json:"model"`
	Status       RunStatus  `json:"status"`
	Branch       string     `json:"branch"`
	TargetBranch string     `json:"targetBranch"`
	Workdir      string     `json:"workdir"`
	SessionID    string     `json:"sessionId"`
	Summary      string     `json:"summary"`
	Error        string     `json:"error"`
	PRURL        string     `json:"prUrl"`
	PRState      string     `json:"prState"`
	ChangeRecord string     `json:"changeRecord"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt"`
	EndedAt      *time.Time `json:"endedAt"`
}

type RunEvent struct {
	ID        int64           `json:"id"`
	RunID     int64           `json:"runId"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"createdAt"`
}

type BranchWatch struct {
	ID          int64     `json:"id"`
	ProjectID   int64     `json:"projectId"`
	RepoID      int64     `json:"repoId"`
	RepoName    string    `json:"repoName"`
	Branch      string    `json:"branch"`
	LastSHA     string    `json:"lastSha"`
	LastMessage string    `json:"lastMessage"`
	LastAuthor  string    `json:"lastAuthor"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Notification struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"` // pr_status | branch_push | run_done | permission
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Link      string    `json:"link"`
	Meta      string    `json:"meta"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"createdAt"`
}

