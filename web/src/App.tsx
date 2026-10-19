import { useEffect, useRef, useState } from 'react';
import { Link, Route, Routes, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from './api';
import { useRealtime, useWsState } from './realtime';
import { Toaster, toast } from './toast';
import { useTheme } from './theme';
import { AgentManagerModal } from './components/AgentManagerModal';
import { fmtTime } from './components/common';
import ProjectsPage from './pages/ProjectsPage';
import ProjectPage from './pages/ProjectPage';
import IssuePage from './pages/IssuePage';

function WsIndicator() {
  const s = useWsState();
  const label = s === 'open' ? '实时已连接' : s === 'connecting' ? '连接中…' : '已断开，重连中';
  return (
    <span className={`ws-ind ${s}`} title="WebSocket">
      <span className="dot" /> {label}
    </span>
  );
}

function NotificationBell() {
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

  return (
    <div ref={wrapRef} style={{ position: 'relative' }}>
      <button
        type="button"
        className={`ghost sm ${unreadCount > 0 ? 'primary' : ''}`}
        title="PR 状态变更、分支推送监控与 Run 执行通知"
        onClick={() => setOpen((v) => !v)}
        style={{ position: 'relative', display: 'inline-flex', alignItems: 'center', gap: 5 }}
      >
        🔔 通知
        {unreadCount > 0 && (
          <span
            style={{
              background: 'var(--danger)',
              color: '#fff',
              borderRadius: 999,
              padding: '0 6px',
              fontSize: 11,
              fontWeight: 700,
              lineHeight: '16px',
            }}
          >
            {unreadCount}
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

export default function App() {
  const qc = useQueryClient();
  useRealtime(qc);
  const [theme, toggleTheme] = useTheme();
  const [agentMgrOpen, setAgentMgrOpen] = useState(false);
  const health = useQuery({ queryKey: qk.health, queryFn: api.health, staleTime: Infinity });
  const restart = useMutation({
    mutationFn: api.restartServer,
    onSuccess: () => {
      toast.info('服务正在重启，WebSocket 将自动重连…');
      setTimeout(() => qc.invalidateQueries(), 1200);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  return (
    <div className="app">
      <header className="topbar">
        <Link to="/" className="brand">
          xIssue
        </Link>
        <div id="crumbs" className="crumbs grow" />
        <WsIndicator />
        <NotificationBell />
        <button
          className="ghost sm"
          title="切换亮色 / 暗色主题"
          onClick={toggleTheme}
        >
          {theme === 'dark' ? '☀ 亮色' : '☾ 暗色'}
        </button>
        <button
          className="ghost sm"
          title="管理 Agent、一键安装 MCP 到所有 IDE/Agent、绑定默认 Provider/模型"
          onClick={() => setAgentMgrOpen(true)}
        >
          ⚙ Agent & MCP
        </button>
        <button
          className="ghost sm"
          title="重启 xIssue 服务（编译新版本后点击即可生效）"
          disabled={restart.isPending}
          onClick={() => restart.mutate()}
        >
          ↻ 重启服务
        </button>
        {health.data && <span className="dim small">v{health.data.version}</span>}
      </header>
      <main className="main">
        <Routes>
          <Route path="/" element={<ProjectsPage />} />
          <Route path="/projects/:id" element={<ProjectPage />} />
          <Route path="/issues/:id" element={<IssuePage />} />
          <Route path="*" element={<div className="page empty">页面不存在</div>} />
        </Routes>
      </main>
      {agentMgrOpen && <AgentManagerModal onClose={() => setAgentMgrOpen(false)} />}
      <Toaster />
    </div>
  );
}

