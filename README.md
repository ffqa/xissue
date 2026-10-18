# xIssue (`xpier-issue`)

个人使用的 **多 Git 仓库 + Issue 看板 + AI Agent 编排与 Diff 审查** 工具（单文件二进制，内置 Web UI）。

## 核心特性（M1）

- **单仓 / 多仓统一模型**：一个 Project 包含 `1..N` 个 Git 仓库。每次 Run 为各仓库从本地 bare mirror 创建独立 `git worktree` 放在同一父目录下，Agent 可跨仓库修改；提交时按仓库分别 commit / push / 提 PR。
- **本地与平台 Issue 统一**：
  - 支持从 GitHub 同步 Issue（复用本机 `gh` 登录态），也支持纯 Git 仓库（`plain`）。
  - 在工具内创建的本地任务（`LOCAL-1`, `LOCAL-2`…）同样作为 Issue 留档，支持一键发布为平台 Issue，并自动导出 Markdown 到 `~/.xissue/issues/` 便于搜索与备份。
- **通用 ACP Agent 驱动**：通过 Agent Client Protocol (stdio JSON-RPC) 统一对接：
  - `opencode` (`opencode acp`)
  - `omp` (`omp acp`)
  - `codex` (`npx -y @zed-industries/codex-acp`)
- **非无人值守权限审批**：不使用 `--dangerously-skip-permissions`。低风险操作（读/搜索/编辑）可按配置自动放行，敏感命令实时推送到 Web UI 高亮等待人工审批，支持通过 Tailscale 随时在手机/电脑上放行。
- **小内存机器友好**：按并发数与预估内存预算（`memoryBudgetMB`）排队调度；Linux 下可选启用 `systemd-run --user --scope -p MemoryMax=...` 限制单次 Agent 内存；Git 仓库共享同一份 `--filter=blob:none` bare mirror。
- **内置审查与小改**：Web UI 内嵌本地打包的 Monaco DiffEditor，支持按仓库查看 Diff、行内小改保存、一键跳回 VSCode/Cursor（`vscode://file{path}`），以及一键提交推送或创建 PR。

## 构建与运行

```bash
# 构建前端与 Go 单文件二进制
make

# 启动服务（默认监听 http://127.0.0.1:7789，数据目录 ~/.xissue）
./bin/xissue
```

常用参数：

```bash
./bin/xissue -listen 127.0.0.1:7789 -data-dir ~/.xissue -token your-optional-token
```

配置文件在首次启动时自动生成于 `~/.xissue/config.yaml`，可自定义各 Agent 启动命令、内存预估与权限白名单。
