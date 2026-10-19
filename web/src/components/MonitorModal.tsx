import React, { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import type { BranchWatch } from '../types';

interface MonitorModalProps {
  onClose: () => void;
}

export const MonitorModal: React.FC<MonitorModalProps> = ({ onClose }) => {
  const qc = useQueryClient();
  const [hours, setHours] = useState<number>(8);
  const [levelFilter, setLevelFilter] = useState<string>('all');
  const [copied, setCopied] = useState(false);

  const statusQ = useQuery({
    queryKey: qk.monitorStatus,
    queryFn: () => api.monitorStatus(),
    refetchInterval: 5000,
  });

  const logsQ = useQuery({
    queryKey: qk.monitorLogs(hours),
    queryFn: () => api.monitorLogs(hours),
    refetchInterval: 5000,
  });

  const triggerMut = useMutation({
    mutationFn: () => api.triggerMonitor(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.monitorStatus });
      qc.invalidateQueries({ queryKey: qk.monitorLogs(hours) });
    },
  });

  const pruneMut = useMutation({
    mutationFn: () => api.pruneMonitorLogs(hours),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.monitorLogs(hours) });
    },
  });

  const status = statusQ.data;
  const rawLogs = logsQ.data ?? [];

  const logs = rawLogs.filter((l) => {
    if (levelFilter === 'all') return true;
    return l.level === levelFilter;
  });

  const copyLogs = () => {
    const text = logs
      .map((l) => `[${new Date(l.timestamp).toLocaleTimeString()}] [${l.level}] [${l.source}] ${l.message}`)
      .join('\n');
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="modal-backdrop" onClick={onClose} style={{ zIndex: 110 }}>
      <div
        className="modal"
        onClick={(e) => e.stopPropagation()}
        style={{ width: 840, maxWidth: '95vw', maxHeight: '90vh', display: 'flex', flexDirection: 'column' }}
      >
        <div className="modal-head" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', borderBottom: '1px solid var(--border)', paddingBottom: 12 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span style={{ fontSize: 18 }}>📡</span>
            <b style={{ fontSize: 16 }}>服务监控与运行日志</b>
            <span
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 5,
                background: 'rgba(34, 197, 94, 0.15)',
                color: '#22c55e',
                borderRadius: 999,
                padding: '2px 8px',
                fontSize: 12,
                fontWeight: 600,
              }}
            >
              <span style={{ width: 6, height: 6, borderRadius: '50%', background: '#22c55e' }} />
              轮询正常 (90s)
            </span>
          </div>
          <button className="ghost sm" onClick={onClose}>
            ✕
          </button>
        </div>

        {/* 顶部状态面板 */}
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(4, 1fr)',
            gap: 12,
            padding: '14px 0',
            borderBottom: '1px solid var(--border)',
          }}
        >
          <div className="card" style={{ padding: 10, textAlign: 'center' }}>
            <div className="dim small">监控分支数</div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 4 }}>
              {status?.watchesCount ?? 0}
            </div>
          </div>
          <div className="card" style={{ padding: 10, textAlign: 'center' }}>
            <div className="dim small">跟踪开放 PR</div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 4 }}>
              {status?.openPRsCount ?? 0}
            </div>
          </div>
          <div className="card" style={{ padding: 10, textAlign: 'center' }}>
            <div className="dim small">上次检查耗时</div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 4, color: 'var(--primary)' }}>
              {status?.lastDurationMs ? `${status.lastDurationMs}ms` : '--'}
            </div>
          </div>
          <div className="card" style={{ padding: 10, textAlign: 'center' }}>
            <div className="dim small">上次轮询时间</div>
            <div style={{ fontSize: 13, fontWeight: 600, marginTop: 7 }}>
              {status?.lastCheckTime ? new Date(status.lastCheckTime).toLocaleTimeString() : '刚刚'}
            </div>
          </div>
        </div>

        {/* 正在监控的分支简要 */}
        {status?.watchedBranches && status.watchedBranches.length > 0 && (
          <div style={{ padding: '10px 0', borderBottom: '1px solid var(--border)' }}>
            <div className="dim small" style={{ marginBottom: 6 }}>
              当前监控分支：
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {status.watchedBranches.map((w: BranchWatch) => (
                <span
                  key={w.id}
                  style={{
                    background: 'var(--hover-bg)',
                    border: '1px solid var(--border)',
                    borderRadius: 4,
                    padding: '2px 8px',
                    fontSize: 12,
                    fontFamily: 'monospace',
                  }}
                  title={w.lastMessage ? `${w.lastMessage} (${w.lastAuthor})` : undefined}
                >
                  <b>{w.repoName}</b>:{w.branch} <span className="dim">({w.lastSha ? w.lastSha.slice(0, 7) : 'init'})</span>
                </span>
              ))}
            </div>
          </div>
        )}

        {/* 操作工具栏 */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '10px 0',
            flexWrap: 'wrap',
            gap: 8,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span className="dim small">日志范围:</span>
            <select
              className="sm"
              value={hours}
              onChange={(e) => setHours(Number(e.target.value))}
              style={{ width: 120 }}
            >
              <option value={8}>最近 8 小时</option>
              <option value={24}>当天 (24小时)</option>
              <option value={2}>最近 2 小时</option>
            </select>

            <span className="dim small" style={{ marginLeft: 6 }}>级别:</span>
            <select
              className="sm"
              value={levelFilter}
              onChange={(e) => setLevelFilter(e.target.value)}
              style={{ width: 100 }}
            >
              <option value="all">全部</option>
              <option value="EVENT">事件 (EVENT)</option>
              <option value="INFO">信息 (INFO)</option>
              <option value="WARN">警告 (WARN)</option>
              <option value="ERROR">错误 (ERROR)</option>
            </select>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <button
              className="ghost sm"
              onClick={copyLogs}
              title="复制当前过滤的日志文本"
            >
              {copied ? '✓ 已复制' : '📋 复制日志'}
            </button>
            <button
              className="ghost sm"
              disabled={pruneMut.isPending}
              onClick={() => pruneMut.mutate()}
              title="立即清理并裁剪超过当前时间范围的历史旧日志"
            >
              🧹 清理旧日志
            </button>
            <button
              className="primary sm"
              disabled={triggerMut.isPending}
              onClick={() => triggerMut.mutate()}
              title="立即手动触发一次所有分支与 PR 状态检查"
            >
              {triggerMut.isPending ? '检查中...' : '⚡ 立即检查'}
            </button>
          </div>
        </div>

        {/* 日志内容区域 */}
        <div
          style={{
            flex: 1,
            minHeight: 280,
            maxHeight: 380,
            overflowY: 'auto',
            background: 'var(--bg-inset, rgba(0,0,0,0.25))',
            border: '1px solid var(--border)',
            borderRadius: 6,
            padding: 10,
            fontFamily: 'monospace',
            fontSize: 12,
            lineHeight: 1.6,
          }}
        >
          {logs.length === 0 ? (
            <div className="empty" style={{ padding: 40 }}>
              暂无监控日志记录
            </div>
          ) : (
            logs.map((l, idx) => {
              let color = 'var(--fg-dim)';
              if (l.level === 'EVENT') color = '#38bdf8';
              if (l.level === 'WARN') color = '#fbbf24';
              if (l.level === 'ERROR') color = '#f87171';
              if (l.level === 'INFO') color = 'var(--fg)';

              return (
                <div key={idx} style={{ display: 'flex', gap: 8, marginBottom: 4 }}>
                  <span className="dim" style={{ flexShrink: 0, color: 'var(--dim)' }}>
                    {new Date(l.timestamp).toLocaleTimeString()}
                  </span>
                  <span
                    style={{
                      flexShrink: 0,
                      fontWeight: 700,
                      width: 54,
                      color,
                      fontSize: 11,
                    }}
                  >
                    [{l.level}]
                  </span>
                  <span className="dim" style={{ flexShrink: 0, width: 68 }}>
                    {l.source}:
                  </span>
                  <span style={{ color: l.level === 'ERROR' ? '#f87171' : 'inherit', wordBreak: 'break-all' }}>
                    {l.message}
                  </span>
                </div>
              );
            })
          )}
        </div>

        <div style={{ marginTop: 10, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span className="dim small">
            共 {logs.length} 条记录（自动保留当天/最近 8 小时，单机文件超过 5MB 自动截断）
          </span>
          <button className="ghost sm" onClick={onClose}>
            关闭
          </button>
        </div>
      </div>
    </div>
  );
};
