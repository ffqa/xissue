# xIssue 深度代码 Review 报告（第二轮 · v0.2.14）

- **审查日期**: 2026-10-21
- **审查版本**: v0.2.14 (commit `3295c72`)
- **对比基线**: `a8f4990`（2026-10-19 第一轮 review 后修复版）
- **改动规模**: 21 个提交、67 个文件、+7263/-481 行（v0.2.10 → v0.2.14：知识库/GitHub Wiki、自动预开发、branch_push 自动化、Telegram/Webhook 通知、hook 进程沙箱、项目删除全量清理、服务管理重写、示例插件）
- **方法**: 后端逐 diff 人工审查 + 隔离实例（:17789）运行时攻击探测；前端（web/src 增量 ~2400 行）全量通读；本地 `go vet`、`go build`、`go test -race -count=1`、前端 `tsc --noEmit`
- **上一轮报告**: `docs/code-review-v0.2.9.md`

---

## ✅ 上一轮遗留项修复验证（实测确认）

| 上轮遗留项 | 状态 | 证据 |
|---|---|---|
| WS `?token=` 首连 401（query token 不放行 /api/） | ✅ 已修 | `bd5fadd`；实测 `GET /api/projects?token=` → 200 |
| batch-run chunked body（ContentLength=-1）跳过解析 | ✅ 已修 | `ContentLength != 0` + `io.EOF` 容忍；X-XIssue 门仍在 |
| 配置文件并发读写竞争 | ✅ 已修 | config.go 全面 RWMutex + 防御拷贝（`Agent`/`AgentsSnapshot`/`PermissionsSnapshot`）+ 原子落盘（tmp + chmod 600 + Sync + rename） |
| hook/plugin 子进程树失控 | ✅ 已修 | automation/proc_unix.go：Setpgid + 进程组 SIGKILL + `cmd.WaitDelay = 2s` |
| 插件 manifest 解析失败仍默认启用 | ✅ 已修 | 读取/解析失败一律 `Enabled=false` 并附错误描述（fail-closed） |
| 跨仓库 hook workdir 回退（A 项目执行 B 仓库脚本） | ✅ 已修 | 回退链移除；branch_push 场景硬性限定 `IsLocalDir(repo.RemoteURL)` |
| fresh clone 无法编译（embed dist 缺失） | ✅ 已修 | `internal/server/dist/.gitkeep` 入库 + Makefile touch + `.gitignore` 改 `/dist/` |
| service restart 挂死 / killPort 误杀客户端连接 | ✅ 已修 | kickstart 优先 + 端口就绪轮询；killPort 只匹配 `-sTCP:LISTEN`，SIGTERM→等待→SIGKILL 分级 |
| Brew Cellar 版本路径写死（升级后服务失效） | ✅ 已修 | `resolveStableBrewBinary` 返回版本无关软链接 + `checkAndMigrateCellarService` 自动迁移旧配置 |
| CI Homebrew Tap 推送失败被静默吞掉 | ✅ 已修 | `f9c8532`：移除 `\|\| echo skip`，缺 token 显式 warning，失败直接红 |
| 测试覆盖仅 3/10 包 | ✅ 大幅改善 | **8 个包带测试**（cmd/automation/config/gitx/knowledge/runner/server/store），含 security_test.go、server_docs_test.go，全部通过 `-race` |

### 运行时安全矩阵（token 模式，隔离实例实测）

```
GET /api/health 无鉴权            → 401 ✓
伪造 Host: evil.com               → 403 ✓ (DNS rebinding 防护)
GET /api/projects?token=<valid>   → 200 ✓ (query token 放行)
POST 无 X-XIssue（带 Bearer）      → 200 ✓ (Bearer 等效凭据)
POST 无任何凭据                    → 401 ✓
```

### 知识库路径穿越攻击探测（5/5 全部拦截）

