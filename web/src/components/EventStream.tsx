import { useEffect, useLayoutEffect, useMemo, useRef } from 'react';
import { useMutation } from '@tanstack/react-query';
import { api } from '../api';
import type { PermissionOption, PlanEntry, RunEvent } from '../types';
import { Markdown, RUN_STATUS_LABEL } from './common';

// ---------- 视图模型 ----------
interface ToolItem {
  kind: 'tool';
  key: string;
  toolCallId: string;
  title: string;
  toolKind: string;
  status: string;
  rawInput?: unknown;
  content: unknown[];
}
interface PermItem {
  kind: 'perm';
  key: string;
  requestId: string;
  title: string;
  toolCall: unknown;
  options: PermissionOption[];
  resolved?: { optionId: string; auto: boolean };
}
type TextKind = 'message' | 'thought' | 'user' | 'stderr' | 'system';
interface TextItem {
  kind: TextKind;
  key: string;
  text: string;
}
interface StatusItem {
  kind: 'status';
  key: string;
  status: string;
}
type Item = ToolItem | PermItem | TextItem | StatusItem;

function asText(v: unknown): string {
  if (v == null) return '';
  return typeof v === 'string' ? v : JSON.stringify(v);
}

function toArray(v: unknown): unknown[] {
  if (v == null) return [];
  return Array.isArray(v) ? v : [v];
}

export interface BuiltView {
  items: Item[];
  plan: PlanEntry[] | null;
  pendingPerms: PermItem[];
}

const TERMINAL_RUN_STATUSES = new Set(['completed', 'failed', 'cancelled', 'submitted', 'discarded']);

export function buildView(events: RunEvent[]): BuiltView {
  const items: Item[] = [];
  const tools = new Map<string, ToolItem>();
  const perms = new Map<string, PermItem>();
  let plan: PlanEntry[] | null = null;

  const settleOpenToolsAndPerms = (finalToolStatus: 'completed' | 'failed') => {
    for (const t of tools.values()) {
      if (t.status === 'in_progress' || t.status === 'pending') {
        t.status = finalToolStatus;
      }
    }
    for (const p of perms.values()) {
      if (!p.resolved) {
        p.resolved = { optionId: 'cancelled', auto: true };
      }
    }
  };

  for (const ev of events) {
    const d = ev.data ?? {};
    switch (ev.type) {
      case 'message':
      case 'thought':
      case 'stderr': {
        const last = items[items.length - 1];
        const text = asText(d.text);
        if (last && last.kind === ev.type) {
          (last as TextItem).text += text;
        } else {
          items.push({ kind: ev.type, key: `e${ev.seq}`, text });
        }
        break;
      }
      case 'user':
        // Starting a new user follow-up turn settles any dangling tools from the previous turn
        settleOpenToolsAndPerms('completed');
        items.push({ kind: 'user', key: `e${ev.seq}`, text: asText(d.text) });
        break;
      case 'system':
        items.push({ kind: 'system', key: `e${ev.seq}`, text: asText(d.text) });
        break;
      case 'status': {
        const st = asText(d.status);
        if (TERMINAL_RUN_STATUSES.has(st)) {
          settleOpenToolsAndPerms(st === 'failed' || st === 'cancelled' ? 'failed' : 'completed');
          if (st === 'completed' && plan) {
            plan = plan.map((entry) =>
              entry.status === 'in_progress' ? { ...entry, status: 'completed' } : entry,
            );
          }
        }
        items.push({ kind: 'status', key: `e${ev.seq}`, status: st });
        break;
      }
      case 'tool_call': {
        const id = asText(d.toolCallId) || `seq${ev.seq}`;
        const existing = tools.get(id);
        if (existing) {
          Object.assign(existing, {
            title: d.title ?? existing.title,
            toolKind: d.kind ?? existing.toolKind,
            status: d.status ?? existing.status,
            rawInput: d.rawInput ?? existing.rawInput,
          });
          if (d.content) existing.content = toArray(d.content);
        } else {
          const t: ToolItem = {
            kind: 'tool',
            key: `t${id}`,
            toolCallId: id,
            title: asText(d.title) || '工具调用',
            toolKind: asText(d.kind),
            status: asText(d.status) || 'pending',
            rawInput: d.rawInput,
            content: toArray(d.content),
          };
          tools.set(id, t);
          items.push(t);
        }
        break;
      }
      case 'tool_call_update': {
        const id = asText(d.toolCallId);
        let t = tools.get(id);
        if (!t) {
          t = {
            kind: 'tool',
            key: `t${id || ev.seq}`,
            toolCallId: id,
            title: asText(d.title) || '工具调用',
            toolKind: '',
            status: 'pending',
            content: [],
          };
          tools.set(id, t);
          items.push(t);
        }
        if (d.status) t.status = d.status;
        if (d.title) t.title = d.title;
        if (d.rawInput) t.rawInput = d.rawInput;
        if (d.content != null) t.content = toArray(d.content); // ACP 中 content 为替换语义
        break;
      }
      case 'plan':
        plan = Array.isArray(d.entries) ? d.entries : [];
        break;
      case 'permission_request': {
        const p: PermItem = {
          kind: 'perm',
          key: `p${d.requestId}`,
          requestId: asText(d.requestId),
          title: asText(d.title) || '权限请求',
          toolCall: d.toolCall,
          options: Array.isArray(d.options) ? d.options : [],
        };
        perms.set(p.requestId, p);
        items.push(p);
        break;
      }
      case 'permission_resolved': {
        const p = perms.get(asText(d.requestId));
        if (p) p.resolved = { optionId: asText(d.optionId), auto: !!d.auto };
        break;
      }
    }
  }
  return { items, plan, pendingPerms: [...perms.values()].filter((p) => !p.resolved) };
}

