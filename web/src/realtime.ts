import { useEffect, useSyncExternalStore } from 'react';
import { QueryClient } from '@tanstack/react-query';
import { api, qk } from './api';
import { toast } from './toast';
import type { Issue, Notification, Run, RunEvent, WsMessage } from './types';
import { showBrowserNotification } from './notificationSettings';

/** 合并事件：按 seq 去重并排序 */
export function mergeEvents(old: RunEvent[] | undefined, incoming: RunEvent[]): RunEvent[] {
  if (!old || old.length === 0) return [...incoming].sort((a, b) => a.seq - b.seq);
  if (incoming.length === 0) return old;
  const maxOld = old[old.length - 1].seq;
  // 快速路径：全部是新的且有序
  if (incoming.every((e, i) => e.seq > maxOld && (i === 0 || e.seq > incoming[i - 1].seq))) {
    return [...old, ...incoming];
  }
  const map = new Map<number, RunEvent>();
  for (const e of old) map.set(e.seq, e);
  for (const e of incoming) map.set(e.seq, e);
  return [...map.values()].sort((a, b) => a.seq - b.seq);
}

export function maxSeq(events: RunEvent[] | undefined): number | undefined {
  return events && events.length ? events[events.length - 1].seq : undefined;
}

/** 事件 queryFn：首次全量拉取，之后以 after=seq 增量补齐 */
export function fetchRunEvents(qc: QueryClient, runId: number) {
  return async (): Promise<RunEvent[]> => {
    const existing = qc.getQueryData<RunEvent[]>(qk.events(runId));
    const after = maxSeq(existing);
    const fetched = await api.events(runId, after);
    // 在请求期间 WS 可能已追加了事件，合并当前缓存
    const current = qc.getQueryData<RunEvent[]>(qk.events(runId));
    return mergeEvents(current ?? existing, fetched ?? []);
  };
}

function upsertById<T extends { id: number }>(list: T[] | undefined, item: T, prepend = false): T[] | undefined {
  if (!list) return list;
  const idx = list.findIndex((x) => x.id === item.id);
  if (idx === -1) return prepend ? [item, ...list] : [...list, item];
  const copy = list.slice();
  copy[idx] = item;
  return copy;
}

// ---- 连接状态（供 UI 显示） ----
type WsState = 'connecting' | 'open' | 'closed';
let wsState: WsState = 'connecting';
const stateListeners = new Set<() => void>();
function setWsState(s: WsState) {
  wsState = s;
  stateListeners.forEach((l) => l());
}
export function useWsState(): WsState {
  return useSyncExternalStore(
    (cb) => {
      stateListeners.add(cb);
      return () => stateListeners.delete(cb);
    },
    () => wsState,
  );
}

function handleMessage(qc: QueryClient, msg: WsMessage) {
  switch (msg.type) {
    case 'run_event': {
      const key = qk.events(msg.runId);
      const cur = qc.getQueryData<RunEvent[]>(key);
      // 尚未加载历史时不建缓存，避免之后 after=seq 漏掉历史
      if (!cur) return;
      const last = maxSeq(cur);
      qc.setQueryData<RunEvent[]>(key, mergeEvents(cur, [msg.event]));
      if (last !== undefined && msg.event.seq > last + 1) {
        // 出现缺口，增量补齐
        qc.invalidateQueries({ queryKey: key });
      }
      break;
    }
    case 'run_updated': {
      const run: Run = msg.run;
      qc.setQueryData(qk.run(run.id), run);
      const runsKey = qk.runs(run.issueId);
      if (qc.getQueryData<Run[]>(runsKey)) {
        qc.setQueryData<Run[]>(runsKey, (old) => upsertById(old, run, true));
      } else {
        qc.invalidateQueries({ queryKey: runsKey });
      }
      if (!['queued', 'starting', 'running', 'awaiting_permission'].includes(run.status)) {
        // 结束后补齐尾部事件并刷新变更
        qc.invalidateQueries({ queryKey: qk.events(run.id) });
        qc.invalidateQueries({ queryKey: qk.changes(run.id) });
        qc.invalidateQueries({ queryKey: qk.comments(run.issueId) });
        qc.invalidateQueries({ queryKey: qk.issueEvents(run.issueId) });
      }
      break;
    }
    case 'issue_updated': {
      const issue: Issue = msg.issue;
      qc.setQueryData(qk.issue(issue.id), issue);
      const issuesKey = qk.issues(issue.projectId);
      if (qc.getQueryData<Issue[]>(issuesKey)) {
        qc.setQueryData<Issue[]>(issuesKey, (old) => upsertById(old, issue));
      } else {
        qc.invalidateQueries({ queryKey: issuesKey });
      }
      qc.invalidateQueries({ queryKey: qk.issueEvents(issue.id) });
      break;
    }
    case 'notification': {
      const n: Notification = msg.notification;
      qc.setQueryData<Notification[]>(qk.notifications, (old) => (old ? [n, ...old] : [n]));
      qc.invalidateQueries({ queryKey: qk.notifications });
      if (n.kind === 'branch_push') {
        qc.invalidateQueries({ queryKey: ['watches'] });
      }
      toast.info(`🔔 ${n.title}`);
      showBrowserNotification(n);
      break;
    }
    case 'status':
      qc.setQueryData(qk.status, msg.status);
      break;
    case 'heartbeat':
      break;
  }
}