| 攻击 | 结果 |
|---|---|
| `docId=local:1:../../../tmp/xtest/secret/keys.md` | 404 "文档路径越界" ✓ |
| symlink 逃逸 `docs/escape/keys.md`（escape → 仓库外目录） | 404 "通过符号链接逃逸出允许目录" ✓ |
| 绝对路径 `local:1:/etc/passwd` | 404 "不允许使用绝对路径" ✓ |
| PUT save 穿越写入 `../../../tmp/xtest/secret/pwned.md` | 500 拒绝，目标目录未被写入 ✓ |
| 目录扫描 `GET /docs` | 不跟随 symlink、不泄漏仓库外文件 ✓ |

`secureDocPath`（knowledge.go:470-498）的逐级 EvalSymlinks 上溯实现质量很高：拒绝绝对路径/卷标/NUL/`..`，Clean 后校验包含关系，再对第一个存在的祖先做 symlink 解析并复核边界。

### 静态检查

```
go vet ./...                  → 无输出（通过）
go build ./cmd/xissue         → 通过
go test -race -count=1 ./...  → 8 包全部 ok
web: tsc --noEmit             → exit 0
```

---

## 本轮新发现

风险重心已从上一轮的"注入/穿越"转移到**破坏性与自动化操作的默认值与确认强度**。

### 🔴 Critical

#### N-C1. 一键落库的 catch-all 自动化规则

**位置**: `web/src/pages/ProjectPage.tsx:161-170, 1781-1815`

新建规则默认 `branch_push` 触发、branch/author/pattern **全空（= 匹配所有分支所有推送）**，`create_issue`/`pull_local`/`notify` 默认勾选且 `enabled: true`，点一次"加入规则列表"即经 `persistRules` PATCH 落库立即生效，零复核。配合后端 `trigger_run` 动作 = 之后每次任何推送自动创建 issue 并启动 agent run（烧 token）。

**建议**: 新规则默认 `enabled: false`；branch_push 规则无 branch 条件时强制填写或二次确认。

#### N-C2. 知识库编辑态 Esc 双重触发，未保存内容直接丢弃

**位置**: `web/src/components/KnowledgeBaseModal.tsx:151-171` + `web/src/components/common.tsx:43-52`

两处都在 window capture 阶段注册 Escape 监听，`stopPropagation()` 拦不住**同一节点**上的其它监听器（需 `stopImmediatePropagation()`）。编辑文档时按 Esc 会同时执行 `setIsEditing(false)` 和 Modal 的 `onClose()`——整个弹窗关闭且丢弃未保存内容，注释声称的"Esc 优先退出编辑"不成立。

**建议**: 集中式 Modal/Esc 栈管理（common.tsx 统一注册，按栈顶分发）。

### 🟠 High

#### N-H1. 自动预开发（auto-develop）对公开仓库的滥用面

**位置**: `internal/server/server.go:571-596`（maybeAutoDevelop）+ `internal/runner/autodevelop.go` + `internal/domain/domain.go`（DefaultAutoDevelopLabels）

虽为项目级 opt-in（`auto_develop` 默认 0），但对**公开 GitHub 仓库**启用后形成完整滥用链：

1. 任何外部人可开 issue，标题/正文含默认泛关键词即命中——`bug/fix/error/需求/缺陷/优化/功能/报错/崩溃`（文本子串匹配，非仅 label）；
2. **`store.go` 强制空标签列表回落默认值**（scanProject / UpdateProject 两处 `len==0 → Default`），用户无法配置为"仅按 label 匹配"来收紧；
3. issue body（攻击者可控）直接进入 `buildInitialPrompt` = **提示注入通道**，而默认 `AutoAllowKinds` 含 `edit`，agent 可在 worktree 内无审批改文件；
4. run 完成后 `autoSubmitPR` **自动 push 分支 + 创建 PR**（不合并，管理员审核）；
5. 无速率/预算上限：批量开 issue 可耗尽 token；卡在待审批的 auto-run 长期占用 budget 槽位（`maxConcurrent` 默认 2），**饿死正常任务**。

