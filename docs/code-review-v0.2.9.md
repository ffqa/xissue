# xIssue 深度代码 Review 报告

- **审查日期**: 2026-10-19
- **审查版本**: v0.2.9 (commit `21e1578`)
- **范围**: 全部 69 个受版本控制文件 —— Go 后端 ~9.6k 行、React 前端 ~5.6k 行、CI/脚本/部署 ~455 行
- **方法**: 后端（server/runner/store/gitx/provider/automation/acp/cmd/CI）逐文件人工通读；前端（web/src）全量通读；本地执行 `go vet ./...`、`go build`、`go test -race ./...` 全部通过
- **分级**: Critical（需立即修复）/ High（尽快修复）/ Medium（计划修复）/ Low（择机处理）

---

## 总体评价

代码质量高于同类个人工具的平均水平：

- SQL 全参数化，无注入面；
- git CLI 封装有系统性注入防护（`ValidateBranchName`/`ValidateSHA`、`--` 分隔符、拦截 `ext::` 与 `-` 前缀 remote）；
- `resolveRepoFile`（runner.go:1398-1436）有完整的路径穿越 + symlink 逃逸防护；
- 前端无 `dangerouslySetInnerHTML`，Markdown 走 react-markdown v9 默认安全配置（丢弃原始 HTML + `urlTransform` 过滤 `javascript:`）；
- 架构与 docs/design.md 一致性很好；SQLite 单写者（`MaxOpenConns=1` + WAL）简单可靠；
- 前端 react-query 单一数据源 + 单例 WS 写缓存 + `mergeEvents` 按 seq 去重的实时架构方向正确。

主要短板集中在三块：

1. **安全边界**："默认无鉴权 + 只听 127.0.0.1" 的信任模型没有覆盖浏览器发起的跨站攻击面（CSRF / CSWSH / DNS rebinding）；
2. **生命周期与并发细节**：run/issue 状态机、取消路径、多仓提交部分失败等场景有多处真实 bug；
3. **宣传与实现脱节**：`xissue mcp --install`、Layer 2 项目 Hooks、MCP 目标清单三处文档描述的功能实际不工作或不完整。

---

## 🔴 Critical

### C1. 默认无 token 时，任意网页可跨站操纵/破坏整个服务（CSRF）

**位置**: `internal/server/server.go:59-88`（authMiddleware）及全部路由

鉴权中间件只校验 Bearer/Cookie，无 Origin/Host 检查、无 CSRF token。浏览器"简单请求"（GET、POST 表单编码、DELETE、HEAD）不触发 CORS 预检，用户访问恶意网页时，该页面可直接对 `127.0.0.1:7789` 执行：

- `DELETE /api/projects/{id}`、`/api/issues/{id}`、`/api/runs/{id}`、`/api/agents/{name}` —— **静默删除数据**（这些 handler 不读 body、不查 Origin）；
- `POST /api/projects/{id}/batch-run` —— handler 忽略 body 解析错误（server.go:977 `_ = readJSON(...)`），空 body 也会用默认 agent 跑全部 Todo/Backlog issue，**消耗真实 token/费用**；
- `POST /api/restart`（不读 body）—— 远程重启服务；
- `POST /api/mcp/install`（忽略 body 错误）—— 改写本机 IDE/Agent 配置文件；
- `POST /api/monitor/trigger`、`/api/monitor/prune`、`/api/notifications/read-all`。

配合 **DNS rebinding**（无 Host 白名单，攻击者域名重绑到 127.0.0.1 后即同源），还能读取所有 API 响应，并通过 `PUT /api/agents/{name}` 写入任意 `command`/`args`/`env` 再 StartRun → **未认证 RCE**。

**建议**:
1. Host 头白名单（仅 `127.0.0.1:7789` / `localhost:7789` / 配置的 Tailscale 地址）；
2. 所有状态变更端点要求自定义头（如 `X-XIssue: 1`，跨站简单请求无法携带）或默认启动时生成随机 token 写入 config；
3. `batch-run` 等忽略 body 错误的 handler 改为强制 JSON Content-Type。

