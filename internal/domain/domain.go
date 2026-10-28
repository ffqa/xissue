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
	ID                    int64            `json:"id"`
	Name                  string           `json:"name"`
	Description           string           `json:"description"`
	DevMode               DevMode          `json:"devMode"`
	TargetBranch          string           `json:"targetBranch"`
	AutoPROnConfirm       bool             `json:"autoPrOnConfirm"`
	AutoDevelop           bool             `json:"autoDevelop"`           // 同步到新 issue 后自动预开发，产物仅开 PR 不合并
	AutoDevelopAgent      string           `json:"autoDevelopAgent"`      // 预开发使用的 agent，空则取默认
	AutoDevelopLabels     []string         `json:"autoDevelopLabels"`     // 视为需求/Bug 的标签（useText 时也作为正文关键字）
	AutoDevelopText       bool             `json:"autoDevelopText"`       // 是否同时匹配标题/正文关键字（公开仓库应关闭）
	AutoDevelopAuthors    []string         `json:"autoDevelopAuthors"`    // 仅响应这些作者创建的 issue；自动开发时必填，空=拒绝
	AutoDevelopDailyLimit int              `json:"autoDevelopDailyLimit"` // 每项目每日自动预开发 run 上限，0=使用默认 5
	AutomationRules       []AutomationRule `json:"automationRules"`
	PullBranches          []string         `json:"pullBranches"` // 手动/自动拉取时只更新这些分支，空=全部
	RunnerID              string           `json:"runnerId"`     // empty = local built-in runner; otherwise a connected remote runner
	Repos                 []Repo           `json:"repos"`
	CreatedAt             time.Time        `json:"createdAt"`
	UpdatedAt             time.Time        `json:"updatedAt"`
}

// DefaultAutoDevelopLabels lists labels/keywords that mark an issue as an
// actionable bug or feature request for automatic pre-development.
func DefaultAutoDevelopLabels() []string {
	return []string{"bug", "fix", "error", "crash", "regression", "enhancement", "feature", "需求", "缺陷", "优化", "功能", "报错", "崩溃"}
}

type RuleConditions struct {
	Author      string `json:"author,omitempty" yaml:"author,omitempty"`           // 指定提交人/作者 (如 release-bot, qa-team，留空匹配任意)
	Pattern     string `json:"pattern,omitempty" yaml:"pattern,omitempty"`         // 正则表达式匹配内容 (如 (?i)status:\s*passed 或 (?i)feat:)
	ReviewState string `json:"reviewState,omitempty" yaml:"reviewState,omitempty"` // 审核状态 (approved / changes_requested / commented)
	Branch      string `json:"branch,omitempty" yaml:"branch,omitempty"`           // 监控分支匹配 (如 main, dev，留空匹配任意分支)
}

type RuleAction struct {
	Type     string   `json:"type" yaml:"type"`                             // merge_pr | set_issue_status | pull_local | notify | create_issue | trigger_run
	Status   string   `json:"status,omitempty" yaml:"status,omitempty"`     // for set_issue_status, e.g. "done"
	Strategy string   `json:"strategy,omitempty" yaml:"strategy,omitempty"` // for merge_pr: "squash" | "merge" | "rebase"
	Message  string   `json:"message,omitempty" yaml:"message,omitempty"`   // for notify
	Channel  string   `json:"channel,omitempty" yaml:"channel,omitempty"`   // for notify: "telegram" | "webhook" | "desktop" | "all"
	Agent    string   `json:"agent,omitempty" yaml:"agent,omitempty"`       // for trigger_run: 指定运行的 agent 名称 (如 codex, opencode, omp)
	Prompt   string   `json:"prompt,omitempty" yaml:"prompt,omitempty"`     // for trigger_run / create_issue
	Labels   []string `json:"labels,omitempty" yaml:"labels,omitempty"`     // for add_labels
}

type AutomationRule struct {
	ID         string         `json:"id" yaml:"id"`
	Name       string         `json:"name" yaml:"name"`
	Enabled    bool           `json:"enabled" yaml:"enabled"`
	Trigger    string         `json:"trigger" yaml:"trigger"` // pr_comment | pr_review | branch_push
	Conditions RuleConditions `json:"conditions" yaml:"conditions"`
	Actions    []RuleAction   `json:"actions" yaml:"actions"`
	ProjectIDs []int64        `json:"projectIds,omitempty" yaml:"projectIds,omitempty"` // 全局规则可选的项目范围，空=全部项目
}

type HookStatus struct {
	Name        string `json:"name"`        // e.g. "on-pr-comment"
	Event       string `json:"event"`       // e.g. "pr_comment"
	Path        string `json:"path"`        // 脚本路径
	Exists      bool   `json:"exists"`      // 是否存在
	Executable  bool   `json:"executable"`  // 是否具有执行权限
	Description string `json:"description"` // 说明
}

type PluginUIInfo struct {
	Entry string   `json:"entry"`
	Title string   `json:"title"`
	Slots []string `json:"slots"`
}

type PluginInfo struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	APIVersion  int           `json:"apiVersion"`
	Description string        `json:"description"`
	Author      string        `json:"author"`
	Events      []string      `json:"events"`
	Permissions []string      `json:"permissions"`
	Path        string        `json:"path"`
	Enabled     bool          `json:"enabled"`
	HasReadme   bool          `json:"hasReadme"`
	HasConfig   bool          `json:"hasConfig"`
	UI          *PluginUIInfo `json:"ui,omitempty"`
	Error       string        `json:"error,omitempty"`
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

type IssuePriority string

