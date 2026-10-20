# 插件：follow-commit-watcher（合并与指定作者提交追踪）

这是一个为 **xIssue** 开发的高级示例插件，用于示范如何利用全生命周期注入点与 Git 工具链实现高度定制的工程流程。

## 🎯 核心解决场景
1. **防回归追踪**：当本人的 PR 被合并（`pr_merged`）后，自动开启雷达，对该主线分支接下来的**第 1 ~ N 次提交**持续关注，实时分析是否有改动覆盖了本人的代码或引发潜在冲突；
2. **指定关键人/机器人追踪**：关注特定提交人（如 CI Bot、Release Bot 或特定核心成员）的新推送，第一时间发出带完整详情的通知；
3. **关键文件内容直接抽取**：当提交涉及 `package.json`、`go.mod`、`CHANGELOG.md` 等核心文件时，通过 `git show <sha>:<file>` 直接抽取前几行关键内容显示在通知中；
4. **关键字自动派生任务**：当提交说明中出现 `breaking`、`release`、`rollback` 等关键字时，自动调用 xIssue 创建关联 Issue 安排跟进。

---

## ⚙️ 配置文件说明 (`config.json`)

在插件目录下放置 `config.json`（可选，插件自带默认值）：

```json
{
  "target_authors": ["release-bot", "ci-bot", "collaborator"],
  "max_post_merge_check": 3,
  "watch_files": ["package.json", "go.mod", "CHANGELOG.md", "VERSION"],
  "auto_create_issue_on_keyword": ["breaking", "rollback", "urgent"]
}
```

- `target_authors`: 需要重点监控的提交人列表；
- `max_post_merge_check`: PR 合并后重点追踪的后续提交次数（例如后 3 次）；
- `watch_files`: 重点关注的文件名列表，命中时自动抽取文件最新内容；
- `auto_create_issue_on_keyword`: 命中关键字自动在 xIssue 看板中创建 Issue。

---

## 🚀 安装与加载

将本目录拷贝或软链接至 xIssue 全局插件目录：
```bash
mkdir -p ~/.xissue/plugins
cp -r plugins/follow-commit-watcher ~/.xissue/plugins/
```
在 xIssue 项目设置中的「自动化」页面即可自动识别并加载该插件。
