import { useEffect, useSyncExternalStore } from 'react';
import { QueryClient } from '@tanstack/react-query';
import { api, qk } from './api';
import type { Issue, Run, RunEvent, WsMessage } from './types';

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
      qc.setQueryData<Run[]>(qk.runs(run.issueId), (old) => upsertById(old, run, true));
      qc.invalidateQueries({ queryKey: qk.runs(run.issueId) });
      if (!['queued', 'starting', 'running', 'awaiting_permission'].includes(run.status)) {
        // 结束后补齐尾部事件并刷新变更
        qc.invalidateQueries({ queryKey: qk.events(run.id) });
        qc.invalidateQueries({ queryKey: qk.changes(run.id) });
        qc.invalidateQueries({ queryKey: qk.comments(run.issueId) });
      }
      break;
    }
    case 'issue_updated': {
      const issue: Issue = msg.issue;
      qc.setQueryData(qk.issue(issue.id), issue);
      qc.setQueryData<Issue[]>(qk.issues(issue.projectId), (old) => upsertById(old, issue));
      qc.invalidateQueries({ queryKey: qk.issues(issue.projectId) });
      break;
    }
  }
}

/** 建立单个 WebSocket 连接，断线指数退避重连 */
export function useRealtime(qc: QueryClient) {
  useEffect(() => {
    let ws: WebSocket | null = null;
    let stopped = false;
    let retry = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let everOpened = false;

    const connect = () => {
      if (stopped) return;
      setWsState('connecting');
      const proto = location.protocol === 'https:' ? 'wss' : 'ws';
      ws = new WebSocket(`${proto}://${location.host}/api/ws`);
      ws.onopen = () => {
        retry = 0;
        setWsState('open');
        if (everOpened) {
          // 重连后补齐：已缓存的事件按 after=seq 增量拉取，其余数据刷新
          qc.invalidateQueries();
        }
        everOpened = true;
      };
      ws.onmessage = (ev) => {
        try {
          handleMessage(qc, JSON.parse(ev.data) as WsMessage);
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
    return () => {
      stopped = true;
      clearTimeout(timer);
      ws?.close();
    };
  }, [qc]);
}
