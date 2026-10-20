#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
xIssue 示例插件: follow-commit-watcher
功能:
1. 监听 pr_merged 事件：记录被合并的 PR 提交信息，初始化后续提交计数器。
2. 监听 branch_push 事件：
   - 追踪本人提交被合并后的后续第 1 ~ N 次提交 (例如验证合入后的代码是否被后续修改覆盖或产生回归)；
   - 追踪指定作者列表 (如 release-bot、某同事) 的最新提交；
   - 自动提取改动文件列表，查看关注的特定文件 (如 package.json, go.mod, CHANGELOG.md) 最新内容；
   - 生成提交 Diff 摘要；
   - 输出动作: 发送详细通知 (可带链接) 与可选自动创建跟踪 Issue。
"""

import sys
import os
import json
import subprocess
from datetime import datetime

STATE_FILE = os.path.join(os.path.dirname(__file__), ".watcher_state.json")
CONFIG_FILE = os.path.join(os.path.dirname(__file__), "config.json")

def load_json(filepath, default_val):
    if os.path.exists(filepath):
        try:
            with open(filepath, "r", encoding="utf-8") as f:
                return json.load(f)
        except Exception:
            pass
    return default_val

def save_json(filepath, data):
    try:
        with open(filepath, "w", encoding="utf-8") as f:
            json.dump(data, f, indent=2, ensure_ascii=False)
    except Exception as e:
        sys.stderr.write(f"保存状态文件失败: {e}\n")

def get_git_repo_path(payload):
    """尝试获取仓库本地工作区或镜像路径"""
    repo = payload.get("repo") or {}
    remote_url = repo.get("remoteUrl", "")
    if remote_url and os.path.exists(remote_url):
        return remote_url
    # 检查项目中的其他 repos
    project = payload.get("project") or {}
    for r in project.get("repos", []):
        p = r.get("remoteUrl", "")
        if p and os.path.exists(p):
            return p
    return None

def run_git_cmd(repo_dir, args):
    """安全执行 git 命令获取 diff 或文件内容"""
    if not repo_dir or not os.path.isdir(repo_dir):
        return ""
    try:
        res = subprocess.run(
            ["git"] + args,
            cwd=repo_dir,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=8
        )
        return res.stdout.strip()
    except Exception as e:
        sys.stderr.write(f"执行 git 命令失败: {e}\n")
        return ""

def main():
    try:
        raw_input = sys.stdin.read()
        if not raw_input.strip():
            return
        payload = json.loads(raw_input)
    except Exception as e:
        sys.stderr.write(f"解析 stdin JSON 失败: {e}\n")
        return

    event = payload.get("event")
    state = load_json(STATE_FILE, {
        "last_merged_pr": None,
        "last_merged_at": None,
        "post_merge_count": 0,
        "watched_commits": []
    })

    config = load_json(CONFIG_FILE, {
        "target_authors": ["release-bot", "ci-bot"],  # 指定重点监控人员
        "max_post_merge_check": 3,                    # 合并后重点追踪前 3 次提交
        "watch_files": ["package.json", "go.mod", "CHANGELOG.md", "README.md", "VERSION"], # 重点关注文件
        "auto_create_issue_on_keyword": ["breaking", "release", "rollback"]
    })

    actions = []

    # 1. 处理 PR 合并事件 (pr_merged)
    if event == "pr_merged":
        pr = payload.get("pr") or {}
        issue = payload.get("issue") or {}
        state["last_merged_pr"] = {
            "number": pr.get("number"),
            "title": pr.get("title"),
            "url": pr.get("url"),
            "issue_key": issue.get("key"),
            "author": pr.get("author")
        }
        state["last_merged_at"] = datetime.now().isoformat()
        state["post_merge_count"] = 0  # 重置计数器
        save_json(STATE_FILE, state)

        actions.append({
            "type": "notify",
            "message": f"🎉 插件已记录 PR #{pr.get('number')} 合并事件，开启后续提交监控追踪 (最多追踪后 {config['max_post_merge_check']} 次提交)。"
        })

    # 2. 处理分支新代码推送 (branch_push)
    elif event == "branch_push":
        branch = payload.get("branch", "")
        commits = payload.get("commits", [])
        repo = payload.get("repo") or {}
        repo_dir = get_git_repo_path(payload)

        post_count = state.get("post_merge_count", 0)
        target_authors = [a.lower().strip("@") for a in config.get("target_authors", [])]
        watch_files = config.get("watch_files", [])

        for c in commits:
            post_count += 1
            author = (c.get("author") or "").lower().strip("@")
            subject = c.get("subject", "")
            sha = c.get("sha", "")
            short_sha = c.get("shortSha") or sha[:7]

            is_target_author = any(ta in author for ta in target_authors)
            is_post_merge_watched = (
                state.get("last_merged_pr") is not None and
                post_count <= config.get("max_post_merge_check", 3)
            )

            if is_target_author or is_post_merge_watched:
                # 提取该提交修改的文件列表
                changed_files_raw = run_git_cmd(repo_dir, ["show", "--name-only", "--pretty=format:", sha])
                changed_files = [line.strip() for line in changed_files_raw.splitlines() if line.strip()]

                # 提取简短 Diff 统计
                diff_stat = run_git_cmd(repo_dir, ["show", "--stat", "--oneline", sha])
                if not diff_stat:
                    diff_stat = f"{short_sha} {subject}"

                # 检查关注的核心文件变动与内容预览
                file_previews = []
                for wf in watch_files:
                    if any(wf in cf for cf in changed_files):
                        content = run_git_cmd(repo_dir, ["show", f"{sha}:{wf}"])
                        if content:
                            snippet = "\n".join(content.splitlines()[:10])  # 前 10 行预览
                            file_previews.append(f"📄 核心文件变动预览 `{wf}`:\n```\n{snippet}\n```")

                # 组装通知信息
                reason = []
                if is_post_merge_watched:
                    last_pr = state.get("last_merged_pr") or {}
                    reason.append(f"PR #{last_pr.get('number')} 合并后的第 {post_count} 次后续提交")
                if is_target_author:
                    reason.append(f"命中关注作者 @{c.get('author')}")

                msg_lines = [
                    f"🔍 [代码追踪] 发现关注的提交: {short_sha} by {c.get('author')}",
                    f"📌 触发原因: {' & '.join(reason)}",
                    f"📝 提交信息: {subject}",
                    f"📂 改动文件数: {len(changed_files)} 个"
                ]
                if changed_files:
                    msg_lines.append(f"改动清单: {', '.join(changed_files[:6])}")
                if file_previews:
                    msg_lines.extend(file_previews)

                actions.append({
                    "type": "notify",
                    "message": "\n".join(msg_lines)
                })

                # 检查是否包含特殊关键字需自动创建 Issue
                keywords = config.get("auto_create_issue_on_keyword", [])
                if any(k in subject.lower() for k in keywords):
                    actions.append({
                        "type": "create_issue",
                        "prompt": f"[自动跟进] 关注提交 {short_sha} 包含关键改动: {subject}"
                    })

        state["post_merge_count"] = post_count
        save_json(STATE_FILE, state)

    # 3. 输出 Action 供 xIssue 调度执行
    if actions:
        print(json.dumps({"actions": actions}, ensure_ascii=False))

if __name__ == "__main__":
    main()