### C2. WebSocket 关闭 Origin 校验，任意站点可窃听全部事件流（CSWSH）

**位置**: `internal/server/ws.go:71-73`

```go
conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
    InsecureSkipVerify: true, // local / Tailscale origins
})
```

任何网页都可 cross-site 连接 `/api/ws`，实时接收 issue 正文、agent 执行日志、权限审批请求、通知内容。默认无 token 时无任何门槛；配置 token 后 SameSite=Lax cookie 可挡住跨站 WS，但默认配置下是完全开放的数据外泄通道。

**建议**: 移除 `InsecureSkipVerify`，改用 `OriginPatterns: []string{"127.0.0.1:*", "localhost:*", "<tailscale-host>"}`。

### C3. 自更新无任何完整性校验（供应链风险）

**位置**: `cmd/xissue/update.go:120-186`

`xissue update` 从 GitHub API 返回的 `browser_download_url`（未做域名白名单）下载压缩包，**不校验 SHA256、不验签**，直接替换常驻服务二进制并重启 —— 而 CI 明明已发布 `SHA256SUMS.txt`（release.yml:88）。仓库或发布链路一旦被攻破，所有执行过 update 的机器直接 RCE。

**建议**: 下载后强制比对同 release 的 `SHA256SUMS.txt`；`browser_download_url` 限定 `github.com` / `objects.githubusercontent.com` 域。

### C4.（前端）WS 无心跳、无兜底轮询，半开连接时 UI 静默冻结

**位置**: `web/src/realtime.ts:122-157`

无应用层 ping/pong、无 `visibilitychange` 复活检测；TCP 半开时 `onclose` 不触发，界面仍显示"实时已连接"，而 events/runs/issues 查询均无轮询兜底（仅 App.tsx:34 的通知有 60s 轮询）。长时间运行的 agent 日志会停更且用户无感知。

**建议**: WS 心跳（超时未收到 pong 主动 close 重连）+ WS 非 open 状态时对活跃 run 开 `refetchInterval`。

---

## 🟠 High

### H1. README 主打命令 `xissue mcp --install` 实际不生效

**位置**: `cmd/xissue/main.go:74` vs `README.md:26`

代码只匹配 `flag.Arg(1) == "install"`（无横线）；`xissue mcp --install` 的 `Arg(1)` 是 `--install`，不匹配 → 落入 `runMCPStdio`，进程静默挂起等待 stdin，用户以为安装完成实际什么都没发生。Makefile:46 用的是 `mcp install`（正确）。main.go:37 的 usage 也写着 `[--install]`，自相矛盾。

### H2. v0.2.9 宣传的 Layer 2 项目 Hooks 在 PR 自动化流程中是死代码

**位置**: `internal/runner/watcher.go:821-824` + `internal/automation/engine.go:114-121`

PR 自动化只对 open PR 的 run 触发，而此时 `run.Workdir` 已被 Submit 清空（runner.go:1299），代码回退到 **bare mirror 路径**（`~/.xissue/mirrors/repo-N.git`）下找 `.xissue/hooks/` —— bare 仓库没有工作树，脚本永远不存在，hooks 永远静默 no-op。`DetectProjectHooks`（watcher.go:992-999）同样指向 mirror，UI 永远显示"无 hooks"。

**附加风险**: 一旦修复路径（例如指向 repo 本地目录或 worktree），"任意外部人评论 PR 即触发执行仓库内脚本 + 自动 `chmod 0755`"（engine.go:195-201）会变成外部可触发的 RCE 面。修复时必须加评论作者/来源门控（信任白名单）。

### H3. `allow_always` 语义过宽：一次批准 → 该 run 后续所有操作全自动放行

**位置**: `internal/runner/runner.go:1021-1026, 1040-1057`

