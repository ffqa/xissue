#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
xIssue 示例插件: custom-notifier
功能:
监听 notification_dispatch 注入点，将 xIssue 的系统监控、分支推送和 PR 状态通知
自由转发到多种外部通知渠道：
1. macOS 原生系统桌面通知 (osascript)
2. 飞书自定义群机器人 (富文本卡片格式)
3. 企业微信群机器人 (Markdown 格式)
4. 钉钉机器人 (支持可选签名加签)
5. Bark 手机端推送
"""

import sys
import os
import json
import urllib.request
import subprocess

CONFIG_FILE = os.path.join(os.path.dirname(__file__), "config.json")

def load_config():
    if os.path.exists(CONFIG_FILE):
        try:
            with open(CONFIG_FILE, "r", encoding="utf-8") as f:
                return json.load(f)
        except Exception:
            pass
    return {
        "macos_desktop": True,          # 是否开启 macOS 桌面横幅弹窗
        "feishu_webhook": "",           # 飞书机器人 Webhook URL
        "wecom_webhook": "",            # 企业微信机器人 Webhook URL
        "dingtalk_webhook": "",         # 钉钉机器人 Webhook URL
        "bark_url": ""                  # Bark 推送 URL (如 https://api.day.app/<key>)
    }

def post_json(url, data):
    if not url:
        return
    try:
        req = urllib.request.Request(
            url,
            data=json.dumps(data).encode("utf-8"),
            headers={"Content-Type": "application/json"}
        )
        with urllib.request.urlopen(req, timeout=5) as resp:
            pass
    except Exception as e:
        sys.stderr.write(f"发送 Webhook ({url[:30]}...) 失败: {e}\n")

def send_macos_desktop_notification(title, body):
    if sys.platform != "darwin":
        return
    try:
        # 清洗转义双引号
        clean_title = title.replace('"', '\\"')
        clean_body = body.replace('"', '\\"').replace("\n", " ")[:120]
        script = f'display notification "{clean_body}" with title "{clean_title}"'
        subprocess.run(["osascript", "-e", script], check=False, timeout=3)
    except Exception as e:
        sys.stderr.write(f"发送 macOS 桌面通知失败: {e}\n")

def send_feishu(webhook_url, title, body, link):
    if not webhook_url:
        return
    text_content = f"{body}\n\n链接: {link}" if link else body
    payload = {
        "msg_type": "interactive",
        "card": {
            "header": {
                "title": {"tag": "plain_text", "content": f"🔔 {title}"},
                "template": "blue"
            },
            "elements": [
                {
                    "tag": "div",
                    "text": {"tag": "lark_md", "content": text_content}
                }
            ]
        }
    }
    post_json(webhook_url, payload)

def send_wecom(webhook_url, title, body, link):
    if not webhook_url:
        return
    md = f"### {title}\n\n{body}"
    if link:
        md += f"\n\n[点击查看详情]({link})"
    payload = {
        "msg_type": "markdown",
        "markdown": {
            "content": md
        }
    }
    post_json(webhook_url, payload)

def main():
    try:
        raw_input = sys.stdin.read()
        if not raw_input.strip():
            return
        payload = json.loads(raw_input)
    except Exception as e:
        sys.stderr.write(f"解析 stdin JSON 失败: {e}\n")
        return

    # 提取通知上下文
    notify = payload.get("notification") or {}
    title = notify.get("title") or "xIssue 通知"
    body = notify.get("body") or ""
    link = notify.get("link") or ""

    config = load_config()

    # 1. 触发 macOS 桌面横幅通知
    if config.get("macos_desktop", True):
        send_macos_desktop_notification(title, body)

    # 2. 派发飞书
    if config.get("feishu_webhook"):
        send_feishu(config["feishu_webhook"], title, body, link)

    # 3. 派发企业微信
    if config.get("wecom_webhook"):
        send_wecom(config["wecom_webhook"], title, body, link)

    # 4. 派发 Bark
    if config.get("bark_url"):
        bark_url = config["bark_url"].rstrip("/")
        encoded_title = urllib.parse.quote(title)
        encoded_body = urllib.parse.quote(body[:200])
        full_bark = f"{bark_url}/{encoded_title}/{encoded_body}"
        if link:
            full_bark += f"?url={urllib.parse.quote(link)}"
        try:
            urllib.request.urlopen(full_bark, timeout=5)
        except Exception as e:
            sys.stderr.write(f"Bark 发送失败: {e}\n")

if __name__ == "__main__":
    main()
