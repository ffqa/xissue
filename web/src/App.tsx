import { useEffect, useRef, useState } from 'react';
import { Navigate, Route, Routes, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from './api';
import { useRealtime } from './realtime';
import { Toaster } from './toast';
import { useTheme } from './theme';
import { AgentManagerModal } from './components/AgentManagerModal';
import { MonitorModal } from './components/MonitorModal';
import { UsageModal } from './components/UsageModal';
import { PluginManagerModal } from './components/PluginManagerModal';
import { StatusBar } from './components/StatusBar';
import { Sidebar } from './components/Sidebar';
import { fmtTime } from './components/common';
import { SettingsModal } from './components/SettingsModal';
import { MCPModal } from './components/MCPModal';
import { SchedulesModal } from './components/SchedulesModal';
import { JobsModal } from './components/JobsModal';
import { normalizeNotificationRoutes, writeBrowserRoutes } from './notificationSettings';
import ProjectsPage from './pages/ProjectsPage';
import NewProjectPage from './pages/NewProjectPage';
import ProjectPage from './pages/ProjectPage';
import IssuePage from './pages/IssuePage';
import PluginPage from './pages/PluginPage';


function NotificationBell({ onOpenSettings }: { onOpenSettings: () => void }) {
  const qc = useQueryClient();
  const nav = useNavigate();
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);

  const listQ = useQuery({
    queryKey: qk.notifications,
    queryFn: () => api.notifications(40),
    refetchInterval: 60000,
  });
  const list = listQ.data ?? [];
  const unreadCount = list.filter((n) => !n.read).length;

  useEffect(() => {
    if (!open) return;
    const onDocClick = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', onDocClick);
    return () => document.removeEventListener('mousedown', onDocClick);
  }, [open]);

  const markReadMut = useMutation({
    mutationFn: (id: number) => api.markNotificationRead(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.notifications }),
  });

  const markAllMut = useMutation({
    mutationFn: () => api.markAllNotificationsRead(),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.notifications }),
  });

  const onToggle = () => {
    const next = !open;
    setOpen(next);
    if (next && unreadCount > 0) {
      markAllMut.mutate();
    }
  };

  return (
    <div ref={wrapRef} style={{ position: 'relative' }}>
      <button
        type="button"
        className="ghost sm"
        title={unreadCount > 0 ? `有 ${unreadCount} 条新通知` : '消息通知'}
        onClick={onToggle}
        style={{
          position: 'relative',
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          padding: '4px 8px',
          fontSize: 14,
        }}
      >
        🔔
        {unreadCount > 0 && (
          <span
            style={{
              position: 'absolute',
              top: -3,
              right: -3,
              background: '#ef4444',
              color: '#ffffff',
              borderRadius: 999,
              minWidth: 16,
              height: 16,
              padding: '0 4px',
              fontSize: 10,
              fontWeight: 700,
              lineHeight: '16px',
              textAlign: 'center',
              boxShadow: '0 1px 3px rgba(0,0,0,0.35)',
              border: '1.5px solid var(--bg, #18181b)',
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            {unreadCount > 99 ? '99+' : unreadCount}
          </span>
        )}
      </button>

      {open && (
        <div
          className="panel"
          style={{
            position: 'absolute',
            right: 0,
            top: 'calc(100% + 6px)',
            width: 400,
            maxHeight: 480,
            display: 'flex',
            flexDirection: 'column',
            zIndex: 90,
            boxShadow: '0 12px 36px rgba(0,0,0,.45)',
          }}
        >
          <div className="panel-head">
            <b>🔔 消息通知 ({unreadCount} 未读)</b>
            <span className="grow" />
            <button
              type="button"
              className="sm ghost"
              title="打开通知渠道与事件路由设置"
              onClick={() => {
                setOpen(false);
                onOpenSettings();
              }}
            >
              ⚙️ 通知设置
            </button>
            {unreadCount > 0 && (
              <button
                type="button"
                className="sm ghost"
                disabled={markAllMut.isPending}
                onClick={() => markAllMut.mutate()}
              >
                全部已读
              </button>
            )}
          </div>
          <div style={{ overflowY: 'auto', flex: 1 }}>
            {list.length === 0 ? (
              <div className="empty" style={{ padding: 28 }}>
                暂无通知（PR 合并/关闭、监控分支有新推送、Run 完成时会在此提醒）
              </div>
            ) : (
              list.map((n) => (
                <div
                  key={n.id}
                  onClick={() => {
                    if (!n.read) markReadMut.mutate(n.id);
                    setOpen(false);
                    if (n.link) nav(n.link);
                  }}
                  style={{
                    padding: '10px 12px',
                    borderBottom: '1px solid var(--border)',
                    cursor: n.link ? 'pointer' : 'default',
                    background: n.read ? 'transparent' : 'var(--accent-bg)',
                  }}
                >
                  <div className="row" style={{ gap: 6 }}>
                    {!n.read && (
                      <span
                        style={{
                          width: 7,
                          height: 7,
                          borderRadius: '50%',
                          background: 'var(--accent)',
                          flexShrink: 0,
                        }}
                      />
                    )}
                    <b className="small grow" style={{ wordBreak: 'break-word' }}>
                      {n.title}
                    </b>
                    <span className="small dim">{fmtTime(n.createdAt)}</span>
                  </div>
                  {n.body && (
                    <div
                      className="small muted mono"
                      style={{
                        marginTop: 4,
                        whiteSpace: 'pre-wrap',
                        wordBreak: 'break-all',
                        maxHeight: 90,
                        overflow: 'hidden',
                      }}
                    >
                      {n.body}
                    </div>
                  )}
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function SettingsRoute() {
  const { tab } = useParams();
  const nav = useNavigate();
  const tabs = ['general', 'channels', 'routes', 'agents', 'automation'] as const;
  const initial = (tabs as readonly string[]).includes(tab ?? '') ? (tab as (typeof tabs)[number]) : 'general';
  return (
    <SettingsModal
      initialTab={initial}
      onOpenAgents={() => nav('/agents')}
      onOpenMCP={() => nav('/mcp')}
    />
  );
}

export default function App() {
  const qc = useQueryClient();
  useRealtime(qc);
  const nav = useNavigate();
  const [theme, toggleTheme] = useTheme();
  const notificationConfigQ = useQuery({
    queryKey: qk.monitorConfig,
    queryFn: api.monitorConfig,
    staleTime: 60_000,
  });
  useEffect(() => {
    if (notificationConfigQ.data) {
      writeBrowserRoutes(normalizeNotificationRoutes(notificationConfigQ.data.notificationRoutes));
    }
  }, [notificationConfigQ.data]);

  return (
    <div className="app">
      <Sidebar />
      <div className="app-main">
        <header className="topbar">
          <div id="crumbs" className="crumbs grow" />
          <button
            className="ghost sm"
            style={{ fontSize: 16, padding: '4px 8px' }}
            title="系统设置：全局设置、监控、用量、插件、Agent、MCP、计划任务、任务队列"
            onClick={() => nav('/settings/general')}
          >
            ⚙️
          </button>
          <NotificationBell onOpenSettings={() => nav('/settings/routes')} />
          <button
            className="ghost sm"
            title={theme === 'dark' ? '切换为亮色模式' : '切换为暗色模式'}
            onClick={toggleTheme}
            style={{ fontSize: 14, padding: '4px 8px' }}
          >
            {theme === 'dark' ? '☀️' : '🌙'}
          </button>
        </header>
        <main className="main">
          <Routes>
            <Route path="/" element={<ProjectsPage />} />
            <Route path="/new-project" element={<NewProjectPage />} />
            <Route path="/projects/:id" element={<ProjectPage />} />
            <Route path="/projects/:id/*" element={<ProjectPage />} />
            <Route path="/issues/:id" element={<IssuePage />} />
            <Route path="/issues/:id/*" element={<IssuePage />} />
            <Route path="/plugins/:id" element={<PluginPage />} />
            <Route path="/settings" element={<Navigate to="/settings/general" replace />} />
            <Route path="/settings/:tab" element={<SettingsRoute />} />
            <Route
              path="/monitor"
              element={<MonitorModal onOpenSettings={() => nav('/settings/channels')} />}
            />
            <Route path="/usage" element={<UsageModal />} />
            <Route path="/plugins" element={<PluginManagerModal />} />
            <Route path="/agents" element={<AgentManagerModal />} />
            <Route path="/mcp" element={<MCPModal />} />
            <Route
              path="/schedules"
              element={
                <SchedulesModal
                  onOpenMonitor={() => nav('/monitor')}
                  onOpenSettings={() => nav('/settings/channels')}
                />
              }
            />
            <Route path="/jobs" element={<JobsModal />} />
            <Route path="*" element={<div className="page empty">页面不存在</div>} />
          </Routes>
        </main>
        <StatusBar onOpenSchedules={() => nav('/schedules')} onOpenJobs={() => nav('/jobs')} />
      </div>
      <Toaster />
    </div>
  );
}