用户对某一个权限请求选 "allow_always" 后，`r.allowSet[runID] = true`，此后 `autoApproveOption` 对该 run 的**一切 kind**（包括 execute/delete）都自动批准，只剩 `AlwaysAskPatterns` 子串匹配兜底（`git push` 的变体如 `git -c x.y=z push`、`git  push` 即绕过）。与设计文档"按 kind 白名单"意图不符。

**建议**: allow_always 按 (kind) 或 (kind, 命令前缀) 粒度记录；AlwaysAskPatterns 改为正则或命令词法匹配。

### H4. Submit 部分失败仍删除全部 worktree，可丢失未推送的代码

**位置**: `internal/runner/runner.go:1263-1297`

多仓 run 中只要任一仓 push 成功（`anyPushed`），`cleanupRunWorkdir` 会对**所有**仓库执行 `worktree remove --force` + `RemoveAll`；commit/push 失败的仓库中未提交的修改直接丢失（ArchiveRun 只保存文本日志，无 diff/patch）。

**建议**: 仅清理成功仓库；失败仓库保留 worktree 并在结果中提示路径。

### H5. `MergeBranchToTarget` 忽略合并失败 + `branch -f` 可能丢提交

**位置**: `internal/gitx/gitx.go:483-497`

本地直合路径（merge_local 模式）：

1. `merge --ff-only` 失败后回退的 merge commit 用 `_, _ =` 吞掉错误 —— 冲突时仍返回 `(headSHA, nil)`，Submit 报告 "(已直接合入 xxx)" 但实际没合入；
2. 目标目录工作区有未提交修改、或当前不在目标分支时，直接 `git branch -f target source`，目标分支上独有的提交被静默抛弃（仅 reflog 可救）。

**建议**: 检查 merge 错误并如实上报；`branch -f` 前验证目标分支是 source 的祖先（可 ff），否则报错。

### H6. `act.cancel` 数据竞争

**位置**: `internal/runner/runner.go:739`（executeTurn goroutine 无锁写）vs `runner.go:687-689`（Cancel 无锁读）

启动瞬间点取消可能读到 nil → `act.cancel()` 不被调用，取消失效，run 继续跑。`go test -race` 未覆盖到该路径。

**建议**: 把 cancel 的创建挪进 `reserveActiveRun`（持锁创建 activeRun 时一并赋值）。

### H7. 删除 issue 不取消运行中的 agent

**位置**: `internal/server/server.go:541-567`

`DELETE /api/issues/{id}` 与 `POST /api/issues/batch-delete` 只删 DB 行（FK 级联删 runs/events），活动 run 的 agent 进程继续运行烧 token，后续事件因 FK 失败静默丢弃，`active` map 中的条目直到 turn 结束才清除。

**建议**: 删除前查 `ListRuns` 中 active 状态的 run 并逐个 `runner.Cancel`。

### H8. budget 取消后 issue 永久卡在 Running

**位置**: `internal/runner/runner.go:756-758`

排队等内存预算时被取消（`budget.acquire` 返回 ctx 错误），只把 run 置为 cancelled，issue 状态不回退（StartRun 已置 Running）；无服务重启则永远卡住（重启恢复逻辑只处理 active run 状态）。

### H9. ACP 回调 panic 会击穿整个 server 进程

**位置**: `internal/agent/acp/client.go:385-415`

`handleNotification`/`stderrLoop` 在 readLoop goroutine 同步调用 `OnEvent`，`handleRequest` 另起 goroutine 调 `OnPermission` —— 两者都没有 recover；`executeTurn` 的 recover 只保护自己的 goroutine。事件处理链（emitEvent → DB → broadcast）上任何 panic = 整个服务进程崩溃。

**建议**: readLoop / handleRequest / stderrLoop 的回调调用处包一层 `defer recover()`，转为 failRun。

### H10.（前端）Monaco 未保存编辑被静默覆盖

