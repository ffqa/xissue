// Package remote defines the control protocol between the xIssue server and a
// remote runner. The runner always dials out; the server never needs an inbound
// port on the execution machine.
package remote

import (
	"encoding/json"

	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
)

const Version = 1

// Frame is the single WebSocket message shape used in both directions.
type Frame struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	RequestID string          `json:"requestId,omitempty"`
	RunID     int64           `json:"runId,omitempty"`
	RunnerID  string          `json:"runnerId,omitempty"`
	Name      string          `json:"name,omitempty"`
	Version   string          `json:"version,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Event     *Event          `json:"event,omitempty"`
	Status    string          `json:"status,omitempty"`
	Workdir   string          `json:"workdir,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Usage     *acp.Usage      `json:"usage,omitempty"`
	Job       *Job            `json:"job,omitempty"`
	OptionID  string          `json:"optionId,omitempty"`
	AutoPush  bool            `json:"autoPush,omitempty"`
	Message   string          `json:"message,omitempty"`
}

// Event is a normalized ACP event forwarded to the server.
type Event struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// Job tells a remote runner to start (or resume) a run.
type Job struct {
	RunID           int64              `json:"runId"`
	RunnerID        string             `json:"runnerId,omitempty"`
	IssueID         int64              `json:"issueId"`
	ProjectID       int64              `json:"projectId"`
	IssueKey        string             `json:"issueKey"`
	IssueTitle      string             `json:"issueTitle"`
	Prompt          string             `json:"prompt"`
	AgentName       string             `json:"agentName"`
	Agent           config.AgentConfig `json:"agent"`
	Model           string             `json:"model"`
	Branch          string             `json:"branch"`
	TargetBranch    string             `json:"targetBranch"`
	SessionID       string             `json:"sessionId,omitempty"`
	Workdir         string             `json:"workdir,omitempty"`
	Repos           []domain.Repo      `json:"repos"`
	WorkspaceAGENTS string             `json:"workspaceAgents,omitempty"`
	AutoPR          bool               `json:"autoPr,omitempty"`
	IsResume        bool               `json:"isResume,omitempty"`
	AutoPush        bool               `json:"autoPush,omitempty"`
}

// PermissionRequest is sent runner -> server and answered with OptionID.
type PermissionRequest struct {
	Title    string                 `json:"title"`
	Kind     string                 `json:"kind"`
	ToolCall map[string]any         `json:"toolCall"`
	Options  []acp.PermissionOption `json:"options"`
}

// PermissionOption is re-exported here to keep the JSON shape explicit.
type PermissionOption = acp.PermissionOption

// RPCRequest is a server -> runner workspace request.
type RPCRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// ChangesResult is the response to "changes".
type ChangesResult struct {
	Changes []RunnerRepoChanges `json:"changes"`
}

type RunnerRepoChanges struct {
	RepoID   int64              `json:"repoId"`
	RepoName string             `json:"repoName"`
	Branch   string             `json:"branch"`
	Files    []RunnerFileChange `json:"files"`
}

type RunnerFileChange struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// SubmitResult is returned by a remote "submit" RPC.
type SubmitResult struct {
	RepoID       int64              `json:"repoId"`
	RepoName     string             `json:"repoName"`
	Branch       string             `json:"branch"`
	TargetBranch string             `json:"targetBranch"`
	Commit       string             `json:"commit"`
	PRURL        string             `json:"prUrl,omitempty"`
	ChangeRecord string             `json:"changeRecord,omitempty"`
	Files        []RunnerFileChange `json:"files,omitempty"`
	Error        string             `json:"error,omitempty"`
}
