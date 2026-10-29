# xIssue HTTP API（M1）

前后端之间的契约。所有 JSON 字段使用 camelCase，时间为 RFC3339 字符串。

## 鉴权

- 默认无 token（监听 127.0.0.1 / Tailscale），但仍有跨站防护：
  - 校验 `Host` 头，只接受 `127.0.0.1` / `localhost` / `::1` 或与 `-listen` 一致的主机（防 DNS rebinding）；
  - 所有状态变更请求（`POST` / `PUT` / `PATCH` / `DELETE`）若带 `Origin`，必须是同源；
  - 状态变更请求还必须带 `X-XIssue: 1` 自定义头，或提供有效 Bearer token（`/api/mcp` 除外，供 CLI/IDE 调用）。
- 若配置了 `token`：请求需带 `Authorization: Bearer <token>`，或 Cookie `xissue_token=<token>`。
  访问 `/?token=<token>` 时服务端会写入该 Cookie 并重定向到 `/`。WebSocket 依赖 Cookie，并校验 Origin。
- `GET /api/fs/browse` / `GET /api/fs/inspect` 无根目录白名单；配置 token 后应视为敏感接口。
- `GET /api/runners/ws` 供远程执行节点主动拨入；配置 token 时必须带 Bearer，未配置 token 时只允许回环地址，并拒绝带浏览器 `Origin` 的请求。连接建立后服务端通过 `agents` RPC 探测该节点的 Agent 命令；`GET /api/runners` 返回 `agents` 与 `agentsKnown`，启动远程 Run 时若已探测完成则拒绝该节点不存在的 Agent。
- `GET /api/plugins/{id}/ui/{path...}` 提供插件静态 UI；只能访问 `<plugin>/ui/` 下的文件，拒绝 `..` 与符号链接逃逸。

## 类型