**位置**: `web/src/pages/IssuePage.tsx:857-860`

`draft` 无 dirty 守卫地跟随 `workQ.data.content` 重置，叠加全局 `refetchOnWindowFocus: true`（main.tsx:28）与 agent 同时改同一文件，用户未保存的编辑被吞。

**建议**: `if (dirty) return` 守卫 + 对 file 查询单独关闭 focus refetch。

### H11.（前端）拖卡片/切状态到 Running 无确认直接启动 agent

**位置**: `web/src/pages/ProjectPage.tsx:399-411`、`web/src/pages/IssuePage.tsx:292-299`、`ProjectPage.tsx:293-300`

- 拖卡片入"执行中"列立即启动 Agent（HTML5 DnD 误拖高发），无确认、失败无回滚、拖到其他列无乐观更新（卡片原地不动直到服务端返回）；
- 状态下拉选 `running` 同样直接 `startRun.mutate()`；
- "自动执行待办"对整列无上限并发启动 Agent，无确认。

一次误操作 = 创建 worktree + 消耗 token。建议全部加确认对话框 + 乐观更新 + onError 回滚。

### H12. `/api/fs/browse`、`/api/fs/inspect` 可枚举主机任意目录

**位置**: `internal/gitx/inspect.go:49-75` + `internal/server/server.go:248-264`

无路径白名单，`?path=~/.ssh` 可列出私钥文件名（虽不读内容）。默认无 token 时是重要侦察面（配合 C1 的 DNS rebinding 可读取响应）。

**建议**: 限制在用户配置的允许根目录内，或至少要求 token。

---

## 🟡 Medium

