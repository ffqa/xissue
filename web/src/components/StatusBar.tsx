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

export function StatusBar() {
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
  const restart = useMutation({
    mutationFn: api.restartServer,
    onSuccess: () => toast.info('服务正在重启，WebSocket 将自动重连…'),
    onError: (e) => toast.error((e as Error).message),
  });
  const st = q.data;
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
      <span className="status-item status-version">v{st.version}</span>
      {st.latestUpdate && <span className="status-item status-update">↑ v{st.latestUpdate}</span>}
      <span className="status-item">UP {fmtUptime(st.uptimeSeconds)}</span>
      <span className="status-item" title={`HeapInuse ${fmtBytes(st.memory.heapInuse)} · Sys ${fmtBytes(st.memory.sys)} · Objects ${st.memory.heapObjects}`}>
        MEM {fmtBytes(st.memory.heapAlloc)}
      </span>
      <span className="status-item" title={`进程 CPU 占单核百分比，${st.cpu.cores} 核`}>
        CPU {st.cpu.processPercent.toFixed(1)}%
      </span>
      <span className="status-item" title={st.disk.error || st.disk.path}>
        DISK {fmtBytes(st.disk.free)} free
        {st.disk.total > 0 ? ` · ${st.disk.usedPercent.toFixed(0)}% used` : ''}
      </span>
      <span className="status-item" title={`下次 GC 堆目标 ${fmtBytes(st.memory.nextGC)} · 已 GC ${st.gc.numGC} 次`}>
        {gc}
      </span>
      <span className="status-item">G {st.goroutines}</span>
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
