import { useQuery } from '@tanstack/react-query';
import { api, qk } from '../api';

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
  const q = useQuery({
    queryKey: qk.status,
    queryFn: api.status,
    refetchInterval: 2000,
    refetchIntervalInBackground: true,
    staleTime: 1000,
    retry: false,
  });
  const st = q.data;
  if (!st) {
    return (
      <footer className="statusbar">
        <span className="dim">{q.isError ? '状态采集不可用' : '状态采集中…'}</span>
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
      <span className="status-item dim">
        {st.autoUpdate ? '自动更新已开启' : '自动更新已关闭'} · 实时
      </span>
    </footer>
  );
}
