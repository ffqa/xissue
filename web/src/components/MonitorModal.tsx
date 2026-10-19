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
  const [isMaximized, setIsMaximized] = useState(false);

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
    <div
      className="modal-backdrop"
      onClick={onClose}
      style={{
        zIndex: 110,
        paddingTop: isMaximized ? 0 : '4vh',
        alignItems: isMaximized ? 'stretch' : 'flex-start',
      }}
    >
      <div
        className="modal"
        onClick={(e) => e.stopPropagation()}
        style={{
          width: isMaximized ? '100vw' : 'min(940px, 95vw)',
          height: isMaximized ? '100vh' : 'min(860px, 90vh)',
          maxWidth: isMaximized ? '100vw' : '95vw',
          maxHeight: isMaximized ? '100vh' : '90vh',
          borderRadius: isMaximized ? 0 : 12,
          padding: isMaximized ? '16px 24px' : '20px 24px',
          display: 'flex',
          flexDirection: 'column',
          boxSizing: 'border-box',
          gap: 14,
          background: 'var(--bg-1)',
          border: isMaximized ? 'none' : '1px solid var(--border-strong)',
        }}
      >
        {/* 顶部标题栏与控制按钮 */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            borderBottom: '1px solid var(--border)',
            paddingBottom: 14,
            flex: 'none',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ fontSize: 20 }}>📡</span>
            <b style={{ fontSize: 17, color: 'var(--text)' }}>服务监控与运行日志</b>
            <span
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                background: 'rgba(76, 183, 130, 0.15)',
                color: 'var(--green)',
                border: '1px solid rgba(76, 183, 130, 0.3)',
                borderRadius: 999,
                padding: '2px 10px',
                fontSize: 12,
                fontWeight: 600,
              }}
            >
              <span style={{ width: 6, height: 6, borderRadius: '50%', background: 'var(--green)' }} />
              后台轮询正常 (90s)
            </span>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <button
              className="ghost sm"
              onClick={() => setIsMaximized(!isMaximized)}
              title={isMaximized ? '还原窗口' : '全屏显示'}
              style={{ fontSize: 13, padding: '4px 10px' }}
            >
              {isMaximized ? '🗗 还原' : '🗖 全屏'}
            </button>
            <button
              className="ghost sm"
              onClick={onClose}
              title="关闭 (Esc)"
              style={{ fontSize: 14, padding: '4px 8px' }}
            >
              ✕
            </button>
          </div>
        </div>

        {/* 顶部指标统计卡片组 */}
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(4, 1fr)',
            gap: 12,
            flex: 'none',
          }}
        >
          <div
            style={{
              padding: '12px 16px',
              borderRadius: 8,
              background: 'var(--bg-2)',
              border: '1px solid var(--border)',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--text-3)' }}>监控分支数</div>
            <div style={{ fontSize: 22, fontWeight: 700, marginTop: 4, color: 'var(--text)' }}>
              {status?.watchesCount ?? 0}
            </div>
          </div>
          <div
            style={{
              padding: '12px 16px',
              borderRadius: 8,
              background: 'var(--bg-2)',
              border: '1px solid var(--border)',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--text-3)' }}>跟踪开放 PR</div>
            <div style={{ fontSize: 22, fontWeight: 700, marginTop: 4, color: 'var(--text)' }}>
              {status?.openPRsCount ?? 0}
            </div>
          </div>
          <div
            style={{
              padding: '12px 16px',
              borderRadius: 8,
              background: 'var(--bg-2)',
              border: '1px solid var(--border)',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--text-3)' }}>上次检查耗时</div>
            <div style={{ fontSize: 22, fontWeight: 700, marginTop: 4, color: 'var(--accent)' }}>
              {status?.lastDurationMs ? `${status.lastDurationMs}ms` : '--'}
            </div>
          </div>
          <div
            style={{
              padding: '12px 16px',
              borderRadius: 8,
              background: 'var(--bg-2)',
              border: '1px solid var(--border)',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--text-3)' }}>上次轮询时间</div>
            <div style={{ fontSize: 14, fontWeight: 600, marginTop: 7, color: 'var(--text)' }}>
              {status?.lastCheckTime ? new Date(status.lastCheckTime).toLocaleTimeString() : '刚刚'}
            </div>
          </div>
        </div>

        {/* 正在监控的分支徽章栏 */}
        {status?.watchedBranches && status.watchedBranches.length > 0 && (
          <div
            style={{
              padding: '10px 14px',
              borderRadius: 8,
              background: 'var(--bg-2)',
              border: '1px solid var(--border)',
              flex: 'none',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--text-3)', marginBottom: 6 }}>
              当前监控分支：
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
              {status.watchedBranches.map((w: BranchWatch) => (
                <span
                  key={w.id}
                  style={{
                    background: 'var(--bg-1)',
                    border: '1px solid var(--border-strong)',
                    borderRadius: 6,
                    padding: '3px 8px',
                    fontSize: 12,
                    fontFamily: 'var(--mono)',
                  }}
                  title={w.lastMessage ? `${w.lastMessage} (${w.lastAuthor})` : undefined}
                >
                  <b style={{ color: 'var(--text)' }}>{w.repoName}</b>
                  <span style={{ color: 'var(--text-3)' }}>:</span>
                  <span style={{ color: 'var(--accent-2)' }}>{w.branch}</span>{' '}
                  <span style={{ color: 'var(--text-3)' }}>({w.lastSha ? w.lastSha.slice(0, 7) : 'init'})</span>
                </span>
              ))}
            </div>
          </div>
        )}

        {/* 工具栏：过滤、刷新与操作 */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            flexWrap: 'wrap',
            gap: 10,
            flex: 'none',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ fontSize: 12, color: 'var(--text-3)' }}>时间范围:</span>
            <select
              className="sm"
              value={hours}
              onChange={(e) => setHours(Number(e.target.value))}
              style={{ width: 130 }}
            >
              <option value={8}>最近 8 小时</option>
              <option value={24}>当天 (24小时)</option>
              <option value={2}>最近 2 小时</option>
            </select>

            <span style={{ fontSize: 12, color: 'var(--text-3)', marginLeft: 6 }}>日志级别:</span>
            <select
              className="sm"
              value={levelFilter}
              onChange={(e) => setLevelFilter(e.target.value)}
              style={{ width: 110 }}
            >
              <option value="all">全部级别</option>
              <option value="EVENT">事件 (EVENT)</option>
              <option value="INFO">信息 (INFO)</option>
              <option value="WARN">警告 (WARN)</option>
              <option value="ERROR">错误 (ERROR)</option>
            </select>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <button
              className="ghost sm"
              onClick={copyLogs}
              title="复制当前过滤的日志文本到剪贴板"
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

        {/* 日志内容展示终端区 */}
        <div
          style={{
            flex: 1,
            minHeight: isMaximized ? 320 : 260,
            overflowY: 'auto',
            background: 'var(--bg)',
            border: '1px solid var(--border-strong)',
            borderRadius: 8,
            padding: '14px 16px',
            fontFamily: 'var(--mono)',
            fontSize: 12,
            lineHeight: 1.65,
            boxSizing: 'border-box',
          }}
        >
          {logs.length === 0 ? (
            <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-3)' }}>
              暂无监控日志记录
            </div>
          ) : (
            logs.map((l, idx) => {
              let badgeColor = 'var(--text-2)';
              let badgeBg = 'rgba(255, 255, 255, 0.06)';
              if (l.level === 'EVENT') {
                badgeColor = 'var(--blue)';
                badgeBg = 'rgba(78, 167, 252, 0.12)';
              } else if (l.level === 'WARN') {
                badgeColor = 'var(--yellow)';
                badgeBg = 'rgba(242, 201, 76, 0.12)';
              } else if (l.level === 'ERROR') {
                badgeColor = 'var(--red)';
                badgeBg = 'rgba(235, 87, 87, 0.15)';
              } else if (l.level === 'INFO') {
                badgeColor = 'var(--text-2)';
              }

              return (
                <div key={idx} style={{ display: 'flex', gap: 10, marginBottom: 5, alignItems: 'flex-start' }}>
                  <span style={{ flexShrink: 0, color: 'var(--text-3)' }}>
                    {new Date(l.timestamp).toLocaleTimeString()}
                  </span>
                  <span
                    style={{
                      flexShrink: 0,
                      fontWeight: 600,
                      width: 58,
                      textAlign: 'center',
                      color: badgeColor,
                      background: badgeBg,
                      borderRadius: 4,
                      padding: '0 4px',
                      fontSize: 11,
                    }}
                  >
                    {l.level}
                  </span>
                  <span style={{ flexShrink: 0, width: 72, color: 'var(--text-3)' }}>
                    [{l.source}]
                  </span>
                  <span
                    style={{
                      color: l.level === 'ERROR' ? 'var(--red)' : 'var(--text)',
                      wordBreak: 'break-all',
                      flex: 1,
                    }}
                  >
                    {l.message}
                  </span>
                </div>
              );
            })
          )}
        </div>

        {/* 底部信息栏 */}
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            paddingTop: 8,
            borderTop: '1px solid var(--border)',
            flex: 'none',
          }}
        >
          <span style={{ fontSize: 12, color: 'var(--text-3)' }}>
            共 {logs.length} 条记录（自动保留最近 8 小时 / 当天，单机日志文件防膨胀）
          </span>
          <button className="ghost sm" onClick={onClose} style={{ padding: '4px 14px' }}>
            关闭
          </button>
        </div>
      </div>
    </div>
  );
};
