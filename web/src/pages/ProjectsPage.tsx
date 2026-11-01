import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Crumbs, fmtTime } from '../components/common';
import { toast } from '../toast';
import type { Project } from '../types';

export default function ProjectsPage() {
  const nav = useNavigate();
  const qc = useQueryClient();
  const projects = useQuery({ queryKey: qk.projects, queryFn: api.projects });
  const togglePin = useMutation({
    mutationFn: (p: Project) => api.updateProject(p.id, { pinned: !p.pinned }),
    onSuccess: (p) => {
      qc.invalidateQueries({ queryKey: qk.projects });
      toast.success(p.pinned ? `已置顶 ${p.name}，侧栏可直接进入` : `已取消置顶 ${p.name}`);
    },
    onError: (e) => toast.error((e as Error).message),
  });
  const [filter, setFilter] = useState('');
  const needle = filter.trim().toLowerCase();
  const filteredProjects = (projects.data ?? []).filter((p) => {
    if (!needle) return true;
    const repos = (p.repos ?? []).map((r) => r.name).join(' ');
    return `${p.name} ${p.description ?? ''} ${repos}`.toLowerCase().includes(needle);
  });

  return (
    <div className="page">
      <Crumbs>
        <span>/</span>
        <span>项目</span>
      </Crumbs>
      <div className="row mb">
        <h1 className="grow">项目</h1>
        <button onClick={() => nav('/new-project')}>📂 从本地目录导入</button>
        <button className="primary" onClick={() => nav('/new-project')}>
          + 新建项目
        </button>
      </div>
      {projects.isLoading && <div className="empty">加载中…</div>}
      {projects.data && projects.data.length === 0 && (
        <div className="panel empty">
          还没有项目，可以点击右上角“从本地目录导入”直接选择本地单仓或多仓目录并自动解析 Git 信息。
        </div>
      )}
      <div className="row mb" style={{ gap: 8 }}>
        <input
          className="sm"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="按项目名 / 描述 / 仓库筛选…"
          aria-label="按项目筛选"
          style={{ maxWidth: 360 }}
        />
        {filter && (
          <button className="ghost sm" onClick={() => setFilter('')} title="清除筛选">
            ✕ 清除
          </button>
        )}
        <span className="small dim">
          {needle ? `${filteredProjects.length} / ${projects.data?.length ?? 0} 个项目` : `共 ${projects.data?.length ?? 0} 个项目`}
        </span>
      </div>
      {projects.data && projects.data.length > 0 && filteredProjects.length === 0 && (
        <div className="panel empty">没有匹配的项目。清空筛选条件可查看全部。</div>
      )}
      <div className="project-grid">
        {filteredProjects.map((p) => (
          <div
            key={p.id}
            className="panel project-card"
            role="link"
            tabIndex={0}
            style={{ cursor: 'pointer' }}
            onClick={() => nav(`/projects/${p.id}`)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                nav(`/projects/${p.id}`);
              }
            }}
          >
            <div className="row" style={{ alignItems: 'center' }}>
              <h2 className="grow ellipsis">{p.name}</h2>
              <span className="badge mono" title="项目唯一标识">{`@${p.slug}`}</span>
              {p.version && <span className="badge" title="主分支最新版本 tag">{p.version}</span>}
              <button
                className="ghost sm"
                title={p.pinned ? '取消置顶（侧栏不再显示）' : '置顶到侧栏'}
                disabled={togglePin.isPending}
                onClick={(e) => {
                  e.stopPropagation();
                  togglePin.mutate(p);
                }}
              >
                {p.pinned ? '📌' : '📍'}
              </button>
              <span className="dim small" style={{ marginLeft: 6 }}>{fmtTime(p.updatedAt)}</span>
            </div>
            <div className="muted small" style={{ minHeight: 18 }}>
              {p.description || <span className="dim">无描述</span>}
            </div>
            <div className="repo-list">
              {(p.repos ?? []).map((r) => (
                <span key={r.id} className="badge">
                  {r.name}
                </span>
              ))}
              {(p.repos ?? []).length === 0 && <span className="dim small">未添加仓库</span>}
            </div>
          </div>
        ))}
      </div>

    </div>
  );
}
