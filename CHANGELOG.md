# Changelog

本项目从 0.6.0 起要求每个 tag 在 `CHANGELOG.md` 有对应条目；更早版本按 git tag 回填。

版本策略（0.x）：
- **patch（小版本）** 用于修复与小功能；**每个小版本号未超过 10 之前不发 minor（中版本）**（例如持续 0.8.1、0.8.2……），除非人工明确指定。人工指定优先于“破坏性变更”判断。
- 处理完一个任务批次且没有排队的后续任务时，**立即发版**，不把已提交改动留在未发布状态。
- 每个 tag 必须有对应 CHANGELOG 条目；发布流程见 `scripts/brew-publish.sh` 与 `.github/workflows/release.yml`。

## [0.8.3] - 2026-11-02

### Changed

- 本机 macOS 构建默认使用自签名代码签名（独立 keychain `xissue-signing.keychain-db` + 自动加入钥匙串搜索列表），签名后 designated requirement 固定为 `identifier "dev.xissue" and certificate root = H"…"`，重建不再让 TCC 重新询问磁盘权限；未创建证书时构建静默跳过。发布产物不在 CI 签名（用户自行编译）。
- `make codesign-setup` 一次性创建并信任该证书（仅一次系统授权）。

## [0.8.2] - 2026-11-02

### Changed

- Agent 权限请求默认放行（yolo / bypass）：`permissions.autoApproveAll` 默认 `true`，Run 不再因 `execute`、`fetch`、`git push` 等请求停在「待审批」；设为 `false` 可恢复原来的按 kinds 放行 + 强制询问行为。

## [0.8.1] - 2026-11-02

### Changed

- 看板 Issue 卡片更紧凑：内边距 `7×9 → 4×8`、元素组间距 `3px → 1px`，标题固定单行并省略（悬停 tooltip 显示完整标题 + key/状态）。
- 卡片摘要字号从 12px 提升到 12.5px，行高收紧到 15px，同样两行内容更易读且更矮。
- 插槽 `project.toolbar` 跟随项目工具/设置移到左侧「当前项目」菜单下方；slot 名称与尺寸不变，侧栏收起时隐藏。`project.header`、`issue.toolbar` 位置不变。

## [0.8.0] - 2026-11-02

### Added

- 项目级 **TODO 列表**与**测试计划**：独立于 Issue 持久保存（Issue 归档/删除不受影响），支持搜索（标题/详情/来源 Key）与未完成/已完成筛选；测试计划同类型重复时自动编号 `x1`、`x2`…，并保留来源 Issue 快照。
- 项目侧栏新增「☑️ TODO 列表」「🧪 测试计划」两个整页，支持行内添加、勾选完成、删除。
- MCP 新增 10 个任务工具：`xissue_list_project_todos`、`xissue_add_todo`、`xissue_add_todos`（批量）、`xissue_update_todo`、`xissue_delete_todo`，以及 `xissue_list_project_test_plans`、`xissue_add_test_plan`、`xissue_add_test_plans`（批量）、`xissue_update_test_plan`、`xissue_delete_test_plan`，Agent 可无需人工介入地维护。
- 新增可选的本机 macOS 代码签名助手（`scripts/sign-macos.sh` / `make codesign-setup`），用于稳定 TCC 授权身份；不配置时构建静默跳过。

### Fixed

- 修复同一 PR 出现两条：按 PR URL 去重（列表、分页、前端跨页都有兜底）；本地-only 仓库不再参与 PR 查询，远端探测会把本地路径降级为 `plain` 并清空 owner，避免串用其它仓库的 PR。
- 恢复整页化导航时丢失的 `project.header` / `project.toolbar` 插件插槽；`project.page`、`issue.actions`、`issue.toolbar`、`issue.page` 保持不变。
- 添加仓库表单默认平台改为 `plain`，避免本地目录被误当成 GitHub 仓库。

## [0.7.0] - 2026-11-01

### Added

- 侧栏导航重构：项目空间与系统设置分两态，前者显示「全部项目」+ 置顶项目 + 当前项目菜单，后者由右上角 `⚙️` 进入并显示系统菜单；侧栏可收缩并记忆状态。
- 项目内功能全部整页化并拥有独立路由：`/projects/:id/{settings,edit,repos,knowledge,readme,prs,commits,cherry-pick,branches,history}`、`/issues/:id/terminal?run=N`、`/settings/:tab`、`/monitor`、`/usage`、`/plugins`、`/agents`、`/mcp`、`/schedules`、`/jobs`、`/new-project`；旧 `?commits=1` / `?prs=1` 深链自动跳转。
- 项目唯一标识 `slug`：创建时自动生成、老库启动回填、可在「项目信息」编辑；冲突/非法格式返回 400。
- 项目主版本：列表与详情返回主仓库最新 semver tag，项目卡片、页头与面包屑展示。
- 项目置顶 `pinned`：列表接口置顶优先，侧栏「全部项目」下只显示置顶项目；项目卡片与项目信息页可切换。
- Issue 页保留项目上下文：侧栏显示当前项目菜单与「当前 Issue」。
- `GET /api/runs/{id}/changes?patch=1` 返回 `worktreePath` 与完整 unified diff（含未跟踪文件，单仓 256KB 截断）。
- MCP 新增 `xissue_get_run_changes`、`xissue_send_message`、`xissue_submit_run`。
- `PATCH /api/issues/{id}` 支持 `{"archived": true|false}`；`runner.doneArchiveMaxCount`（默认 20）按项目保留最近完成的 N 条。
- 用量统计拆分「缓存读取」token，输入/缓存/输出/总计口径一致。