**建议**: 仅按 label 匹配（GitHub 上打 label 需 triage 权限，天然门控）；允许保存空关键词列表；增加作者白名单与每项目每日 auto-run 上限；auto-develop 的 run 遇权限请求直接标记失败而非占槽等待；README 对公开仓库场景加显著警告。

#### N-H2. Telegram Bot Token / Webhook URL 明文回显

**位置**: `internal/server/server.go`（handleGetMonitorConfig 返回完整 MonitorSnapshot）+ `web/src/api.ts:249`

实测 `GET /api/monitor/config` 原样返回 `telegramBotToken: "BOT-SECRET-123"`。与 agent env 的 `******` 脱敏方案（上轮 M19）不一致；任何能打开 UI 的人（Tailscale 共享、录屏、devtools、react-query 缓存）都能拿到 bot token。

**建议**: 后端照搬 `maskSensitiveEnv` 模式返回 `******`，前端留空表示不修改（AgentManagerModal 已有同款交互可复用）。

#### N-H3. 「暂停监控」按钮可清空已存凭据

**位置**: `web/src/components/MonitorModal.tsx:116-127, 217`

暂停/恢复把**整份表单草稿** PUT 到 `/api/monitor/config`；`configQ` 尚未加载完成时按钮未 disable，`cfgTgToken`/`cfgWebhookUrl` 为 `''` → 直接清空已配置的 Telegram Token / Webhook URL。

**建议**: 暂停操作只发 `{enabled: bool}`（或后端支持 PATCH 语义）；加载完成前 disable 按钮。与 N-H2 一并修复。

#### N-H4. Wiki 文档编辑会被下次同步静默销毁

**位置**: `internal/knowledge/knowledge.go:68-80`（SyncGitHubWiki `reset --hard`）+ `:636-647`（SaveDoc 不区分来源）

`PUT /api/projects/{id}/docs/save` 允许写入 `wiki:` 前缀文档（本地 wiki clone），但保存**既不 commit 也不 push**；下次 `sync-wiki` 执行 `git reset --hard origin/HEAD` 时用户编辑**无提示全部丢失**。

**建议**: wiki 文档在 UI/服务端设为只读；或保存时 commit + push 回 `<repo>.wiki.git`（GitHub Wiki 可写权限即可推送）。

#### N-H5. 删除项目的名称确认在服务端可绕过

**位置**: `internal/server/server.go:456-470`

```go
if confirmName != "" && confirmName != proj.Name { ... 400 ... }
```

不带 name 的 `DELETE /api/projects/{id}` 直接执行 `CleanupProject`（杀 agent 进程、删 worktree/mirror/归档，**不可逆**）+ 级联删库。前端 DeleteProjectModal 有确认，但 API 层一条 curl 即穿透（X-XIssue 头挡住跨站浏览器请求，挡不住本机其它进程/脚本）。

**建议**: 服务端强制要求 name（缺失即 400），确认语义与前端对齐。

#### N-H6.（前端）persistRules 并发 PATCH 竞态

**位置**: `web/src/pages/ProjectPage.tsx:196-208`

连点规则开关产生并发 PATCH，`onSuccess` 无条件 `setQueryData`，慢的旧响应覆盖新状态 → 开关显示与服务端不一致（自动化规则"以为关了其实开着"）。

**建议**: mutation 串行化，或用 `onSuccess(data, variables)` 按 variables 写缓存。

### 🟡 Medium