/** 建立单个 WebSocket 连接，断线指数退避重连 */
export function useRealtime(qc: QueryClient) {
  useEffect(() => {
    let ws: WebSocket | null = null;
    let stopped = false;
    let retry = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let watchdogTimer: ReturnType<typeof setInterval> | undefined;
    let everOpened = false;
    let lastSeen = Date.now();

    const connect = () => {
      if (stopped) return;
      setWsState('connecting');
      const proto = location.protocol === 'https:' ? 'wss' : 'ws';
      let url = `${proto}://${location.host}/api/ws`;
      try {
        const token = localStorage.getItem('xissue_token');
        if (token) {
          url += `?token=${encodeURIComponent(token)}`;
        }
      } catch {
        // ignore storage errors
      }
      ws = new WebSocket(url);
      lastSeen = Date.now();

      ws.onopen = () => {
        retry = 0;
        lastSeen = Date.now();
        setWsState('open');
        if (everOpened) {
          // 重连后补齐：已缓存的事件按 after=seq 增量拉取，其余数据刷新
          qc.invalidateQueries();
        }
        everOpened = true;
      };
      ws.onmessage = (ev) => {
        lastSeen = Date.now();
        try {
          const data = JSON.parse(ev.data) as WsMessage;
          if (data.type === 'heartbeat') {
            return;
          }
          handleMessage(qc, data);
        } catch (e) {
          console.warn('ws message error', e);
        }
      };
      ws.onclose = () => {
        setWsState('closed');
        if (stopped) return;
        const delay = Math.min(30000, 1000 * 2 ** retry++);
        timer = setTimeout(connect, delay);
      };
      ws.onerror = () => ws?.close();
    };

    connect();

    // 1. 心跳 Watchdog：若超过 45s 未收到任何通信，判定连接已半开挂死，主动重连
    watchdogTimer = setInterval(() => {
      if (stopped) return;
      if (ws && ws.readyState === WebSocket.OPEN) {
        if (Date.now() - lastSeen > 45000) {
          console.warn('[xissue-ws] 心跳超时（>45s 无数据），主动重置连接');
          ws.close();
        }
      }
    }, 15000);

    // 2. 页面可见性检测：从后台/休眠切回前台时，主动探测并复活连接
    const onVisibility = () => {
      if (document.visibilityState === 'visible' && !stopped) {
        if (!ws || ws.readyState !== WebSocket.OPEN) {
          clearTimeout(timer);
          connect();
        } else {
          qc.invalidateQueries();
        }
      }
    };
    document.addEventListener('visibilitychange', onVisibility);

    return () => {
      stopped = true;
      clearTimeout(timer);
      clearInterval(watchdogTimer);
      document.removeEventListener('visibilitychange', onVisibility);
      ws?.close();
    };
  }, [qc]);
}