// ---------- 渲染 ----------
function ToolContent({ c }: { c: unknown }) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const x = c as any;
  if (x && typeof x === 'object') {
    if (x.type === 'content' && x.content) {
      if (x.content.type === 'text') return <pre>{x.content.text}</pre>;
      return <pre>{JSON.stringify(x.content, null, 2)}</pre>;
    }
    if (x.type === 'diff') {
      return (
        <div>
          <div className="small muted mono">diff: {x.path}</div>
          <pre>{diffPreview(x.oldText ?? '', x.newText ?? '')}</pre>
        </div>
      );
    }
    if (x.type === 'terminal') return <div className="small dim mono">terminal: {x.terminalId}</div>;
    if (x.type === 'text' && typeof x.text === 'string') return <pre>{x.text}</pre>;
  }
  return <pre>{typeof c === 'string' ? c : JSON.stringify(c, null, 2)}</pre>;
}

function diffPreview(oldText: string, newText: string): string {
  const o = oldText.split('\n');
  const n = newText.split('\n');
  if (!oldText) return n.map((l) => '+ ' + l).join('\n');
  // 简化展示：去掉公共前后缀
  let s = 0;
  while (s < o.length && s < n.length && o[s] === n[s]) s++;
  let eo = o.length - 1;
  let en = n.length - 1;
  while (eo >= s && en >= s && o[eo] === n[en]) {
    eo--;
    en--;
  }
  return [...o.slice(s, eo + 1).map((l) => '- ' + l), ...n.slice(s, en + 1).map((l) => '+ ' + l)].join('\n');
}

const TOOL_STATUS_CLASS: Record<string, string> = {
  pending: 'st-queued',
  in_progress: 'st-running',
  completed: 'st-completed',
  failed: 'st-failed',
};

const TOOL_STATUS_LABEL: Record<string, string> = {
  pending: '等待中',
  in_progress: '执行中…',
  completed: '已完成',
  failed: '失败',
};