| # | 位置 | 问题 |
|---|---|---|
| N-M1 | config.go `Default()` | 新增 `ActiveHours: "08:00-20:00"` 默认值：老用户 config.yaml 无 monitor 段 → 升级后**夜间 8 小时监控/PR 同步静默停摆**（PR 半夜合并，issue 次日才流转）。应在 README/升级说明标注，或老配置迁移时默认全天 |
| N-M2 | runner.go `CleanupProject` 步骤 4 | 归档目录用原始 `proj.Name`，而 ArchiveRun 写入用 `sanitizeFileName(proj.Name)` —— 含空格/斜杠/中文标点的项目名删不干净，`~/.xissue/issue/<name>` 泄漏 |
| N-M3 | store.go `DeleteProject` | 通知清理 `LIKE '%/projects/1%'` 过宽——删项目 1 连带匹配 `/projects/12`、`/projects/100` 的通知；且 server 层 CleanupProject 在 DB 删除**之前**执行，DB 失败时磁盘资源已不可恢复 |
| N-M4 | watcher.go `processBranchPushAutomation` | `trigger_run` 未指定 agent 时 `for name := range AgentsSnapshot() { break }` —— map 无序，**每次推送随机选 agent**；应复用 `DefaultAgentName()`（偏好 opencode + 字典序） |
| N-M5 | engine.go `MatchBranchPushRule` | author 条件用 `strings.Contains(actual, expected)` 子串匹配（`bob` 命中 `bobby`）；且 git author name 可被推送者任意伪造，不应作为信任凭据（Layer 2 hook 门控依赖它时尤其危险） |
| N-M6 | service.go:286 | **上轮 M11 未修**：`pkill -f <persistentBin>` 在从 `~/.local/bin/xissue` 执行 `service install` 时匹配自身命令行 → 自杀，安装中断在 bootstrap 之前 |
| N-M7 | release.yml:57-58 | **上轮 M13 未修**：workflow_dispatch 的 tag 输入仍直接内插 shell（`VERSION="${{ github.event.inputs.tag }}"`）→ CI 命令注入模式，应经 `env:` 传递 |
| N-M8 | knowledge.go `SearchDocs` | 每次请求（含 MCP `xissue_search_docs`）全量扫描并**读满所有文档内容**，无缓存无上限；大仓库秒级同步阻塞 HTTP handler |
| N-M9 | knowledge.go:733-742 `extractSnippet` | 按**字节**偏移切割 UTF-8（`content[start:end]`），中文文档 snippet 出现 U+FFFD 乱码；应转 rune 或用 `utf8.RuneStart` 对齐 |
| N-M10 | knowledge.go `ResolveDocFilePath` | `mirror:` docID 返回目录路径，`SaveDoc` 对目录 WriteFile 必然 EISDIR → 500；UI/服务端应禁止编辑 mirror 文档 |
| N-M11 | 前端多处 | 知识库搜索无防抖（每键 POST /docs/search，无 keepPreviousData 闪烁）；syncWiki 无 pending 态可连点并发 clone；`navigator.clipboard` 在非安全上下文（README 宣称的 `http://100.64.x.x:7789` 用法）为 undefined，点击抛 TypeError 仍提示"已复制"（WatchCommitDetailModal.tsx:96、MonitorModal.tsx:111 等） |
| N-M12 | DeleteProjectModal.tsx:25-31 | 删除后 `invalidateQueries` 再导航 → 已删项目的活跃查询 refetch 得 404 弹错误 toast；应用 `removeQueries` |
| N-M13 | IssuePage.tsx:628 | 模型探测 `await` 无 try/catch → unhandled rejection 且无用户提示（AgentManagerModal.tsx:370 同款已修，此处漏网） |

### 🟢 Low

- `plugins/follow-commit-watcher/.watcher_state.json` 运行时状态文件被提交进仓库（`post_merge_count: 1` 为测试残留）；插件把状态写在脚本同目录，安装到 `~/.xissue/plugins` 后无碍，但仓库内不应入库；
- acp/client.go `parseConfigOptions` 改用 `Contains(id, "model")` 宽匹配，可能误捕获含 "model" 字样的无关配置项（有 `len(models)>0` 守卫，风险有限）；
- `escapeTelegramMarkdown` 只转义 `_`，`*`/反引号会破坏 Markdown 渲染（外观问题）；
- knowledge.go:347 `getRepoLocalDir` 把 **project.Description 当目录路径**探测——描述字段兼职路径语义令人意外，建议独立字段；
- docs/detail 路径越界错误映射 404、save 越界映射 500，应统一 400；
- ListAgentModels 探测超时 12s→35s，同步 HTTP 阻塞窗口变大（有 10min 缓存兜底）；
- MonitorStatus.Running 仍硬编码 true（新增 Enabled 字段后语义重复）；
- handleSyncProject 内层 `p := provider.For(...)` 变量遮蔽仍在（可读性）；
- 前端：`<button>` 嵌 `<Link>` 非法嵌套（ProjectsPage.tsx:122-133）、WatchCommitDetailModal 外链 host 未校验/`ownerRepo` 未 encode、双 dropdown 互不关闭不响应 Esc、KnowledgeBaseModal 选中项失效不纠正。

