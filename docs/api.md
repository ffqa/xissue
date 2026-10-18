# xIssue HTTP API（M1）

前后端之间的契约。所有 JSON 字段使用 camelCase，时间为 RFC3339 字符串。

## 鉴权

- 默认无鉴权（监听 127.0.0.1 / Tailscale）。
- 若配置了 `token`：请求需带 `Authorization: Bearer <token>`，或 Cookie `xissue_token=<token>`。
  访问 `/?token=<token>` 时服务端会写入该 Cookie 并重定向到 `/`。WebSocket 依赖 Cookie。

## 类型

```ts
type Provider = "github" | "gitlab" | "gitea" | "plain";

interface Repo {
  id: number; projectId: number;
  name: string;            // 工作区内的目录名，如 "frontend"
  provider: Provider;
  remoteUrl: string;       // git clone 地址
  ownerRepo: string;       // 平台上的 "owner/name"；plain 为空
  defaultBranch: string;   // 如 "main"
  testCommand: string;     // 可选
}

interface Project {
  id: number; name: string; description: string;
  repos: Repo[];
  createdAt: string; updatedAt: string;
}

type IssueStatus = "backlog" | "todo" | "running" | "review" | "pr_open" | "done" | "failed";

interface Issue {
  id: number; projectId: number;
  key: string;             // "LOCAL-42" 或 "owner/name#12"
  source: "local" | "remote";
  repoId: number | null;   // remote issue 所在仓库
  remoteNumber: number | null;
  remoteUrl: string;
  title: string; body: string;  // body 为 Markdown
  labels: string[];
  status: IssueStatus;
  activeRunId: number | null;   // 最近一次 Run
  awaiting: boolean;            // 最近一次 Run 正在等待权限审批
  createdAt: string; updatedAt: string;
}

interface Comment {
  id: number; issueId: number;
  author: "me" | "agent" | "system";
  body: string; createdAt: string;
}

type RunStatus = "queued" | "starting" | "running" | "awaiting_permission"
               | "completed" | "failed" | "cancelled" | "submitted" | "discarded";

interface Run {
  id: number; issueId: number;
  agent: string;           // "opencode" | "omp" | "codex" ...
  status: RunStatus;
  branch: string;          // 所有仓库共用的分支名
  workdir: string;         // 运行目录绝对路径（父目录，下面是各仓库 worktree）
  sessionId: string;
  summary: string; error: string;
  createdAt: string; startedAt: string | null; endedAt: string | null;
}

interface RunEvent {
  id: number; runId: number; seq: number;
  type: "message" | "thought" | "tool_call" | "tool_call_update" | "plan"
      | "permission_request" | "permission_resolved" | "status" | "stderr" | "system" | "user";
  data: any;
  createdAt: string;
}
```

`RunEvent.data` 约定：

| type | data |
|---|---|
| message / thought | `{ text: string }`（流式分片，前端需按顺序拼接相邻同类事件） |
| user | `{ text: string }`（用户追加的指令） |
| tool_call | `{ toolCallId, title, kind, status, rawInput? }` |
| tool_call_update | `{ toolCallId, status?, title?, content? }` |
| plan | `{ entries: [{ content, status, priority }] }` |
| permission_request | `{ requestId: string, title: string, toolCall: any, options: [{ optionId, name, kind }] }`，kind 为 `allow_once`/`allow_always`/`reject_once`/`reject_always` |
| permission_resolved | `{ requestId, optionId, auto: boolean }` |
| status | `{ status: RunStatus }` |
| stderr / system | `{ text: string }` |

## 接口

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | `{ ok, version, editorUrlTemplate }`。`editorUrlTemplate` 如 `vscode://file{path}`，前端替换 `{path}` 为绝对路径 |
| GET | `/api/agents` | `[{ name, command, available }]` |
| GET | `/api/projects` | `Project[]` |
| POST | `/api/projects` | `{ name, description }` → `Project` |
| GET / PATCH / DELETE | `/api/projects/{id}` | PATCH `{ name?, description? }` |
| POST | `/api/projects/{id}/repos` | `{ name, provider, remoteUrl, ownerRepo, defaultBranch, testCommand }` → `Repo` |
| DELETE | `/api/projects/{id}/repos/{repoId}` | |
| POST | `/api/projects/{id}/sync` | 从平台导入 open issues → `{ imported: number }` |
| GET | `/api/projects/{id}/issues` | `Issue[]` |
| POST | `/api/projects/{id}/issues` | `{ title, body, labels? }` → `Issue`（本地 Issue，status=todo） |
| GET / PATCH | `/api/issues/{id}` | PATCH `{ title?, body?, status? }` |
| POST | `/api/issues/{id}/publish` | `{ repoId }` 把本地 Issue 发布到平台 → `Issue` |
| GET / POST | `/api/issues/{id}/comments` | POST `{ body }` |
| GET | `/api/issues/{id}/runs` | `Run[]`（新的在前） |
| POST | `/api/issues/{id}/runs` | `{ agent, prompt? }` 启动 Run → `Run`。`prompt` 为额外说明 |
| GET | `/api/runs/{id}` | `Run` |
| GET | `/api/runs/{id}/events?after=<seq>` | `RunEvent[]` |
| POST | `/api/runs/{id}/message` | `{ text }` 追加指令 / 打回反馈（会话结束后会自动恢复会话） |
| POST | `/api/runs/{id}/permissions/{requestId}` | `{ optionId }` |
| POST | `/api/runs/{id}/cancel` | |
| GET | `/api/runs/{id}/changes` | `[{ repoId, repoName, branch, files: [{ path, status, additions, deletions }] }]`，status 为 `M/A/D/R/?` |
| GET | `/api/runs/{id}/file?repoId=&path=&side=base\|work` | `{ content: string }`，base 为默认分支版本 |
| PUT | `/api/runs/{id}/file?repoId=&path=` | `{ content }` 保存到工作区 |
| POST | `/api/runs/{id}/submit` | `{ mode: "pr" \| "push", title?, body?, commitMessage? }` → `{ results: [{ repoId, repoName, branch, commit, prUrl, error }] }` |
| POST | `/api/runs/{id}/discard` | 删除 worktree 与分支 |

## WebSocket

`GET /api/ws`，服务端单向推送：

```ts
{ type: "run_event", runId, issueId, event: RunEvent }
{ type: "run_updated", run: Run }
{ type: "issue_updated", issue: Issue }
```

错误统一返回 `{ error: string }` 与对应 HTTP 状态码。
