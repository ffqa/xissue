# xIssue (`xpier-issue`)

**多 Git 仓库 + 拖拽式看板 + AI Agent 编排 + MCP 全 IDE 接入 + PR/分支监控 + Diff 审查** 工具（单文件二进制，内置 Web UI 与 SQLite）。

仓库地址：<https://github.com/ffqa/xissue>

---

## 🚀 快速开始与安装

xIssue 提供多种安装与运行方式（已内置完整前端与数据引擎，单文件独立运行，无需配置 Node.js 或 Go）：

### 方式一：Homebrew 安装（推荐，支持自动升级与服务托管）

```bash
# 1. 添加 tap 并安装
brew tap ffqa/tap
brew install ffqa/tap/xissue

# 2. 安装并启动系统级常驻自启服务（二选一）：
xissue service install               # 推荐：xIssue 内置自管服务（开机自启）
# 或使用 Homebrew 托管：
brew services start ffqa/tap/xissue

# 3. （可选）一键把 xIssue MCP 插件注册到本机所有主流 IDE / CLI Agent
xissue mcp --install
```

### 方式二：下载预编译 Release 单文件运行

可以直接从 [GitHub Releases](https://github.com/ffqa/xissue/releases) 下载对应系统的预编译包：

```bash
# 1. 下载最新发布包（以 macOS Apple Silicon 为例，架构可选：darwin-arm64 / darwin-amd64 / linux-amd64 / linux-arm64）
curl -fL "$(curl -s https://api.github.com/repos/ffqa/xissue/releases/latest | grep "browser_download_url.*darwin-arm64.tar.gz" | cut -d '"' -f 4)" | tar -xz
cd xissue-*-darwin-arm64

# 2. 一键安装为开机自启常驻服务（自动复制到 ~/.local/bin 并生成配置，无需保留解压包）
./xissue service install
```

> **安全与持久化保障**：
> `xissue service install` 会自动将二进制复制安装到 `$HOME/.local/bin/xissue` 并清理 macOS 隔离属性。
> LaunchAgent plist（macOS）与 systemd（Linux）配置**100% 固定指向持久化路径**，安装完成后可立刻删除下载解压的目录，常驻后台服务完全不受影响！

### 🛠️ 常驻后台服务管理命令 (`xissue service`)

无论通过 Homebrew 安装还是预编译包安装，均可直接使用 `xissue service` 管理后台常驻服务：

```bash
xissue service status     # 查看常驻服务运行状态与 7789 端口监听
xissue service restart    # 重启常驻服务
xissue service uninstall  # 停止并卸载常驻自启服务
```

### 🔄 检查与版本自动升级 (`xissue update`)

无论是通过 Homebrew 还是直接下载预编译二进制安装，均可通过一行命令自动检查或升级至最新版本：

```bash
# 检查是否有更新
xissue update --check

# 一键自动升级（Homebrew 模式自动调用 brew upgrade，独立二进制模式自动下载替换最新版本并热重启服务）
xissue update
```

### macOS 首次运行被系统拦截（Gatekeeper）

若手动下载的二进制首次执行被 macOS 拦截：

```bash
sudo xattr -r -c ~/.local/bin/xissue
chmod +x ~/.local/bin/xissue
```

---

## ✨ 核心特性

- **单仓 / 多仓统一模型**：一个 Project 包含 `1..N` 个 Git 仓库。每次 Run 为各仓库从本地 bare mirror 创建独立 `git worktree` 放在同一父目录下，Agent 可跨仓库修改；提交时按仓库分别 commit / push / 提 PR。
- **精简四列拖拽流转看板**：
  - 看板卡片仅展示**标题**与**两行摘要**，干净清爽；完整描述、评论、状态下拉与代码审查均在详情页内操作。
  - 支持直接拖拽卡片跨列流转：将卡片拖入 **⚡ 执行中 (Running)** 列即可自动唤醒默认 AI Agent 开始执行任务。
- **全 IDE / CLI Agent 通用 MCP 接入 (`xissue mcp`)**：
  - 支持 `xissue mcp --install` 一键注入到 Claude Code、Cursor、Windsurf、VS Code Copilot、OpenCode、Gemini CLI、Antigravity、Qoder CLI (`qoderclicn`)、Cline、Roo Code、Zed 等。
  - 支持通过 MCP 直接创建任务并传 `autoRun=true` 立即调度后台 Agent。
- **通用 ACP Agent 驱动 & 热会话复用（Warm ACP Session）**：
  - 通过 Agent Client Protocol (stdio JSON-RPC) 统一对接 `opencode`、`omp`、`codex` 等 Agent。
  - 首轮完成后进程自动进入 20 分钟热驻留池；发送追加指令（“继续”）时直接在原进程内热恢复上下文，**0 进程重启、0 重复历史 Token 消耗**。
- **🔔 通知中心 + 分支监控 (Watch) + PR 状态自动同步**：
  - 监控任意仓库分支推送动态，发现新提交立即弹窗/响铃通知，并可一键带入提交 SHA 打开 **🍒 Cherry-Pick 创建 PR**。
  - 自动轮询同步 `PR Open` 任务的远端合并/关闭状态，合并后自动流转至 `Done`。
- **🍒 Cherry-Pick 创建 PR & 标准改动记录生成**：
  - 支持在提交审查或 Cherry-Pick 时自动生成团队规范格式改动记录（`本次改动：a. / b. / 分支 / PR / 备注`），一键复制备查。
- **内置审查与暗色/亮色主题切换**：
  - Web UI 内嵌本地打包的 Monaco DiffEditor，支持行内编辑保存、一键跳回 VSCode/Cursor。

---

## 🛠️ 从源码构建与常用命令

```bash
# 查看所有可用命令
make

# 完整构建（前端 + Go 单文件二进制 ./bin/xissue）
make build

# 编译并发布到本机常驻服务 (http://127.0.0.1:7789)
make deploy

# 一键安装 MCP 到本机所有 IDE / Agent
make mcp-install
```

常用启动参数：

```bash
./bin/xissue -listen 127.0.0.1:7789 -data-dir ~/.xissue -token your-optional-token
```
