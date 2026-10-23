import type { Notification } from './types';

export type NotificationChannel = 'browser' | 'telegram' | 'webhook';
export type NotificationRoutes = Record<string, NotificationChannel[]>;

export const NOTIFICATION_EVENTS: { kind: string; label: string; description: string }[] = [
  { kind: 'run_done', label: '任务完成 / 失败', description: 'Agent Run 结束、待验收或失败' },
  { kind: 'permission', label: '等待授权', description: 'Agent 请求高风险权限' },
  { kind: 'pr_status', label: 'PR 状态', description: 'PR 创建、合并、关闭、Cherry-Pick' },
  { kind: 'branch_push', label: '分支推送', description: '监控分支出现新提交' },
  { kind: 'automation', label: '自动化触发', description: '规则、Hooks、插件执行动作' },
  { kind: 'update', label: '版本更新', description: '发现或完成 xIssue 自更新' },
  { kind: 'test', label: '测试通知', description: '手动测试通知链路' },
];

export const NOTIFICATION_CHANNELS: { id: NotificationChannel; label: string }[] = [
  { id: 'browser', label: '浏览器' },
  { id: 'telegram', label: 'Telegram' },
  { id: 'webhook', label: 'Webhook' },
];

export const BROWSER_NOTIFY_KEY = 'xissue.browserNotify';
export const BROWSER_ROUTES_KEY = 'xissue.notificationRoutes';
export const BROWSER_ONLY_HIDDEN_KEY = 'xissue.browserOnlyHidden';

export function defaultNotificationRoutes(): NotificationRoutes {
  return Object.fromEntries(
    NOTIFICATION_EVENTS.map((e) => [e.kind, NOTIFICATION_CHANNELS.map((c) => c.id)]),
  );
}

export function normalizeNotificationRoutes(
  routes?: Record<string, string[]> | null,
): NotificationRoutes {
  const base = defaultNotificationRoutes();
  if (!routes || Object.keys(routes).length === 0) return base;
  const out: NotificationRoutes = {};
  for (const event of NOTIFICATION_EVENTS) {
    const selected = routes[event.kind];
    out[event.kind] = selected
      ? selected.filter((ch): ch is NotificationChannel =>
          NOTIFICATION_CHANNELS.some((c) => c.id === ch),
        )
      : [...base[event.kind]];
  }
  return out;
}

export function readBrowserNotify(): boolean {
  try {
    return localStorage.getItem(BROWSER_NOTIFY_KEY) === '1';
  } catch {
    return false;
  }
}

export function writeBrowserNotify(enabled: boolean): void {
  try {
    localStorage.setItem(BROWSER_NOTIFY_KEY, enabled ? '1' : '0');
  } catch {
    // ignore storage failures
  }
}

export function readBrowserOnlyHidden(): boolean {
  try {
    return localStorage.getItem(BROWSER_ONLY_HIDDEN_KEY) === '1';
  } catch {
    return false;
  }
}

export function writeBrowserOnlyHidden(enabled: boolean): void {
  try {
    localStorage.setItem(BROWSER_ONLY_HIDDEN_KEY, enabled ? '1' : '0');
  } catch {
    // ignore storage failures
  }
}

export function readBrowserRoutes(): NotificationRoutes {
  try {
    const raw = localStorage.getItem(BROWSER_ROUTES_KEY);
    if (!raw) return defaultNotificationRoutes();
    return normalizeNotificationRoutes(JSON.parse(raw) as Record<string, string[]>);
  } catch {
    return defaultNotificationRoutes();
  }
}

export function writeBrowserRoutes(routes: NotificationRoutes): void {
  try {
    localStorage.setItem(BROWSER_ROUTES_KEY, JSON.stringify(routes));
  } catch {
    // ignore storage failures
  }
}

export function browserRouteAllowsKind(kind: string): boolean {
  if (!readBrowserNotify()) return false;
  if (typeof Notification === 'undefined' || Notification.permission !== 'granted') return false;
  const routes = readBrowserRoutes();
  const channels = routes[kind];
  return !channels || channels.includes('browser');
}

export function showBrowserNotification(n: Notification): void {
  if (!browserRouteAllowsKind(n.kind)) return;
  if (readBrowserOnlyHidden() && document.visibilityState === 'visible') return;
  try {
    const note = new Notification(n.title, {
      body: n.body || undefined,
      icon: '/favicon.svg',
      tag: `xissue-${n.id}`,
    });
    note.onclick = () => {
      window.focus();
      if (n.link) window.location.assign(n.link);
      note.close();
    };
  } catch {
    // Some browsers refuse Notification construction outside a user gesture.
  }
}
