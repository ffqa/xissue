import React, { useState, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import type { BranchWatch, MonitorConfig } from '../types';
import { toast } from '../toast';
import { copyText } from '../clipboard';

interface MonitorModalProps {
  onClose: () => void;
}

export const MonitorModal: React.FC<MonitorModalProps> = ({ onClose }) => {
  const qc = useQueryClient();
  const [hours, setHours] = useState<number>(8);
  const [levelFilter, setLevelFilter] = useState<string>('all');
  const [copied, setCopied] = useState(false);
  const [isMaximized, setIsMaximized] = useState(false);
  const [showConfig, setShowConfig] = useState(false);

  // 监控运行时状态与日志
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

  // 监控与通知策略配置
  const configQ = useQuery({
    queryKey: qk.monitorConfig,
    queryFn: () => api.monitorConfig(),
  });

  const [cfgEnabled, setCfgEnabled] = useState(true);
  const [cfgActiveHours, setCfgActiveHours] = useState('');
  const [cfgPublicUrl, setCfgPublicUrl] = useState('');
  const [cfgTgToken, setCfgTgToken] = useState('');
  const [cfgTgChatId, setCfgTgChatId] = useState('');
  const [cfgWebhookUrl, setCfgWebhookUrl] = useState('');
  const [cfgSyncIssues, setCfgSyncIssues] = useState(false);
  const [cfgSyncIntervalMin, setCfgSyncIntervalMin] = useState(10);
  const [cfgAutoDevelopIssues, setCfgAutoDevelopIssues] = useState(false);

  useEffect(() => {
    if (configQ.data) {
      setCfgEnabled(configQ.data.enabled ?? true);
      setCfgActiveHours(configQ.data.activeHours || '');
      setCfgPublicUrl(configQ.data.publicUrl || '');
      setCfgTgToken(configQ.data.telegramBotToken || '');
      setCfgTgChatId(configQ.data.telegramChatId || '');
      setCfgWebhookUrl(configQ.data.webhookUrl || '');
      setCfgSyncIssues(configQ.data.syncIssues ?? false);
      setCfgSyncIntervalMin(configQ.data.syncIntervalMin || 10);
      setCfgAutoDevelopIssues(configQ.data.autoDevelopIssues ?? false);
    }
  }, [configQ.data]);

  const toggleMonitorMut = useMutation({
    mutationFn: (enabled: boolean) => api.patchMonitorConfig({ enabled }),
    onSuccess: (updated) => {
      qc.setQueryData(qk.monitorConfig, updated);
      qc.invalidateQueries({ queryKey: qk.monitorStatus });
      toast.success(updated.enabled ? '监控已恢复运行' : '监控已暂停');
    },
    onError: (err: Error) => toast.error(`切换监控状态失败: ${err.message}`),
  });

  const updateConfigMut = useMutation({
    mutationFn: (newCfg: MonitorConfig) => api.updateMonitorConfig(newCfg),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.monitorConfig });
      qc.invalidateQueries({ queryKey: qk.monitorStatus });
      toast.success('监控策略与通知设置已保存');
    },
    onError: (err: Error) => toast.error(`保存失败: ${err.message}`),
  });

  const testNotifyMut = useMutation({
    mutationFn: () => api.testNotify(),
    onSuccess: (res) => toast.success(res.message || '测试通知已派发'),
    onError: (err: Error) => toast.error(`测试通知发送失败: ${err.message}`),
  });

  const triggerMut = useMutation({
    mutationFn: () => api.triggerMonitor(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.monitorStatus });
      qc.invalidateQueries({ queryKey: qk.monitorLogs(hours) });
      toast.success('已触发即时检查');
    },
  });

  const pruneMut = useMutation({
    mutationFn: () => api.pruneMonitorLogs(hours),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.monitorLogs(hours) });
    },
  });

  const syncAllMut = useMutation({
    mutationFn: () => api.syncAllIssues(),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: qk.monitorStatus });
      qc.invalidateQueries({ queryKey: qk.monitorLogs(hours) });
      qc.invalidateQueries({ queryKey: qk.projects });
      qc.invalidateQueries({ queryKey: ['issues'] });
      if (res.errors?.length) {
        toast.error(`${res.message}；部分失败: ${res.errors.join('; ')}`);
      } else {
        toast.success(res.message || 'Issue 同步完成');
      }
    },
    onError: (err: Error) => toast.error(`同步失败: ${err.message}`),
  });

  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener('keydown', handleKey, true);
    return () => window.removeEventListener('keydown', handleKey, true);
  }, [onClose]);

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
    void copyText(text).then((ok) => {
      if (ok) {
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      } else {
        toast.error('复制失败，请手动选择文本');
      }
    });
  };

  const handleTogglePause = () => {
    if (!configQ.data) return;
    const nextEnabled = !cfgEnabled;
    setCfgEnabled(nextEnabled);
    toggleMonitorMut.mutate(nextEnabled);
  };

  const handleSaveConfig = () => {
    updateConfigMut.mutate({
      enabled: cfgEnabled,
      activeHours: cfgActiveHours,
      publicUrl: cfgPublicUrl,
      telegramBotToken: cfgTgToken,
      telegramChatId: cfgTgChatId,
      webhookUrl: cfgWebhookUrl,
      syncIssues: cfgSyncIssues,
      syncIntervalMin: cfgSyncIntervalMin,
      autoDevelopIssues: cfgAutoDevelopIssues,
    });
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
          width: isMaximized ? '100vw' : 'min(980px, 95vw)',
          height: isMaximized ? '100vh' : 'min(880px, 90vh)',
          maxWidth: isMaximized ? '100vw' : '95vw',
          maxHeight: isMaximized ? '100vh' : '90vh',
          borderRadius: isMaximized ? 0 : 12,
          padding: isMaximized ? '16px 24px' : '20px 24px',
          display: 'flex',
          flexDirection: 'column',
          boxSizing: 'border-box',
          gap: 12,
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
            paddingBottom: 12,
            flex: 'none',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ fontSize: 20 }}>📡</span>
            <b style={{ fontSize: 17, color: 'var(--text)' }}>服务监控与通知系统</b>
            <span
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                background: !status?.enabled
                  ? 'rgba(234, 179, 8, 0.15)'
                  : 'rgba(76, 183, 130, 0.15)',
                color: !status?.enabled ? 'var(--yellow, #eab308)' : 'var(--green)',
                border: !status?.enabled
                  ? '1px solid rgba(234, 179, 8, 0.3)'
                  : '1px solid rgba(76, 183, 130, 0.3)',
                borderRadius: 999,
                padding: '2px 10px',
                fontSize: 12,
                fontWeight: 600,
              }}
            >
              <span
                style={{
                  width: 6,
                  height: 6,
                  borderRadius: '50%',
                  background: !status?.enabled ? '#eab308' : 'var(--green)',
                }}
              />
              {!status?.enabled
                ? '已暂停'
                : status?.running === false
                  ? `休眠中 (${status?.activeHours || '全天'})`
                  : `运行中 (${status?.activeHours || '全天'})`}
            </span>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <button
              className="sm"
              onClick={handleTogglePause}
              disabled={!configQ.data || configQ.isLoading || toggleMonitorMut.isPending}
              title={!configQ.data ? '正在加载监控配置…' : cfgEnabled ? '暂停后台监控' : '恢复后台监控'}
              style={{
                borderColor: cfgEnabled ? 'rgba(234, 179, 8, 0.4)' : 'rgba(76, 183, 130, 0.4)',
                color: cfgEnabled ? '#eab308' : 'var(--green)',
              }}
            >
              {cfgEnabled ? '⏸️ 暂停监控' : '▶️ 恢复监控'}
            </button>
            <button
              className={`sm ${showConfig ? 'primary' : 'ghost'}`}
              onClick={() => setShowConfig(!showConfig)}
              title="配置监控活跃时间段与 Telegram/Webhook 通知"
            >
              ⚙️ 策略与通知
            </button>
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

        {/* 策略与通知设置折叠面板 */}
        {showConfig && (
          <div
            style={{
              padding: '14px 16px',
              borderRadius: 8,
              background: 'var(--bg-2)',
              border: '1px solid var(--accent)',
              display: 'flex',
              flexDirection: 'column',
              gap: 12,
              flex: 'none',
            }}
          >
            <div className="row" style={{ justifyContent: 'space-between', alignItems: 'center' }}>
              <b style={{ fontSize: 14, color: 'var(--accent)' }}>⚙️ 监控活跃时段与外部通知平台设置</b>
              <div className="row" style={{ gap: 8 }}>
                <button
                  className="ghost sm"
                  disabled={syncAllMut.isPending}
                  onClick={() => syncAllMut.mutate()}
                  title="立即拉取所有项目的远程 Issue 并导入待办池"
                >
                  {syncAllMut.isPending ? '同步中…' : '🔄 同步全部 Issue'}
                </button>
                <button
                  className="ghost sm"
                  disabled={testNotifyMut.isPending}
                  onClick={() => testNotifyMut.mutate()}
                  title="向已配置的 Telegram 或 Webhook 发送一条测试消息"
                >
                  {testNotifyMut.isPending ? '发送中…' : '🧪 测试通知'}
                </button>
                <button
                  className="primary sm"
                  disabled={updateConfigMut.isPending}
                  onClick={handleSaveConfig}
                >
                  {updateConfigMut.isPending ? '保存中…' : '💾 保存设置'}
                </button>
              </div>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 12 }}>
              <label className="field" style={{ margin: 0 }}>
                监控活跃时段 (Active Hours)
                <input
                  type="text"
                  className="sm mono"
                  placeholder="如 08:00-20:00 (留空为全天)"
                  value={cfgActiveHours}
                  onChange={(e) => setCfgActiveHours(e.target.value)}
                />
                <span className="dim small" style={{ fontSize: 11, marginTop: 2 }}>
                  非工作时间段自动休眠，停止后台轮询以节省资源与电量。
                </span>
              </label>

              <label className="field" style={{ margin: 0 }}>
                <span className="row" style={{ gap: 8, alignItems: 'center' }}>
                  <input
                    type="checkbox"
                    checked={cfgSyncIssues}
                    onChange={(e) => setCfgSyncIssues(e.target.checked)}
                  />
                  自动同步远程 Issue
                </span>
                <span className="dim small" style={{ fontSize: 11, marginTop: 2 }}>
                  按间隔拉取所有项目的开放 Issue；新 Issue 若命中项目的自动预开发规则，会按既有配额启动 Run。
                </span>
              </label>

              <label className="field" style={{ margin: 0 }}>
                同步间隔（分钟）
                <input
                  type="number"
                  className="sm mono"
                  min={1}
                  max={1440}
                  value={cfgSyncIntervalMin}
                  onChange={(e) => setCfgSyncIntervalMin(Math.max(1, Number(e.target.value) || 10))}
                  disabled={!cfgSyncIssues}
                />
                <span className="dim small" style={{ fontSize: 11, marginTop: 2 }}>
                  默认 10 分钟；仅监控总开关开启时生效。也可随时点上方「同步全部 Issue」立即执行。
                </span>
              </label>

              <label className="field" style={{ margin: 0 }}>
                <span className="row" style={{ gap: 8, alignItems: 'center' }}>
                  <input
                    type="checkbox"
                    checked={cfgAutoDevelopIssues}
                    onChange={(e) => setCfgAutoDevelopIssues(e.target.checked)}
                  />
                  自动开发判定的 Bug / 需求
                </span>
                <span className="dim small" style={{ fontSize: 11, marginTop: 2 }}>
                  同步后先判定类型：Bug/需求自动打标并启动 Agent，完成后自动发 PR；建议只打标不开发。
                  仍需项目配置了可用 Agent，并受项目每日配额限制。
                </span>
              </label>

              <label className="field" style={{ margin: 0 }}>
                外部访问地址 (通知链接)
                <input
                  type="text"
                  className="sm mono"
                  placeholder="如 http://100.64.0.2:7789"
                  value={cfgPublicUrl}
                  onChange={(e) => setCfgPublicUrl(e.target.value)}
                />
                <span className="dim small" style={{ fontSize: 11, marginTop: 2 }}>
                  通知里的链接需要它才能点击；留空时按监听地址自动推导（0.0.0.0 时取本机非回环 IP）。
                </span>
              </label>

              <label className="field" style={{ margin: 0 }}>
                Telegram Bot Token
                <input
                  type="password"
                  className="sm mono"
                  placeholder="如 123456789:ABCdefGhI..."
                  value={cfgTgToken}
                  onChange={(e) => setCfgTgToken(e.target.value)}
                />
                <span className="dim small" style={{ fontSize: 11, marginTop: 2 }}>
                  创建 Bot 获得的 Token；保存后以 ****** 回显，保持 ****** 不变即沿用已存值。
                </span>
              </label>

              <label className="field" style={{ margin: 0 }}>
                Telegram Chat ID
                <input
                  type="text"
                  className="sm mono"
                  placeholder="如 -1001234567 或个人 ChatID"
                  value={cfgTgChatId}
                  onChange={(e) => setCfgTgChatId(e.target.value)}
                />
                <span className="dim small" style={{ fontSize: 11, marginTop: 2 }}>
                  接收通知的目标群组或个人 Chat ID。
                </span>
              </label>

              <label className="field" style={{ margin: 0 }}>
                通用 Webhook URL (飞书 / 企微 / 钉钉 / Slack)
                <input
                  type="text"
                  className="sm mono"
                  placeholder="https://open.feishu.cn/... 或 https://qyapi.weixin.qq.com/..."
                  value={cfgWebhookUrl}
                  onChange={(e) => setCfgWebhookUrl(e.target.value)}
                />
                <span className="dim small" style={{ fontSize: 11, marginTop: 2 }}>
                  POST JSON 格式载荷；保存后以 ****** 回显，保持 ****** 不变即沿用已存值。
                </span>
              </label>
            </div>
          </div>
        )}

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
              padding: '10px 14px',
              borderRadius: 8,
              background: 'var(--bg-2)',
              border: '1px solid var(--border)',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--text-3)' }}>监控分支数</div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 2, color: 'var(--text)' }}>
              {status?.watchesCount ?? 0}
            </div>
          </div>
          <div
            style={{
              padding: '10px 14px',
              borderRadius: 8,
              background: 'var(--bg-2)',
              border: '1px solid var(--border)',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--text-3)' }}>跟踪开放 PR</div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 2, color: 'var(--text)' }}>
              {status?.openPRsCount ?? 0}
            </div>
          </div>
          <div
            style={{
              padding: '10px 14px',
              borderRadius: 8,
              background: 'var(--bg-2)',
              border: '1px solid var(--border)',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--text-3)' }}>上次检查耗时</div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 2, color: 'var(--accent)' }}>
              {status?.lastDurationMs ? `${status.lastDurationMs}ms` : '--'}
            </div>
          </div>
          <div
            style={{
              padding: '10px 14px',
              borderRadius: 8,
              background: 'var(--bg-2)',
              border: '1px solid var(--border)',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--text-3)' }}>外部通知渠道</div>
            <div style={{ fontSize: 13, fontWeight: 600, marginTop: 5, color: 'var(--text)' }}>
              {status?.telegramConfigured || status?.webhookConfigured ? (
                <span style={{ color: 'var(--green)' }}>
                  ✓ {status.telegramConfigured ? 'TG ' : ''}
                  {status.webhookConfigured ? 'Webhook' : ''}
                </span>
              ) : (
                <span className="dim">未配置</span>
              )}
            </div>
          </div>
        </div>

        {status?.issueSyncEnabled && (
          <div className="row small muted" style={{ flexWrap: 'wrap', gap: 10, flex: 'none' }}>
            <span>🔄 Issue 自动同步：每 {status.issueSyncIntervalMin || 10} 分钟</span>
            <span>
              上次：{status.lastIssueSyncTime ? new Date(status.lastIssueSyncTime).toLocaleString() : '尚未执行'}
            </span>
            {status.lastIssueSyncTime && <span>新增 {status.lastIssueSyncCount} 条</span>}
            {status.lastIssueSyncError && (
              <span style={{ color: 'var(--yellow, #eab308)' }}>部分失败: {status.lastIssueSyncError}</span>
            )}
          </div>
        )}

        {/* 正在监控的分支徽章栏 */}
        {status?.watchedBranches && status.watchedBranches.length > 0 && (
          <div
            style={{
              padding: '8px 12px',
              borderRadius: 8,
              background: 'var(--bg-2)',
              border: '1px solid var(--border)',
              flex: 'none',
            }}
          >
            <div style={{ fontSize: 12, color: 'var(--text-3)', marginBottom: 4 }}>
              当前监控分支：
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {status.watchedBranches.map((w: BranchWatch) => (
                <span
                  key={w.id}
                  style={{
                    background: 'var(--bg-1)',
                    border: '1px solid var(--border-strong)',
                    borderRadius: 6,
                    padding: '2px 8px',
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
              style={{ width: 110 }}
            >
              <option value={1}>最近 1 小时</option>
              <option value={4}>最近 4 小时</option>
              <option value={8}>最近 8 小时</option>
              <option value={24}>最近 24 小时</option>
            </select>

            <span style={{ fontSize: 12, color: 'var(--text-3)', marginLeft: 6 }}>日志级别:</span>
            <div style={{ display: 'inline-flex', gap: 4 }}>
              {['all', 'EVENT', 'WARN', 'ERROR', 'INFO'].map((lvl) => (
                <button
                  key={lvl}
                  type="button"
                  className={`sm ${levelFilter === lvl ? 'primary' : 'ghost'}`}
                  style={{ padding: '2px 8px', fontSize: 11 }}
                  onClick={() => setLevelFilter(lvl)}
                >
                  {lvl === 'all' ? '全部' : lvl}
                </button>
              ))}
            </div>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <button
              className="ghost sm"
              onClick={copyLogs}
              title="复制当前过滤后的日志文本"
              style={{ padding: '4px 10px' }}
            >
              {copied ? '✓ 已复制' : '📋 复制日志'}
            </button>
            <button
              className="ghost sm"
              onClick={() => pruneMut.mutate()}
              disabled={pruneMut.isPending}
              title="清理并保留设定小时数以内的日志"
              style={{ padding: '4px 10px' }}
            >
              🧹 清理过期
            </button>
            <button
              className="primary sm"
              onClick={() => triggerMut.mutate()}
              disabled={triggerMut.isPending}
              style={{ padding: '4px 12px' }}
            >
              {triggerMut.isPending ? '正在检查…' : '🔄 立即检查'}
            </button>
          </div>
        </div>

        {/* 日志控制台主区域 */}
        <div
          style={{
            flex: 1,
            overflowY: 'auto',
            background: 'var(--bg-0)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            padding: '12px 14px',
            fontFamily: 'var(--mono)',
            fontSize: 12,
            lineHeight: 1.5,
          }}
        >
          {logs.length === 0 ? (
            <div style={{ color: 'var(--text-3)', textAlign: 'center', padding: '40px 0' }}>
              暂无匹配的监控日志记录
            </div>
          ) : (
            logs.map((l, idx) => {
              let badgeBg = 'rgba(255, 255, 255, 0.08)';
              let badgeColor = 'var(--text-2)';
              if (l.level === 'EVENT') {
                badgeBg = 'rgba(76, 183, 130, 0.2)';
                badgeColor = 'var(--green)';
              } else if (l.level === 'WARN') {
                badgeBg = 'rgba(234, 179, 8, 0.2)';
                badgeColor = '#eab308';
              } else if (l.level === 'ERROR') {
                badgeBg = 'rgba(239, 68, 68, 0.2)';
                badgeColor = 'var(--red)';
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
