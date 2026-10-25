# xIssue 功能索引

本文件按项目约定保留 `Fetures.md` 拼写。项目简介、安装和运行命令见 [README.md](README.md)；完整 HTTP 字段、鉴权规则和返回类型见 [docs/api.md](docs/api.md)。以下是当前源码中的主要功能入口，功能行为以运行中的版本为准。

## 基本使用

1. 安装并启动 `xissue`，在本机浏览器打开服务地址（默认监听 `127.0.0.1:7789`）。
2. 创建项目，添加一个或多个 Git 仓库；也可从本地目录导入。
3. 新建 Issue，选择目标仓库（可选），或者从远端平台同步 Issue。
4. 在看板启动 Agent Run，检查运行日志和文件差异，再选择提交、提 PR 或丢弃。
5. 在监控设置中管理 Issue 同步、分支监控与通知；项目设置中管理自动化规则和代码拉取。

调用 `/api/` 下的写入接口通常需要 `X-XIssue: 1` 或有效 Bearer token；配置了 token 后还需要通过鉴权。时间字段为 RFC3339，JSON 字段使用 camelCase。错误返回 `{ "error": "原因" }`；详情见 [docs/api.md](docs/api.md)。

## 项目与仓库

| 入口 | 用途与主要参数 | 成功返回 |
| --- | --- | --- |
| `GET /api/projects` | 列出项目，无参数 | `Project[]` |
| `POST /api/projects` | 创建项目：`name` 必填，`description` 可选 | `201 Project` |
| `GET /api/projects/{id}` | 查看项目：`id` 为项目编号 | `Project`，包含仓库列表 |
| `PATCH /api/projects/{id}` | 更新项目名称、开发模式、目标分支、拉取分支白名单、自动预开发和自动化规则；仅传需要修改的字段 | 更新后的 `Project` |
| `POST /api/projects/{id}/repos` | 添加仓库：`name`、`remoteUrl`、`provider`、`ownerRepo`、`defaultBranch`、`testCommand` | `201 Repo` |
| `POST /api/projects/{id}/sync` | 从项目仓库同步远端 Issue | 同步结果，含 `imported` |
| `GET /api/projects/{id}/pull-status` | 查询各本地仓库的分支、工作区状态与是否允许拉取 | `RepoPullStatus[]`：`repoId`、`repoName`、`local`、`branch`、`targetBranch`、`clean`、`canPull`、`reason` |
| `POST /api/projects/{id}/pull` | 刷新镜像，并在当前分支等于目标分支且工作区干净时快进本地 | `{ ok, pulledRepos, repos, message }`；失败仓库含 `reason`/`error` |
| `DELETE /api/projects/{id}` | 删除项目；需提供与项目名称一致的确认名称，属于不可逆操作 | `{ ok, deleted }` |

## Issue、执行与审查

| 入口 | 用途与主要参数 | 成功返回 |
| --- | --- | --- |
| `GET /api/projects/{id}/issues` | 获取项目看板任务 | `Issue[]` |
| `POST /api/projects/{id}/issues` | 创建本地任务：`title` 必填，`body`、`labels`、`repoId` 可选；`repoId` 指定项目内的单个目标仓库 | `201 Issue`，包含 `repoId` |
| `PATCH /api/issues/{id}` | 修改任务标题、正文或状态 | 更新后的 `Issue` |
| `POST /api/issues/{id}/publish` | 将本地任务发布到仓库平台：传 `{ repoId }` | 更新后的 `Issue` |
| `POST /api/issues/{id}/runs` | 启动一次 Agent 执行：可传 `agent`、`prompt` | `Run` |
| `GET /api/runs/{id}/events?after={seq}` | 获取执行日志；首次最多返回最近 500 条，`after` 为已看到的事件序号时增量补齐 | `RunEvent[]` |
| `POST /api/runs/{id}/message` | 向 Agent 追加指令：传 `{ text }` | 操作结果 |
| `POST /api/runs/{id}/permissions/{requestId}` | 审批 Agent 操作：传 `{ optionId }` | 操作结果 |
| `GET /api/runs/{id}/changes` | 按仓库查看文件变动 | `RepoChanges[]` |
| `GET /api/runs/{id}/file?repoId=&path=&side=` | 读取工作区或基础分支文件；`side` 为 `work` 或 `base` | `{ content }` |
| `PUT /api/runs/{id}/file?repoId=&path=` | 向 Run 工作区写入文件：传 `{ content }` | 操作结果 |
| `POST /api/runs/{id}/submit` | 审核后提交：`mode` 为 `merge_local`、`pr` 或 `push`，可传标题、提交消息和目标分支 | 各仓库的提交/PR 结果与错误信息 |
| `POST /api/runs/{id}/discard` | 放弃执行并清理工作区 | 操作结果 |

## PR、通知与扩展

| 入口 | 用途与主要参数 | 成功返回 |
| --- | --- | --- |
| `GET /api/projects/{id}/prs?state={state}` | 按 `open`、`merged`、`closed`、`all` 查看 PR | `RemotePR[]` |
| `POST /api/projects/{id}/prs/{number}/merge` | 在指定仓库合并 PR：传 `{ repoId, method }`，`method` 为 `squash`、`merge` 或 `rebase` | `{ ok, merged, message }` |
| `POST /api/projects/{id}/prs/{number}/close` | 在指定仓库关闭 PR：传 `{ repoId }` | `{ ok, closed }` |
| `GET /api/monitor/config` / `PATCH /api/monitor/config` | 查看或修改监控设置：`enabled`、`syncIssues`、`syncIntervalMin`、通知渠道等；读取时机密字段会脱敏 | `MonitorConfig` |
| `POST /api/monitor/sync` | 手动同步全部项目的远端 Issue | `{ ok, projects, imported, errors?, message }` |
| `GET /api/notifications` | 获取应用内通知 | `Notification[]` |
| `POST /api/mcp` | 使用 MCP Streamable HTTP 调用工具：请求体为 JSON-RPC，受接口鉴权约束 | JSON-RPC 响应 |
| `GET /api/plugins` | 查看已安装插件和状态 | `PluginInfo[]` |

WebSocket `GET /api/ws` 推送 Run、Issue、通知与状态事件。插件、Hook 与自动化动作的事件契约见 [docs/PLUGINS_AND_HOOKS.md](docs/PLUGINS_AND_HOOKS.md)。

## 本次评审记录

- 优点：项目将 API、执行器、存储、Git 平台适配和 Web 界面分开；已有 HTTP 接口测试、静态检查和历史评审记录。
- 已处理：仓库名称与 Run 工作区路径校验、文件读写符号链接逃逸、跨项目 PR/Issue/Cherry-Pick 仓库归属校验、代码拉取结果逐仓反馈、Git 状态读取失败按不可拉取处理、旧配置不再覆盖显式 `syncIssues: false`/`syncIntervalMin`、Issue 自动同步受监控总开关约束、仍被 Issue 引用的仓库禁止删除。
- 验证：`go test -count=1 ./...`、`go test -race ./...`、前端 `tsc --noEmit` 全部通过；新增测试覆盖符号链接逃逸、路径穿越、跨项目操作、拉取快进/分支不一致/状态失败、配置迁移与仓库删除保护。