### Changed

- 直接合入（`merge_local`）失败时 Run 记为 `failed`、Issue 停留在 `review`，不再误标 Done 被自动归档。
- 直接合入前自动 `rebase` 到最新目标分支；冲突时中止并返回明确错误，成功或失败都会清理临时分支。
- 用量页、看板等页面改为完整页面布局，看板四列自适应高度、页面不再出现外层滚动条。
- 无 `-token` 时拒绝监听非回环地址（API 暴露终端与文件写入能力）。

### Fixed

- 手动同步与后台同步互斥，避免同一 Issue 并发触发重复自动预开发；重复的失败/额度提示不再刷屏。
- Done 自动归档按 `completed_at` 判断，评论不再让旧任务逃避归档。
- 仓库外链：项目仓库与 Issue 所属仓库可跳转到对应平台页面。

## [0.6.1] - 2026-10-31

### Changed

- 项目 Issue 列表默认只返回未归档条目；归档改为独立接口 `GET /api/projects/{id}/issues/archived`，支持 `q/limit/offset` 分页搜索，避免归档数据继续进入看板首屏。
- Done/归档弹窗按需加载归档页；归档页支持服务端搜索与「加载更多」，删除后同时失效两套列表缓存。
- 新增 `(project_id, archived_at, updated_at)` 与 `(project_id, status, updated_at)` 索引。

### Fixed

- 归档 Issue 不再出现在 Done 列，也不再随项目 Issue 列表全量传输。

## [0.6.0] - 2026-10-30

### Changed

- 移除 Backlog 状态、页签、面板、拖拽排序接口与 `backlogRank`；旧 `backlog` 数据在启动时迁移到 `todo`。
- 新增 Done 自动归档：`issues.archived_at`，默认 30 天，`runner.doneArchiveDays` 可配置；Done 列只显示未归档，弹窗分「已完成 / 已归档」。
- 工作流收敛为 Todo → Running → Review → Done。

### Fixed

- 归档 Issue 不再出现在 Done 列；重新打开到非 Done 状态会清除归档标记。


## [0.5.16] - 2026-10-30

- feat: horizontal PR rows and project filter (0.5.16)

## [0.5.15] - 2026-10-30

- fix: mount toolbar plugins at end of action row (0.5.15)

## [0.5.14] - 2026-10-30

- fix: let toolbar plugins resize their width (0.5.14)

## [0.5.13] - 2026-10-30

- fix: remove long backlog hint (0.5.13)

## [0.5.12] - 2026-10-30

- fix: remove batch auto-run and compact board cards (0.5.12)

## [0.5.11] - 2026-10-29

- feat: add default-agent AI API for plugins (0.5.11)

## [0.5.10] - 2026-10-29

- fix: align small watch controls at 24px (0.5.10)

## [0.5.9] - 2026-10-29

- feat: add toolbar plugin slots and host modal bridge (0.5.9)

## [0.5.8] - 2026-10-29

- feat: lazy-load project PRs with fast REST first page (0.5.8)

## [0.5.7] - 2026-10-29

- feat: add run-now action to scheduled tasks (0.5.7)

## [0.5.6] - 2026-10-29

- fix: paginate all GitHub PRs instead of capping at 1000 (0.5.6)

## [0.5.5] - 2026-10-29

- fix: load full GitHub PR lists and sort newest first (0.5.5)

## [0.5.4] - 2026-10-29

- fix: recover empty PR lists and sort newest first (0.5.4)

## [0.5.3] - 2026-10-29

- feat: filter PR list to my submissions (0.5.3)

## [0.5.2] - 2026-10-29

- feat: cache lists, batch GitHub reads, queue branch deletes, add backlog and PR links (0.5.2)

## [0.5.1] - 2026-10-29

- feat: move schedules to status bar and stream branch deletion progress (0.5.1)

## [0.5.0] - 2026-10-28

- feat: protect cherry-pick sources, expose schedules and compact project/board headers (v0.5.0)

## [0.4.1] - 2026-10-28

- fix: make only card detail/external links clickable (v0.4.1)

## [0.4.0] - 2026-10-28

- feat: add sub-issues, draggable attachments, label presets, compact cards and merged-branch cleanup (v0.4.0)

## [0.3.0] - 2026-10-28

- feat: add issue labels, priority, attachments, remote binding and timeline (v0.3.0)

