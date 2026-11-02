import { useEffect, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { PluginSlot } from './PluginFrame';
import { api, qk } from '../api';

const SYSTEM_LINKS: { to: string; icon: string; label: string; match?: string }[] = [
  { to: '/settings/general', icon: '⚙️', label: '全局设置', match: '/settings' },
  { to: '/monitor', icon: '📡', label: '监控中心' },
  { to: '/usage', icon: '📊', label: '用量统计' },
  { to: '/plugins', icon: '🧩', label: '插件管理', match: '/plugins' },
  { to: '/agents', icon: '🤖', label: 'Agent 管理' },
  { to: '/mcp', icon: '🔌', label: 'MCP 接入' },
  { to: '/schedules', icon: '⏱', label: '计划任务' },
  { to: '/jobs', icon: '📋', label: '任务队列' },
];

export const SYSTEM_ROUTES = ['/settings', '/monitor', '/usage', '/plugins', '/agents', '/mcp', '/schedules', '/jobs'];

export function isSystemRoute(pathname: string): boolean {
  return SYSTEM_ROUTES.some((p) => pathname === p || pathname.startsWith(`${p}/`));
}

const PROJECT_LINKS: { to: string; key: string; icon: string; label: string }[] = [
  { to: '', key: '', icon: '📋', label: '看板' },
  { to: '/todos', key: 'todos', icon: '☑️', label: 'TODO 列表' },
  { to: '/test-plans', key: 'test-plans', icon: '🧪', label: '测试计划' },
  { to: '/settings', key: 'settings', icon: '⚙️', label: '项目设置' },
  { to: '/edit', key: 'edit', icon: '✏️', label: '项目信息' },
  { to: '/repos', key: 'repos', icon: '📦', label: '添加仓库' },
  { to: '/knowledge', key: 'knowledge', icon: '📚', label: '知识库' },
  { to: '/readme', key: 'readme', icon: '📖', label: 'README' },
  { to: '/prs', key: 'prs', icon: '🔀', label: 'Pull Requests' },
  { to: '/commits', key: 'commits', icon: '📜', label: '提交记录' },
  { to: '/cherry-pick', key: 'cherry-pick', icon: '🍒', label: 'Cherry-Pick' },
  { to: '/branches', key: 'branches', icon: '🧹', label: '分支清理' },
  { to: '/history', key: 'history', icon: '✅', label: '已完成 / 归档' },
];

const STORAGE_KEY = 'xissue.sidebar.collapsed';

export function Sidebar() {
  const { pathname } = useLocation();
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(STORAGE_KEY) === '1');
  const projectsQ = useQuery({ queryKey: qk.projects, queryFn: api.projects });
  const projectMatch = pathname.match(/^\/projects\/(\d+)(?:\/([^/]+))?/);
  const issueMatch = pathname.match(/^\/issues\/(\d+)/);
  const issueId = issueMatch ? Number(issueMatch[1]) : 0;
  // An issue page still belongs to a project: keep the project menu available.
  const issueQ = useQuery({
    queryKey: qk.issue(issueId),
    queryFn: () => api.issue(issueId),
    enabled: issueId > 0,
  });
  const activeProjectId = projectMatch?.[1] ?? (issueQ.data ? String(issueQ.data.projectId) : undefined);
  const projectView = projectMatch?.[2] ?? '';
  const systemMode = isSystemRoute(pathname);

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, collapsed ? '1' : '0');
  }, [collapsed]);

  return (
    <aside className={`sidebar${collapsed ? ' collapsed' : ''}`}>
      <Link to="/" className="sidebar-brand" title="xIssue">
        <span>🐞</span>
        <span className="label">xIssue</span>
      </Link>
      <div className="sidebar-scroll">
        {systemMode ? (
          <>
            <Link to="/" className="sidebar-item" title="返回项目">
              <span className="icon">🏠</span>
              <span className="label">返回项目</span>
            </Link>
            <div className="sidebar-sec">系统设置</div>
            {SYSTEM_LINKS.map((it) => {
              const isActive = it.match
                ? pathname === it.match || pathname.startsWith(`${it.match}/`)
                : pathname === it.to;
              return (
                <Link key={it.to} to={it.to} className={`sidebar-item${isActive ? ' active' : ''}`} title={it.label}>
                  <span className="icon">{it.icon}</span>
                  <span className="label">{it.label}</span>
                </Link>
              );
            })}
          </>
        ) : (
          <>
            <div className="sidebar-sec">项目</div>
            <Link to="/" className={`sidebar-item${pathname === '/' ? ' active' : ''}`} title="全部项目">
              <span className="icon">🗂️</span>
              <span className="label">全部项目</span>
            </Link>
            {(projectsQ.data ?? [])
              .filter((p) => p.pinned)
              .map((p) => (
                <Link
                  key={p.id}
                  to={`/projects/${p.id}`}
                  className={`sidebar-item${pathname.startsWith(`/projects/${p.id}`) ? ' active' : ''}`}
                  title={`${p.name}（已置顶）`}
                >
                  <span className="icon">📌</span>
                  <span className="label">{p.name}</span>
                </Link>
              ))}

            {activeProjectId && (
              <>
                <div className="sidebar-sec">当前项目</div>
                {issueId > 0 && (
                  <Link to={`/issues/${issueId}`} className="sidebar-item active" title="当前 Issue">
                    <span className="icon">🐞</span>
                    <span className="label">当前 Issue</span>
                  </Link>
                )}
                {PROJECT_LINKS.map((it) => (
                  <Link
                    key={it.key || 'board'}
                    to={`/projects/${activeProjectId}${it.to}`}
                    className={`sidebar-item${projectView === it.key ? ' active' : ''}`}
                    title={it.label}
                  >
                    <span className="icon">{it.icon}</span>
                    <span className="label">{it.label}</span>
                  </Link>
                ))}
                {!collapsed && (() => {
                  const proj = (projectsQ.data ?? []).find((p) => String(p.id) === activeProjectId);
                  return (
                    <div className="sidebar-plugins">
                      <PluginSlot
                        name="project.toolbar"
                        inline
                        context={{ projectId: Number(activeProjectId), projectName: proj?.name, runnerId: proj?.runnerId }}
                      />
                    </div>
                  );
                })()}
              </>
            )}
          </>
        )}
      </div>
      <div className="sidebar-foot">
        <button
          type="button"
          className="sidebar-item"
          style={{ border: 0, background: 'transparent', cursor: 'pointer', textAlign: 'left' }}
          title={collapsed ? '展开侧栏' : '收起侧栏'}
          onClick={() => setCollapsed((v) => !v)}
        >
          <span className="icon">{collapsed ? '»' : '«'}</span>
          <span className="label">收起侧栏</span>
        </button>
      </div>
    </aside>
  );
}