| # | 位置 | 问题 |
|---|---|---|
| M1 | provider/github.go:199-214 | `GetPRDetails` 构造 PRComment/PRReview 时丢失 `CreatedAt`/`SubmittedAt`；当 gh 不返回 id 时，自动化去重 eventKey 退化为 `author + 零值时间`（watcher.go:835），同作者后续评论被误判"已处理"，自动化漏触发 |
| M2 | automation/engine.go:72-76 | `ReviewState` 条件在 comment 事件（review==nil）时被跳过而非判 false —— 配了 `reviewState: approved` 的 pr_comment 规则可被任意普通评论命中并触发 merge_pr |
| M3 | runner.go:465-503 | StartRun 的"同 issue 已有活动 run"检查与 `reserveActiveRun` 之间有 TOCTOU，并发请求可为同一 issue 创建两个活动 run；reserve 失败时还会留下孤儿 queued run 记录 |
| M4 | runner.go:976-1033 | `handlePermission` 等待用 `c.ctx`（进程级）而非 turn ctx，取消依赖 Close 兜底；`allowSet` 仅在 Discard/Delete 清理，run 正常完成即泄漏条目 |
| M5 | gitx/gitx.go:120-139, 328-342 | `BranchTip` 复用 `resolveBaseRef` 回退链 —— watch 一个不存在的分支会静默拿 main/master 的 tip 当基准，分支创建后触发一堆无关提交通知 |
| M6 | server.go:771-788 | `GET /api/runs/{id}/archive` 在 GET 请求里触发写盘副作用（违反幂等语义），且把无上界的 events.log 整个读入 JSON 响应，长 run 可达数十 MB |
| M7 | runner/watcher.go:120-146 | 分支/提交下拉每次请求都 `EnsureMirror`（含网络 fetch）；90s 轮询持 `watchMu` 执行网络调用，与手动触发/merge 后的同步检查互相阻塞，UI 卡顿 |
| M8 | watcher.go:690-698, 776-799 | monitor.log 每条日志起一个 goroutine 异步 append，与 prune 的整文件读改写并发竞争，可丢行/交错 |
| M9 | acp/proc_unix.go:22-25, proc_windows.go:9-13 | Unix killGroup 的 2s 延迟 SIGKILL 打到 `-pid` 进程组，进程早已退出时理论上可误杀复用的 pgid；Windows 只杀主进程不杀进程树，agent 子进程泄漏 |
| M10 | server/restart_windows.go:5 | `execSelf` 在 Windows 是 no-op → `/api/restart` 变成"退出不自启"，而 release 明确发布 windows 包 |
| M11 | cmd/xissue/service.go:184 | `pkill -f <persistentBin>`：从 `~/.local/bin/xissue` 再次执行 `service install` 时命令行匹配自身 → 自杀，install 中断在 bootstrap 之前，服务未加载 |
| M12 | automation/engine.go:306-355 | `plugin.yaml` 用 `json.Unmarshal` 解析 —— 合法 YAML 清单静默失败，回落为 `Enabled=true` + 默认事件（意外启用）；`getPluginEntrypoint` 的 entrypoint 未做 `..`/绝对路径校验；hook/plugin 执行未设 `cmd.Dir`（脚本相对路径全错） |
| M13 | .github/workflows/release.yml:57-58, 94 | `workflow_dispatch` 的 tag 输入直接内插进 shell（`VERSION="${{ github.event.inputs.tag }}"`）→ CI 命令注入模式（需维护者权限，但应经 `env:` 传递加固）；:46 `pnpm install --no-frozen-lockfile` 放弃 lockfile 保障 |
| M14 | go.mod:3 vs release.yml:40 | go.mod 声明 `go 1.26.0`，CI 安装 1.24，靠 GOTOOLCHAIN 自动下载兜底 —— 隐式且拖慢 CI，应升到 1.26 |
| M15 | server/mcp.go:450-475 | `injectJSONMap` 全量重写 `~/.claude.json` 等大文件；tmp 0644 + rename 会把原 0600 权限放宽为 0644（该文件可能含凭证）；与 IDE 并发写有互相覆盖风险 |
| M16 | store/store.go:446-462 | BatchDeleteIssues 占位符无分批，>999 个 id 触发 SQLite 变量上限直接报错 |
| M17 | web/src/realtime.ts:84,97 | 每条 `run_updated`/`issue_updated` 都先 `setQueryData` 再 `invalidateQueries`，批量运行时形成 refetch 风暴；应仅缓存缺失/终态时 invalidate 或节流合并 |
| M18 | web/src/pages/ProjectsPage.tsx:50-75 | 建项目后串行 addRepo，中途失败仅 toast，留下半导入项目，用户重试会创建重复项目；应后端一次性批量导入或失败自动清理 |
| M19 | web/src/components/AgentManagerModal.tsx:41,458-467 + runner.go:186-195 | Agent `env`（含 `OPENAI_API_KEY` 等）经 `GET /api/agents` 明文下发并回显 textarea —— 后端应脱敏（`***`），留空表示不变 |
| M20 | web/src/pages/ProjectPage.tsx:481-489, 1052-1075 | 设置弹窗把 project 快照进本地 state，保存时整体 PATCH，期间 WS/他人改动被覆盖（lost update），关闭不提示未保存 |
| M21 | web/src/components/EventStream.tsx:331-343 | 每收到一个事件就全量 `buildView` 重算 + 整列表重渲染（长 run 近似 O(n²)），子组件无 memo，`ToolItem` 被就地 `Object.assign` 修改；应增量构建 + memo + 虚拟滚动 |
| M22 | web/src/api.ts:42-73 + realtime.ts:126 | 前端完全不处理鉴权：不发 `Authorization`、不识别 401，WS URL 不带 token；token 缺失/过期时无限 toast `unauthorized` + 无限指数重连 |
| M23 | web/src/pages/ProjectPage.tsx:276-280 | 仅为渲染角标数字，每次进页面/窗口聚焦都调 `/projects/:id/prs`（真实打 GitHub API），易触发限流并拖慢首屏；应设 staleTime 或后端提供轻量 count 接口 |

---

## 🟢 Low（择要）

**文档与发布**