## [0.2.43] - 2026-10-28

- fix: keep project list independent of repo detection (v0.2.43)

## [0.2.42] - 2026-10-28

- fix: keep one PR refresh timer per shared query (v0.2.42)

## [0.2.41] - 2026-10-28

- feat: warm and cache project PR list with 60s refresh (v0.2.41)

## [0.2.40] - 2026-10-28

- test: guard plugin iframe sandbox remount and cache settings (v0.2.40)

## [0.2.39] - 2026-10-27

- fix: preserve issue slot spacing while removing project gaps (v0.2.39)

## [0.2.38] - 2026-10-27

- fix: remount plugin iframe on sandbox switch and tighten slot spacing (v0.2.38)

## [0.2.37] - 2026-10-27

- fix: tighten project header spacing (v0.2.37)

## [0.2.36] - 2026-10-27

- feat: add global plugin UI sandbox switch (v0.2.36)

## [0.2.35] - 2026-10-27

- feat: let trusted plugin UI access the host app directly (v0.2.35)

## [0.2.34] - 2026-10-27

- feat: add GitLab provider, remote agent capabilities, frontend tests and sandboxed plugin UI (v0.2.34)

## [0.2.33] - 2026-10-26

- fix: fail fast when a provider has no PR support for v0.2.33

## [0.2.32] - 2026-10-26

- fix: require auto-develop author allowlists and keep local merges local for v0.2.32

## [0.2.31] - 2026-10-26

- fix: refresh default-agent cache immediately for v0.2.31

## [0.2.30] - 2026-10-26

- fix: honor the configured default agent in project actions for v0.2.30

## [0.2.29] - 2026-10-26

- fix: wait for remote archives during shutdown for v0.2.29

## [0.2.28] - 2026-10-26

- fix: restart the running Homebrew service after updates for v0.2.28

## [0.2.27] - 2026-10-26

- feat: move project README into the toolbar for v0.2.27

## [0.2.26] - 2026-10-26

- fix: keep completed status when resolving remote permissions for v0.2.26

## [0.2.25] - 2026-10-26

- fix: restart supervised services as fresh processes for v0.2.25

## [0.2.24] - 2026-10-25

- fix: repair bare local remote HEAD before mirroring for v0.2.24

## [0.2.23] - 2026-10-25

- fix: harden remote runner ownership, usage and retention for v0.2.23

## [0.2.22] - 2026-10-23

- fix: migrate Codex ACP, expose probe stderr and verify service restarts

## [0.2.21] - 2026-10-23

- fix: discover Herd and version-manager Node bins for model probing

## [0.2.20] - 2026-10-23

- feat: fullscreen project modals, global automation rules and settings UX

## [0.2.19] - 2026-10-22

- feat: knowledge base fullscreen and WebSocket status stream

## [0.2.18] - 2026-10-22

- feat: backfill triage labels for existing synced issues

## [0.2.17] - 2026-10-22

- perf: read commit history from cached mirrors

## [0.2.16] - 2026-10-22

- feat: commit history page, hourly auto-update and live status bar

## [0.2.15] - 2026-10-22

- feat: global plugin management, issue triage and auto-develop gate

## [0.2.14] - 2026-10-21

- fix: persist automation rules immediately on add/toggle/delete

## [0.2.13] - 2026-10-21

- feat: auto pre-develop synced issues into review-only PRs

## [0.2.12] - 2026-10-21

- fix: secure doc paths, config concurrency, hook sandboxing and clean-checkout builds

## [0.2.11] - 2026-10-20

- fix: cross-platform process termination for windows cross-compilation

## [0.2.10] - 2026-10-19

- feat: add SVG & ICO favicon and bump version to v0.2.10

## [0.2.9] - 2026-10-19

- feat: v0.2.9 release - ABC automation & plugin architecture, PR report auto-merge, and monitor modal fullscreen/padding refinement

## [0.2.8] - 2026-10-19

- feat: v0.2.8 release - built-in xissue service commands, unblock process restart, and persistent installation

## [0.2.7] - 2026-10-19

- feat: v0.2.7 release - persistent installation, homebrew tap & self-update, monitor dashboard, and refined topbar UI

## [0.2.6] - 2026-10-19

- fix(release): v0.2.6 - package deploy directory in release archives and add fallback templates to install-service.sh

## [0.2.5] - 2026-10-19

- feat: v0.2.5 release - project PR management, in-app PR merge/close, and automatic local pull

## [0.2.4] - 2026-10-19

- feat: v0.2.4 release - project DevMode (single_main vs main_dev), direct merge to local main, and customizable PR workflow

## [0.2.3] - 2026-10-19

- fix(ci): fix pnpm workspace configuration and upgrade pnpm to v10 in release workflow

## [0.2.2] - 2026-10-19

- feat: v0.2.2 release - auto detect repo remote & platform sync, bump version to 0.2.2

## [0.2.1] - 2026-10-19

- ci: use --no-frozen-lockfile in release workflow
