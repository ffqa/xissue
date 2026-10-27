# Plugin UI demo

Copy this directory to `~/.xissue/plugins/ui-demo/` and open
**⚙️ → 🧩 插件管理 → 打开插件页面**. The same UI is mounted in the
`project.page` and `issue.actions` slots.

Contract (all optional except `entry`):

```yaml
ui:
  entry: ui/index.html   # required, must live under ui/
  title: UI Demo
  slots:
    - project.page       # project.page | project.header
    - issue.actions      # issue.actions | issue.page
```

The host renders the entry in a same-origin iframe by default. Plugins are
trusted, operator-installed extensions, so they can read `parent.document`,
cookies and localStorage, and call `/api/*` directly. The global
**插件 UI 沙盒模式** (Settings → General → 🧩 插件 UI) can turn on a
same-origin-blocking sandbox for less trusted plugins. Communication can also
use `postMessage`:

| Direction | Message |
|---|---|
| plugin → host | `{type:"xissue:ready"}` |
| host → plugin | `{type:"xissue:context", slot, context}` |
| plugin → host | `{type:"xissue:notify", kind:"info"\|"success"\|"error", message}` |
| plugin → host | `{type:"xissue:open", path:"/issues/1"}` |
| plugin → host | `{type:"xissue:resize", height: 320}` |
| plugin → host | `{type:"xissue:reload"}` |

Only relative in-app paths beginning with `/` are accepted by
`xissue:open`; `//host` is rejected.