- README.md:86 宣称 MCP 支持 VS Code Copilot / Gemini CLI / Antigravity / Cline / Roo Code / Zed，实际 `InstallMCPTargets`（mcp.go:337-350）只有 opencode/claude/qoder/cursor/windsurf 5 个目标；
- docs/api.md 停留在 M1 时期：mcp / notifications / cherry-pick / batch-run / monitor / plugins / restart 等端点 0 记载（实际约 60 条路由）；
- Formula/xissue.rb 仍是 0.2.8（server.go:25 已 0.2.9）；`Version` 是 const 无法 ldflags 注入，workflow_dispatch 的 tag 与二进制内版本可能不一致，触发 update 死循环；
- fresh clone 无法直接 `go build`/`make dev`：`internal/server/dist` 未入库（.gitignore:6 的 negation 没有配套提交 index.html），`go:embed all:dist` 编译失败，必须先构建前端。

**update.go**

- :164 `strings.Contains(exePath, "tmp")` 会误判 `/Users/tmpuser/...` 之类路径；
- :260-290 下载失败时临时文件泄漏（`defer os.Remove` 只覆盖成功路径）；
- :188-200 `0.2` vs `0.2.0` 误判为新版本（`len(lParts) > len(cParts)`）；
- 解压无大小上限（zip bomb 场景，归档来自 GitHub 风险低）。

**server/store**

- main.go:160-163 MCP stdio 桥对非 200 响应静默吞掉（不写 stdout），MCP 客户端挂死无提示；
- server.go:65,81 token 用 `==` 比较（建议 `crypto/subtle.ConstantTimeCompare`）；`?token=` 会留在浏览器历史/访问日志；
- ws.go:32-37 hub 通道满（256）即丢事件，客户端无感知（前端 seq 增量拉取可部分弥补，但见 C4）；
- watcher.go:717 `MonitorStatus.Running: true` 硬编码；
- store.go:29 DSN `fmt.Sprintf` 拼接未转义路径中的 `?`/`#` 字符；
- store.go:39-47 增量迁移靠忽略 `ALTER TABLE` 报错，无 schema 版本号管理；
- server.go:419-465 handleSyncProject 内层 `p := provider.For(...)` 遮蔽外层项目变量 `p`；多仓中途失败时已导入 issue 保留但客户端只见 502。

**runner/gitx**

- gitx.go:229-233 未跟踪文件全量 `os.ReadFile` 数行数，大二进制文件浪费内存；
- gitx.go:242-249 `ShowBase` 吞掉所有错误统一当"新文件"处理；
- runner.go:1502-1503 "Do NOT run git push" 等安全规则仅为 prompt 层约束，无强制；
- runner.go:301-421 ListAgentModels 同步阻塞最长 12s+800ms（有 10min 缓存兜底）；
- automation MatchRule 的 `Author` 留空即匹配任意评论者 —— 规则配置 UI 应显著警告。

**前端（详见前端清单）**

