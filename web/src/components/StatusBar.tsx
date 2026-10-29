import { useMemo } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { api, qk } from '../api';
import { useWsState } from '../realtime';
import { toast } from '../toast';

function fmtBytes(n: number): string {
  if (!n || n < 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

function fmtUptime(sec: number): string {
  const s = Math.max(0, Math.floor(sec));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m ${s % 60}s`;
}

function fmtCountdown(ms: number): string {
  const sec = Math.max(0, Math.ceil(ms / 1000));
  if (sec >= 3600) return `${Math.floor(sec / 3600)}h`;
  if (sec >= 60) return `${Math.floor(sec / 60)}m ${sec % 60}s`;
  return `${sec}s`;
}

export function StatusBar({ onOpenSchedules }: { onOpenSchedules?: () => void }) {
  const wsState = useWsState();
  const q = useQuery({
    queryKey: qk.status,
    queryFn: api.status,
    // Normal path: status is pushed over the existing WebSocket. Only poll
    // HTTP while the socket is down (or still connecting).
    enabled: wsState !== 'open',
    refetchInterval: wsState === 'open' ? false : 2000,
    refetchIntervalInBackground: true,
    staleTime: Infinity,
    retry: false,
  });
  const schedulesQ = useQuery({
    queryKey: qk.schedules,
    queryFn: api.schedules,
    refetchInterval: 5000,
    staleTime: 4000,
    retry: false,
  });
  const nextTask = useMemo(() => {
    const now = Date.now();
    return (schedulesQ.data ?? [])
      .filter((task) => task.enabled && task.nextRun && new Date(task.nextRun).getTime() > now - 5000)
      .sort((a, b) => new Date(a.nextRun!).getTime() - new Date(b.nextRun!).getTime())[0];
  }, [schedulesQ.data]);
  const nextRemaining = nextTask?.nextRun ? Math.max(0, new Date(nextTask.nextRun).getTime() - Date.now()) : 0;
  const nextProgress = nextTask && nextTask.intervalSeconds > 0
    ? Math.max(0, Math.min(100, 100 - (nextRemaining / (nextTask.intervalSeconds * 1000)) * 100))
    : 0;
  const restart = useMutation({
    mutationFn: api.restartServer,
    onSuccess: () => toast.info('服务正在重启，WebSocket 将自动重连…'),
    onError: (e) => toast.error((e as Error).message),
  });
  const st = q.data;
  const queue = st?.queue ?? { pending: 0, running: 0, maxConcurrent: 0, maxQueued: 0 };
  const wsLabel = wsState === 'open' ? 'WS 实时' : wsState === 'connecting' ? 'WS 连接中…' : 'HTTP 兜底';
  if (!st) {
    return (
      <footer className="statusbar">
        <span className={`ws-ind ${wsState}`}>
          <span className="dot" /> {wsLabel}
        </span>
        <span className="dim">{q.isError ? '状态采集不可用' : '状态采集中…'}</span>
        <span className="grow" />
        <button
          type="button"
          className="ghost sm status-restart"
          title="重启 xIssue 服务"
          disabled={restart.isPending}
          onClick={() => restart.mutate()}
        >
          {restart.isPending ? '重启中…' : '↻ 重启'}
        </button>
      </footer>
    );
  }
  const gc =
    st.gc.estimatedSec > 0
      ? `GC ~${st.gc.estimatedSec < 60 ? `${Math.round(st.gc.estimatedSec)}s` : `${Math.round(st.gc.estimatedSec / 60)}m`}`
      : st.gc.numGC > 0
        ? `GC #${st.gc.numGC}`
        : 'GC 待采样';
  return (
    <footer className="statusbar" title={`PID ${st.pid} · ${st.goVersion} · ${st.os}/${st.arch}`}>
      <a
        className="status-item status-version"
        href="https://github.com/ffqa/xissue"
        target="_blank"
        rel="noreferrer"
        title="打开 xIssue 官方仓库"
      >
        <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" fill="currentColor">
          <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.4 7.4 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
        </svg>
        <span>v{st.version}</span>
      </a>
      {st.latestUpdate && <span className="status-item status-update">↑ v{st.latestUpdate}</span>}
      <span className="status-item">UP {fmtUptime(st.uptimeSeconds)}</span>
      <span className="status-item" title={`HeapInuse ${fmtBytes(st.memory.heapInuse)} · Sys ${fmtBytes(st.memory.sys)} · Objects ${st.memory.heapObjects}`}>
        MEM {fmtBytes(st.memory.heapAlloc)}
      </span>
      <span className="status-item" title={`进程 CPU 占单核百分比，${st.cpu.cores} 核`}>
        CPU {st.cpu.processPercent.toFixed(1)}%
      </span>
      <span
        className="status-item"
        title={`${st.disk.error || st.disk.path}\n数据库 ${fmtBytes(st.disk.database)} · 工作区 ${fmtBytes(st.disk.runWorkspaces)}`}
      >
        DATA {fmtBytes(st.disk.dataUsed)}
      </span>
      <span className="status-item" title={st.disk.error || st.disk.path}>
        DISK {fmtBytes(st.disk.free)} free
        {st.disk.total > 0 ? ` · ${st.disk.usedPercent.toFixed(0)}% used` : ''}
      </span>
      <span className="status-item" title={`下次 GC 堆目标 ${fmtBytes(st.memory.nextGC)} · 已 GC ${st.gc.numGC} 次`}>
        {gc}
      </span>
      <span
        className="status-item"
        title={`Agent 运行 ${queue.running}/${queue.maxConcurrent} · 排队 ${queue.pending}${queue.maxQueued > 0 ? ` · 队列上限 ${queue.maxQueued}` : ' · 队列不限'}`}
      >
        RUN {queue.running}/{queue.maxConcurrent}
        {queue.pending > 0 ? ` +Q${queue.pending}` : ''}
      </span>
      <span className="status-item">G {st.goroutines}</span>
      <button
        type="button"
        className="status-item status-schedule"
        title={nextTask ? `${nextTask.name} · ${nextTask.interval} · 下次 ${fmtCountdown(nextRemaining)}` : '查看后台定时任务'}
        onClick={onOpenSchedules}
        disabled={!onOpenSchedules}
      >
        <span>⏱ {nextTask ? nextTask.name : `${schedulesQ.data?.filter((t) => t.enabled).length ?? 0} 个任务`}</span>
        {nextTask && (
          <>
            <span className="status-progress"><span style={{ width: `${nextProgress}%` }} /></span>
            <span className="dim">{fmtCountdown(nextRemaining)}</span>
          </>
        )}
      </button>
      <span className="grow" />
      <span className={`ws-ind ${wsState}`} title="状态通道：WebSocket 推送；断线时自动切换 HTTP 轮询">
        <span className="dot" /> {wsLabel}
      </span>
      <span className="status-item dim">{st.autoUpdate ? '自动更新已开启' : '自动更新已关闭'}</span>
      <button
        type="button"
        className="ghost sm status-restart"
        title="重启 xIssue 服务（编译新版本后点击即可生效）"
        disabled={restart.isPending}
        onClick={() => restart.mutate()}
      >
        {restart.isPending ? '重启中…' : '↻ 重启'}
      </button>
    </footer>
  );
}
