# Changelog

本项目从 0.6.0 起要求每个 tag 在 `CHANGELOG.md` 有对应条目；更早版本按 git tag 回填。

版本策略（0.x）：patch 用于兼容的修复/小功能，minor 用于破坏性变更（状态/API/数据结构），patch 号不要求连续填满。

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