- 剪贴板 `clipboard.writeText` 多处未 await/catch 仍提示"已复制"（CherryPickModal:163,423、MonitorModal:56、AgentManagerModal:285、IssuePage:417,731,1086,1314、ProjectPage:1622）—— 非安全上下文（`-listen 0.0.0.0` 走 http）必失败；
- `editorUrl` 与 `href={issue.remoteUrl|pr.url}` 无 scheme 白名单（common.tsx:102-109、IssuePage:444），`javascript:` 残余风险；
- MonitorModal.tsx:118 硬编码"后台轮询正常 (90s)"无视后端 `status.running`；:62-73 backdrop onClick 误关 + 标题写 "(Esc)" 却无键盘处理；:334 日志 `key={idx}` + 5s 整表重建丢滚动位置；
- common.tsx:39-43 Modal 用 window 级 Escape 无栈管理，嵌套弹窗一次 Esc 全关；无 focus trap/`aria-modal`；
- IssuePage.tsx:52-62 路由参数变化组件复用，`selectedRunId` 不随 issue 重置；:826-834 已消失文件仍被选中致 diff 404；
- CherryPickModal.tsx:84-96 注释称"未手工编辑才填充"但无守卫，commits refetch 覆盖用户手改内容；
- LocalDirPicker.tsx:22-38 目录浏览无取消/卸载保护，快速点击旧响应覆盖新目录；:102 `divideY` 非 CSS 属性（无效样式）；
- ProjectPage.tsx:153 自动化规则正则无校验，非法 pattern 保存后静默失效（Go RE2 与 JS 正则方言不同，应走后端 dry-run 校验）；
- toast.tsx:19-27 无按文本去重，网络抖动时错误 toast 刷屏；
- types.ts:156 `RunEvent.data: any`，事件契约脆弱；
- PRListModal.tsx:266-282 `isPending` 禁用全部按钮；收到 `pr_status` 通知时列表不刷新（realtime.ts:105 只 invalidate watches）；
- App.tsx:211-217 重启后的 `setTimeout` 未清理。

---

## 测试与工程化

- **覆盖率**: 10 个 Go 包中仅 3 个有测试（gitx / store / runner-archive）。runner.go 1505 行核心调度逻辑 0 覆盖 —— H6 竞争、H8 状态卡死、H4 部分失败清理恰在无测试区；server / acp / automation / provider / config 全无测试。
- gitx_test.go 质量不错（真实 git 仓库集成测试，覆盖 mirror/worktree/merge 主路径），可作为补测范本。
- CI 有 `go test -race`（好），前端只有 tsc 类型检查，无 eslint/单测/E2E。
- `go vet`、`go build`、`go test -race ./...` 当前全部通过。

---

## 修复优先级建议（Top 10）

| 序 | 项 | 工作量 | 说明 |
|---|---|---|---|
| 1 | C1+C2: Host/Origin 白名单 + 状态变更端点自定义头 | ~半天 | 消掉最大攻击面，不影响现有前端（同源自动带头） |
| 2 | C3: update 校验 SHA256SUMS | ~1h | CI 已产出校验文件，只差下载比对 |
| 3 | H1: `--install` 兼容 + 文档对齐 | ~10min | 一行 `strings.TrimLeft(arg, "-")` 级别修复 |
| 4 | H4+H5: Submit 仅清理成功仓库；MergeBranchToTarget 检查合并错误、禁用裸 `branch -f` | ~半天 | 防数据/提交丢失 |
| 5 | H7+H8: 删 issue 先 Cancel；budget 取消回退 issue 状态 | ~1h | 防 token 浪费与状态卡死 |
| 6 | H6: `act.cancel` 挪进 `reserveActiveRun` 持锁赋值 | ~10min | 顺带补一个 -race 并发取消测试 |
| 7 | H9: acp 回调加 recover | ~30min | 防单事件 panic 击穿全服务 |
| 8 | C4+H10: 前端 WS 心跳 + Monaco dirty 守卫 | ~半天 | 实时可靠性与编辑安全 |
| 9 | H2: Hooks 指向真实工作区 + 评论作者门控（或暂时下线该功能） | ~1天 | 修复或收敛 v0.2.9 宣传功能 |
| 10 | H3+M19: allow_always 收窄到 kind 粒度；agents API env 脱敏 | ~半天 | 权限模型与凭证暴露 |

---

## 附录：验证记录

```
$ go vet ./...            # 无输出（通过）
$ go build -o /tmp/xissue-review-build ./cmd/xissue   # 通过
$ go test -race ./...
ok  github.com/xpier/xissue/internal/gitx     2.784s
ok  github.com/xpier/xissue/internal/runner   2.885s
ok  github.com/xpier/xissue/internal/store    3.456s
（其余包 no test files）
```

*报告生成: 2026-10-19，基于 commit 21e1578 (v0.2.9)。*