### XSS 结论（前端）

本轮新增渲染面（知识库 Markdown、commit 详情、通知）**均安全**：react-markdown@9 未引入 rehype-raw、默认 `urlTransform` 剥离 `javascript:`、commit subject/author 按文本节点渲染、外链统一 `rel="noreferrer"`。上轮修复（WS watchdog、Monaco dirty 守卫）未被新代码破坏。

---

## 总体评价

本轮迭代质量明显高于上一轮：**上轮 12 个遗留项修复 10 个**，安全手法专业（fail-closed manifest、进程组沙箱、逐级 symlink 解析、常量时间比较），测试从 3 包扩至 8 包且全带 `-race`，构造的 5 种路径穿越攻击全部被拦截，知识库模块（knowledge.go）是全项目防护最完整的代码。

主要债务集中在三处：

1. **自动化/破坏性操作的默认值与确认强度**（N-C1、N-H1、N-H5）——一键生效的 catch-all 规则、公开仓库 auto-develop 滥用链、服务端可绕过的删除确认；
2. **凭据处理不一致**（N-H2/N-H3）——agent env 已脱敏而 monitor 凭据明文回显，且会被 UI bug 误清空；
3. **两个上轮遗留未修**（N-M6 pkill 自杀、N-M7 CI tag 注入）。

## 修复优先级建议（Top 8）

| 序 | 项 | 工作量 |
|---|---|---|
| 1 | N-H2+N-H3: monitor config 脱敏回显 + 暂停只发 `{enabled}` | ~2h |
| 2 | N-H5: 删除项目服务端强制名称确认 | ~10min |
| 3 | N-C1: 新规则默认停用 + branch_push 无条件时强制确认 | ~1h |
| 4 | N-H4: wiki 文档只读或保存即 push | ~2h |
| 5 | N-H1: auto-develop 收紧（label-only 可选、空列表可保存、每日上限、作者白名单） | ~半天 |
| 6 | N-C2: Modal Esc 栈管理（顺带修 N-M12 removeQueries） | ~2h |
| 7 | N-M6: pkill 排除自身 PID（`pkill -f` 前先取 `os.Getpid()` 过滤，或改用 launchctl bootout） | ~30min |
| 8 | N-M7: release.yml tag 经 env 传递 | ~10min |

---

## 附录：验证记录

```
$ go vet ./...                       # 通过（无输出）
$ go build -o /tmp/xissue-v214 ./cmd/xissue   # 通过
$ go test -race -count=1 ./...
ok  github.com/xpier/xissue/cmd/xissue            3.785s
ok  github.com/xpier/xissue/internal/automation   6.271s
ok  github.com/xpier/xissue/internal/config       6.278s
ok  github.com/xpier/xissue/internal/gitx         3.590s
ok  github.com/xpier/xissue/internal/knowledge    4.371s
ok  github.com/xpier/xissue/internal/runner       5.890s
ok  github.com/xpier/xissue/internal/server       6.971s
ok  github.com/xpier/xissue/internal/store        6.664s
$ cd web && tsc -p tsconfig.json --noEmit   # exit 0
```

运行时探测：token 模式安全矩阵 5/5、知识库穿越攻击拦截 5/5（详见上文），测试实例与 fixture 已清理。

*报告生成: 2026-10-21，基于 commit 3295c72 (v0.2.14)。第一轮报告见 docs/code-review-v0.2.9.md。*