const (
	IssuePriorityLow    IssuePriority = "low"
	IssuePriorityNormal IssuePriority = "normal"
	IssuePriorityHigh   IssuePriority = "high"
	IssuePriorityUrgent IssuePriority = "urgent"
)

func ValidIssuePriority(p IssuePriority) bool {
	switch p {
	case IssuePriorityLow, IssuePriorityNormal, IssuePriorityHigh, IssuePriorityUrgent:
		return true
	}
	return false
}

type Issue struct {
	ID           int64         `json:"id"`
	ProjectID    int64         `json:"projectId"`
	Key          string        `json:"key"`
	Source       string        `json:"source"` // local | remote
	RepoID       *int64        `json:"repoId"`
	RemoteNumber *int64        `json:"remoteNumber"`
	RemoteURL    string        `json:"remoteUrl"`
	Title        string        `json:"title"`
	Body         string        `json:"body"`
	Labels       []string      `json:"labels"`
	Priority     IssuePriority `json:"priority"`
	Status       IssueStatus   `json:"status"`
	ActiveRunID  *int64        `json:"activeRunId"`
	Awaiting     bool          `json:"awaiting"`
	PRURL        string        `json:"prUrl"`
	PRState      string        `json:"prState"` // "" | "open" | "merged" | "closed"
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
	SyncError    string        `json:"syncError,omitempty"` // transient response field, not persisted
}

type IssueEvent struct {
	ID        int64     `json:"id"`
	IssueID   int64     `json:"issueId"`
	Kind      string    `json:"kind"`
	Actor     string    `json:"actor"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

type Attachment struct {
	ID        int64     `json:"id"`
	IssueID   int64     `json:"issueId"`
	Name      string    `json:"name"`
	MIME      string    `json:"mime"`
	Size      int64     `json:"size"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
}

type Comment struct {
	ID        int64     `json:"id"`
	IssueID   int64     `json:"issueId"`
	Author    string    `json:"author"` // me | agent | system | remote login
	Body      string    `json:"body"`
	RemoteID  string    `json:"remoteId,omitempty"`
	RemoteURL string    `json:"remoteUrl,omitempty"`
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
	ID           int64     `json:"id"`
	IssueID      int64     `json:"issueId"`
	Agent        string    `json:"agent"`
	Model        string    `json:"model"`
	Status       RunStatus `json:"status"`
	Branch       string    `json:"branch"`
	TargetBranch string    `json:"targetBranch"`
	Workdir      string    `json:"workdir"`
	SessionID    string    `json:"sessionId"`
	Summary      string    `json:"summary"`
	Error        string    `json:"error"`
	PRURL        string    `json:"prUrl"`
	PRState      string    `json:"prState"`
	ChangeRecord string    `json:"changeRecord"`
	AutoPR       bool      `json:"autoPr"` // 自动预开发产生的 run，完成后自动发 PR（不合并）
	RunnerID     string    `json:"runnerId"`
	// Usage is populated from ACP usage_update / prompt usage. CostAmount is
	// attributed to this run (deltas of the session's cumulative cost).
	InputTokens         int64      `json:"inputTokens"`
	OutputTokens        int64      `json:"outputTokens"`
	TotalTokens         int64      `json:"totalTokens"`
	ContextUsed         int64      `json:"contextUsed"`
	ContextSize         int64      `json:"contextSize"`
	CostAmount          float64    `json:"costAmount"`
	CostCurrency        string     `json:"costCurrency"`
	UsageCumulativeCost float64    `json:"usageCumulativeCost"`
	UsageJSON           string     `json:"usageJson,omitempty"`
	UsageUpdatedAt      *time.Time `json:"usageUpdatedAt"`
	CreatedAt           time.Time  `json:"createdAt"`
	StartedAt           *time.Time `json:"startedAt"`
	EndedAt             *time.Time `json:"endedAt"`
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

// DocSource defines the source kind of a knowledge base document.
type DocSource string

const (
	DocSourceGitHubWiki DocSource = "github_wiki" // GitHub Wiki (repo.wiki.git)
	DocSourceLocalDoc   DocSource = "local_doc"   // 本地仓库 docs/README
)

// DocItem represents a summarized entry in the project knowledge base.
type DocItem struct {
	ID        string    `json:"id"`        // Unique doc identifier (e.g. "wiki:1:Home.md" or "local:1:docs/design.md")
	ProjectID int64     `json:"projectId"` // Project ID
	RepoID    int64     `json:"repoId"`    // Associated repository ID
	RepoName  string    `json:"repoName"`  // Associated repository name
	Source    DocSource `json:"source"`    // "github_wiki" | "local_doc"
	Title     string    `json:"title"`     // Extracted title from first markdown header or filename
	Path      string    `json:"path"`      // Relative file path (e.g. "docs/design.md" or "Home.md")
	Category  string    `json:"category"`  // requirements | adr | reports | work | other
	Size      int64     `json:"size"`      // File size in bytes
	Excerpt   string    `json:"excerpt"`   // Short excerpt for search/list preview
	UpdatedAt time.Time `json:"updatedAt"` // Last modified time
}

// DocDetail represents the full content of a knowledge base document.
type DocDetail struct {
	DocItem
	Content string `json:"content"` // Full Markdown content
}

// ProjectReadme is the root README of one repository in a project.
type ProjectReadme struct {
	RepoID    int64  `json:"repoId"`
	RepoName  string `json:"repoName"`
	Branch    string `json:"branch"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Found     bool   `json:"found"`
}

// DocSearchResult represents a hit in knowledge base search.
type DocSearchResult struct {
	DocItem
	Snippet string `json:"snippet"` // Matched highlighted snippet with context
	Score   int    `json:"score"`   // Relevance score
}