```ts
type Provider = "github" | "gitlab" | "gitea" | "plain";
// github、gitea 与 gitlab 均实现 Issue/PR API。

interface MergedBranch {
  name: string; sha: string; author: string; email: string;
  subject: string; date: string;
}

interface Repo {
  id: number; projectId: number;
  name: string;            // 工作区内的目录名，如 "frontend"
  provider: Provider;
  remoteUrl: string;       // git clone 地址
  ownerRepo: string;       // 平台上的 "owner/name"；plain 为空
  defaultBranch: string;   // 如 "main"
  testCommand: string;     // 可选
}

type DevMode = "single_main" | "main_dev";

interface RemoteAgentCapability {
  name: string;
  available: boolean;
  path?: string;
  error?: string;
}

interface RemoteRunnerInfo {
  id: string; name: string; version: string; online: boolean;
  connectedAt?: string;
  agents?: RemoteAgentCapability[];
  agentsKnown: boolean;    // 探测完成前为 false，服务端此时不会拒绝 Agent
}

interface PluginUIInfo {
  entry: string;           // 必须位于 ui/ 下
  title: string;
  slots: string[];         // project.page | project.header | issue.page | issue.actions
}

interface Project {
  id: number; name: string; description: string;
  devMode: DevMode;        // "single_main" (默认直合本地 main) | "main_dev" (必须提 PR 到 dev)
  targetBranch: string;    // 默认目标分支，如 "main" 或 "dev"
  autoPrOnConfirm: boolean;
  pullBranches: string[];  // 拉取代码时只更新这些分支，空数组=全部
  runnerId?: string;       // 空=本机；非空=已连接的远程 Runner 名称
  automationRules: AutomationRule[];
  repos: Repo[];
  createdAt: string; updatedAt: string;
}

type IssueStatus = "backlog" | "todo" | "running" | "review" | "pr_open" | "done" | "failed";
type IssuePriority = "low" | "normal" | "high" | "urgent";

interface Issue {
  id: number; projectId: number;
  key: string;             // "LOCAL-42" 或 "owner/name#12"
  source: "local" | "remote";
  repoId: number | null;   // remote issue 所在仓库
  parentIssueId: number | null; // 父任务；子任务创建后不可成环
  remoteNumber: number | null;
  remoteUrl: string;
  title: string; body: string;  // body 为 Markdown
  labels: string[];
  priority: IssuePriority;  // 默认 normal
  backlogRank: number;       // Backlog 手动排序
  status: IssueStatus;
  activeRunId: number | null;   // 最近一次 Run
  awaiting: boolean;            // 最近一次 Run 正在等待权限审批
  createdAt: string; updatedAt: string;
  syncError?: string;           // 仅创建响应：远程同步失败原因，本地 Issue 仍保留
}

interface Job {
  id: number; kind: string;
  status: "queued" | "running" | "completed" | "failed" | "cancelled";
  repoId: number; payload: { branch?: string }; error?: string;
  createdAt: string; startedAt?: string; finishedAt?: string;
}

interface IssueEvent {
  id: number; issueId: number;
  kind: string;                 // created/status/priority/labels/remote_sync/attachment...
  actor: string; message: string;
  createdAt: string;
}

interface Attachment {
  id: number; issueId: number;
  name: string; mime: string; size: number;
  url: string; createdAt: string;
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

> 下表为 M1 阶段的核心接口，并非完整路由清单；`internal/server/server.go` 中注册的 agents 管理、监控配置、watches、docs/Wiki、cherry-pick、plugins、notifications、MCP 等端点未全部列出。以代码注册表为准。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | `{ ok, version, editorUrlTemplate }`。`editorUrlTemplate` 如 `vscode://file{path}`，前端替换 `{path}` 为绝对路径 |
| GET | `/api/status` | 实时运行状态：版本、PID、uptime、堆内存、进程 CPU、磁盘、`dataUsed`/`runWorkspaces`/`database`、`queue.pending/running/maxConcurrent/maxQueued`、GC、goroutine、自动更新状态；WebSocket 每 2 秒推送同结构 |
| GET / PATCH | `/api/settings` | 全局设置；PATCH `{ autoUpdate?, pluginUISandbox?, defaultAgent?, agentRoles?, globalAutomationRules? }`；`pluginUISandbox` 默认 `false`（插件 iframe 同源、可访问宿主），设为 `true` 时启用无同源权限的 sandbox。`agentRoles` 为 Agent 身份分配 `{ issueReview?, issueDevelop?, prReview?, autoMerge? }`（留空跟随 `defaultAgent`；全局自动化规则支持 `projectIds` 限定项目，空数组表示全部项目） |
| GET / PUT / PATCH | `/api/monitor/config` | 监控与通知配置；`notificationRoutes` 为按事件分通道的路由表 |
| GET | `/api/plugins` | 已安装插件 → `PluginInfo[]`（`apiVersion`、`events`、`permissions`、`enabled`、`hasReadme`、`hasConfig`、`error`）；CLI 为 `xissue plugin list` |
| PATCH | `/api/plugins/{id}` | `{ enabled }` 即时启用/停用（写/删 `.disabled`）→ `PluginInfo[]`；manifest 无效时不能启用 |
| GET | `/api/plugins/{id}/readme` | `{ name, path, content }`，预览插件 README |
| GET | `/api/plugins/{id}/config` | `{ exists, template, content, path }`；无 `config.json` 时优先返回 `config.example.json` 内容 |
| PUT | `/api/plugins/{id}/config` | `{ content }` 原子写入 `config.json`（校验 JSON，权限 `0600`）→ 同 GET |
| GET | `/api/agents` | `[{ name, command, available }]` |
| GET | `/api/runners` | `{ runners: [{ id, name, version, online, connectedAt }] }`；远程 Runner 通过 `GET /api/runners/ws`（WebSocket，`xissue runner`）注册 |
| GET | `/api/projects` | `Project[]` |
| POST | `/api/projects` | `{ name, description }` → `Project` |
| GET / PATCH / DELETE | `/api/projects/{id}` | PATCH `{ name?, description? }` |
| POST | `/api/projects/{id}/repos` | `{ name, provider, remoteUrl, ownerRepo, defaultBranch, testCommand }` → `Repo`；`name` 会去除首尾空白，必须是单个目录名（字母数字开头，可含空格、`.`、`-`、`_`，禁止 `/`、`\`、`..`、`.git`） |
| DELETE | `/api/projects/{id}/repos/{repoId}` | 仓库仍被 Issue 引用时返回 409，避免 `repo_id` 被置空后执行范围丢失 |
| GET | `/api/repos/{repoId}/merged-branches?refresh=1` | 已合并到默认分支、且 tip 作者匹配本机 Git 身份的远程分支 → `MergedBranch[]` |
| POST | `/api/repos/{repoId}/branches/delete` | `{ branches: string[] }` → `{ deleted: string[] }`；只允许删除上列分支 |
| POST | `/api/projects/{id}/sync` | 从平台导入 open issues → `{ imported: number }` |
| POST | `/api/projects/{id}/pull` | 刷新所有仓库镜像；仅当本地当前分支等于目标分支且工作区干净时执行 `git pull --ff-only origin <target>`。返回 `{ ok, pulledRepos, repos, message }`，`repos[]` 逐仓说明 `mirrorUpdated`、`localUpdated`、`reason`、`error`；任一仓库 Git 失败返回 502 |
| GET | `/api/projects/{id}/prs` | 查询项目 PR 列表（`?state=open\|merged\|closed\|all`）→ `RemotePR[]`；含可选 CI 汇总 `ciState`/`ciPassed`/`ciFailed`/`ciPending` |
| POST | `/api/projects/{id}/prs/{number}/merge` | 合并项目内仓库的 PR，刷新镜像并尝试快进本地目标分支；`message` 包含逐仓本地更新结果。`repoId` 必须属于路径中的项目，否则 400 → `{ ok, merged, message }` |
| POST | `/api/projects/{id}/prs/{number}/close` | 在项目中直接关闭 PR → `{ ok, closed }` |
| GET | `/api/repos/{repoId}/commits/{sha}` | 单个 Commit 详情：元信息、文件增删统计、统一 diff（超过 512KB 截断） |
| GET | `/api/schedules` | 后台定时任务列表：`{ id, name, interval, intervalSeconds, detail, enabled, settings?, nextRun? }[]`；`settings=monitor` 去监控中心启用，`settings=settings` 去通用设置启用 |
| GET | `/api/monitor/status` | 获取监控服务状态、上次检查耗时、监控分支与 PR 数 → `MonitorStatus` |
| GET | `/api/monitor/logs` | 查询监控与执行日志（`?hours=8` 默认最近8小时）→ `MonitorLogEntry[]` |
| POST | `/api/monitor/trigger` | 立即触发一次全分支与 PR 状态检查 → `{ ok, watchesTriggered, prsUpdated, message }` |
| POST | `/api/monitor/sync` | 立即同步所有项目的远程 Issue → `{ ok, projects, imported, errors?, message }` |
| POST | `/api/monitor/prune` | 立即裁剪旧监控日志（`?hours=8`）→ `{ ok, prunedCount, message }` |
| POST | `/api/maintenance/prune` | 立即执行 Run 归档/日志清理/工作区清理（按 `runner.eventRetentionDays` 与 `runner.workspaceRetentionDays`）→ `{ ok, stats }` |
| POST | `/api/mcp` | MCP Streamable HTTP JSON-RPC（`Accept: application/json` 或 `text/event-stream`）；无状态，所有客户端共享一个服务 |
| DELETE | `/api/mcp` | 无状态 MCP 会话终止，返回 204 |
| GET | `/api/projects/{id}/issues` | `Issue[]` |
| POST | `/api/projects/{id}/backlog/reorder` | `{ ids: number[] }` 写入 Backlog 顺序 |
| POST | `/api/projects/{id}/issues` | `{ title, body, labels?, priority?, repoId?, parentId?, syncRemote? }` → `Issue`。`syncRemote=true` 且仓库支持 Issue 时，后端创建远程 Issue 并把 `source/key/remoteNumber/remoteUrl` 绑定到本地行；失败返回 201 + `syncError`，本地 Issue 保留 |
| GET / PATCH | `/api/issues/{id}` | PATCH `{ title?, body?, labels?, priority?, status? }` |
| POST | `/api/issues/{id}/publish` | `{ repoId }` 把本地 Issue 发布到平台 → `Issue`；`repoId` 必须属于 Issue 所在项目，否则 400 |
| GET / POST | `/api/issues/{id}/comments` | POST `{ body }` |
| GET | `/api/issues/{id}/events` | `IssueEvent[]`（状态/优先级/标签/远程同步/附件等时间线事件） |
| GET | `/api/issues/{id}/subissues` | 子任务 `Issue[]` |
| GET / POST | `/api/issues/{id}/pr-links` | 手动关联 PR；POST `{ url, title? }` |
| DELETE | `/api/issues/{id}/pr-links/{linkID}` | 移除手动 PR 关联 |
| GET / POST | `/api/issues/{id}/attachments` | GET → `Attachment[]`；POST multipart `file`（单文件 ≤10MB）→ `Attachment` |
| GET / DELETE | `/api/issues/{id}/attachments/{attachmentID}` | 下载/预览附件或删除附件 |
| GET | `/api/issues/{id}/runs` | `Run[]`（新的在前） |
| POST | `/api/issues/{id}/runs` | `{ agent?, prompt? }` 启动 Run → `Run`。`agent` 留空时使用 `agentRoles.issueDevelop`（其次 `defaultAgent`）；`prompt` 为额外说明 |
| GET | `/api/runs/{id}` | `Run` |
| GET | `/api/runs/{id}/events?after=<seq>` | `RunEvent[]`；首次（`after=0`）最多返回最近 500 条，之后用 `after=seq` 增量补齐；完整历史在归档 `events.log` |
| POST | `/api/runs/{id}/message` | `{ text }` 追加指令 / 打回反馈（会话结束后会自动恢复会话） |
| POST | `/api/runs/{id}/permissions/{requestId}` | `{ optionId }` |
| POST | `/api/runs/{id}/cancel` | |
| GET | `/api/runs/{id}/changes` | `[{ repoId, repoName, branch, files: [{ path, status, additions, deletions }] }]`，status 为 `M/A/D/R/?` |
| GET | `/api/runs/{id}/file?repoId=&path=&side=base\|work` | `{ content: string }`，base 为默认分支版本；路径必须位于仓库 worktree 内 |
| PUT | `/api/runs/{id}/file?repoId=&path=` | `{ content }` 保存到工作区；拒绝绝对路径、`..` 与逃逸 worktree 的符号链接 |
| POST | `/api/runs/{id}/submit` | `{ mode: "merge_local" \| "pr" \| "push", title?, body?, commitMessage?, targetBranch?, changeItems?, remark? }` → `{ results: [{ repoId, repoName, branch, targetBranch, commit, prUrl, changeRecord, error }] }` |
| POST | `/api/runs/{id}/discard` | 删除 worktree 与分支 |

## WebSocket

`GET /api/ws`，服务端单向推送：

```ts
{ type: "run_event", runId, issueId, event: RunEvent }
{ type: "run_updated", run: Run }
{ type: "issue_updated", issue: Issue }
{ type: "notification", notification: Notification }
{ type: "status", status: SystemStatus }  // 每 2 秒；状态栏优先使用此通道
{ type: "heartbeat" }
```

错误统一返回 `{ error: string }` 与对应 HTTP 状态码。
