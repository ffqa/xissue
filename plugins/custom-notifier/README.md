# 插件：custom-notifier（多渠道自定义通知扩展）

这是一个用于演示 **xIssue 通知扩展注入点 (`notification_dispatch`)** 的插件。

通过该注入点，您可以像 DeepSeek Harness 一样，完全自定义与扩展系统的通知体系，将通知无缝派发至任意团队 IM 工具、手机终端或桌面系统。

---

## 🎯 支持渠道
1. **macOS 原生桌面通知**：使用 `osascript` 弹出系统通知横幅；
2. **飞书群机器人**：使用飞书富文本互动卡片（`lark_md`）；
3. **企业微信机器人**：使用 Markdown 格式消息推送并附带直达链接；
4. **Bark 手机推送**：支持将通知推送至 iOS Bark App 并带 URL 直达跳转；
5. **钉钉 / Discord / Slack**：可在代码中简单追加标准 Webhook 载荷。

---

## ⚙️ 配置文件说明 (`config.json`)

在插件目录下创建 `config.json`：

```json
{
  "macos_desktop": true,
  "feishu_webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/xxxx",
  "wecom_webhook": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxx",
  "bark_url": "https://api.day.app/your_bark_key"
}
```

---

## 🚀 安装方法

```bash
mkdir -p ~/.xissue/plugins
cp -r plugins/custom-notifier ~/.xissue/plugins/
```
安装后，系统产生的任何通知（分支更新、PR 审查、合并或自动化事件）都将自动触发此插件执行多平台分发。