function ToolView({ t }: { t: ToolItem }) {
  return (
    <details className="tool">
      <summary>
        <span className={`dot ${TOOL_STATUS_CLASS[t.status] ?? ''}`} title={t.status} />
        {t.toolKind && <span className="tool-kind">{t.toolKind}</span>}
        <span className="grow ellipsis">{t.title}</span>
        <span className="dim small">{TOOL_STATUS_LABEL[t.status] ?? t.status}</span>
      </summary>
      <div className="tool-body">
        {t.rawInput !== undefined && (
          <div>
            <div className="small dim">输入</div>
            <pre>{typeof t.rawInput === 'string' ? t.rawInput : JSON.stringify(t.rawInput, null, 2)}</pre>
          </div>
        )}
        {t.content.map((c, i) => (
          <ToolContent key={i} c={c} />
        ))}
        {t.rawInput === undefined && t.content.length === 0 && <div className="small dim">无详情</div>}
      </div>
    </details>
  );
}

function PermView({ p, runId }: { p: PermItem; runId: number }) {
  const respond = useMutation({
    mutationFn: (optionId: string) => api.respondPermission(runId, p.requestId, optionId),
  });
  const chosen = p.resolved && p.options.find((o) => o.optionId === p.resolved!.optionId);
  return (
    <div className={`perm${p.resolved ? ' resolved' : ''}`}>
      <div className="row">
        <b className="grow">🔐 {p.title}</b>
        {p.resolved && (
          <span className="small muted">
            已处理：{chosen?.name ?? p.resolved.optionId}
            {p.resolved.auto ? '（自动）' : ''}
          </span>
        )}
      </div>
      {p.toolCall != null && (
        <details>
          <summary className="small muted">详情</summary>
          <pre className="small" style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
            {JSON.stringify(p.toolCall, null, 2)}
          </pre>
        </details>
      )}
      {!p.resolved && (
        <div className="row" style={{ flexWrap: 'wrap' }}>
          {p.options.map((o) => (
            <button
              key={o.optionId}
              className={o.kind.startsWith('allow') ? 'allow' : 'reject'}
              disabled={respond.isPending}
              onClick={() => respond.mutate(o.optionId)}
            >
              {o.name}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function PlanView({ plan }: { plan: PlanEntry[] }) {
  const done = plan.filter((e) => e.status === 'completed').length;
  return (
    <div className="plan">
      <div className="row small">
        <b className="grow">计划</b>
        <span className="dim">
          {done}/{plan.length}
        </span>
      </div>
      <ul>
        {plan.map((e, i) => (
          <li key={i} className={e.status}>
            <span>{e.status === 'completed' ? '☑' : e.status === 'in_progress' ? '◐' : '☐'}</span>
            <span className="grow">{e.content}</span>
            {e.priority === 'high' && <span className="badge">high</span>}
          </li>
        ))}
      </ul>
    </div>
  );
}

export function EventStream({ runId, events }: { runId: number; events: RunEvent[] }) {
  const view = useMemo(() => buildView(events), [events]);
  const ref = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  useEffect(() => {
    stick.current = true;
  }, [runId]);

  useLayoutEffect(() => {
    const el = ref.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [view]);

  const onScroll = () => {
    const el = ref.current;
    if (el) stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
  };

  return (
    <div className="run-log" ref={ref} onScroll={onScroll}>
      {view.plan && view.plan.length > 0 && <PlanView plan={view.plan} />}
      {view.items.length === 0 && <div className="empty">暂无事件</div>}
      {view.items.map((it) => {
        switch (it.kind) {
          case 'message':
            return (
              <div key={it.key} className="ev-msg">
                <Markdown>{it.text}</Markdown>
              </div>
            );
          case 'thought':
            return (
              <div key={it.key} className="ev-thought">
                {it.text}
              </div>
            );
          case 'user':
            return (
              <div key={it.key} className="ev-user">
                {it.text}
              </div>
            );
          case 'stderr':
            return (
              <div key={it.key} className="ev-stderr">
                {it.text}
              </div>
            );
          case 'system':
            return (
              <div key={it.key} className="ev-sys">
                {it.text}
              </div>
            );
          case 'status':
            return (
              <div key={it.key} className="ev-status">
                {RUN_STATUS_LABEL[it.status] ?? it.status}
              </div>
            );
          case 'tool':
            return <ToolView key={it.key} t={it} />;
          case 'perm':
            return <PermView key={it.key} p={it} runId={runId} />;
        }
      })}
    </div>
  );
}
