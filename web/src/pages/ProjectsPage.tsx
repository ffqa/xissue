import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Crumbs, Modal, fmtTime } from '../components/common';
import { LocalDirPickerModal } from '../components/LocalDirPicker';
import { toast } from '../toast';
import type { InspectedRepo, InspectResult, Provider } from '../types';

const PROVIDERS: Provider[] = ['github', 'gitlab', 'gitea', 'plain'];

export default function ProjectsPage() {
  const qc = useQueryClient();
  const nav = useNavigate();
  const projects = useQuery({ queryKey: qk.projects, queryFn: api.projects });

  const [open, setOpen] = useState(false);
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');
  const [pickedRepos, setPickedRepos] = useState<InspectedRepo[]>([]);
  const [pickerOpen, setPickerOpen] = useState(false);

  const handlePickedDir = (res: InspectResult) => {
    setPickerOpen(false);
    if (!name.trim()) {
      setName(res.suggestedName);
    }
    if (!desc.trim()) {
      setDesc(res.path);
    }
    if (res.repos.length === 0) {
      toast.info('所选目录及其直接子目录未检测到 .git 仓库，你仍可创建项目后手动添加');
      setOpen(true);
      return;
    }
    setPickedRepos((prev) => {
      const existing = new Set(prev.map((r) => r.name));
      const merged = [...prev];
      for (const r of res.repos) {
        if (!existing.has(r.name)) {
          merged.push(r);
          existing.add(r.name);
        }
      }
      return merged;
    });
    setOpen(true);
  };

  const create = useMutation({
    mutationFn: async () => {
      const p = await api.createProject({ name: name.trim(), description: desc.trim() });
      for (const r of pickedRepos) {
        await api.addRepo(p.id, {
          name: r.name.trim(),
          provider: r.provider,
          remoteUrl: r.remoteUrl.trim(),
          ownerRepo: r.provider === 'plain' ? '' : r.ownerRepo.trim(),
          defaultBranch: r.defaultBranch.trim() || 'main',
          testCommand: r.testCommand.trim(),
        });
      }
      return p;
    },
    onSuccess: (p) => {
      qc.invalidateQueries({ queryKey: qk.projects });
      setOpen(false);
      setName('');
      setDesc('');
      setPickedRepos([]);
      toast.success(pickedRepos.length > 0 ? `项目已创建并导入 ${pickedRepos.length} 个仓库` : '项目已创建');
      nav(`/projects/${p.id}`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const updatePicked = <K extends keyof InspectedRepo>(idx: number, k: K, v: InspectedRepo[K]) => {
    setPickedRepos((list) => list.map((it, i) => (i === idx ? { ...it, [k]: v } : it)));
  };

  return (
    <div className="page">
      <Crumbs>
        <span>/</span>
        <span>项目</span>
      </Crumbs>
      <div className="row mb">
        <h1 className="grow">项目</h1>
        <button
          onClick={() => {
            setName('');
            setDesc('');
            setPickedRepos([]);
            setPickerOpen(true);
          }}
        >
          📂 从本地目录导入
        </button>
        <button
          className="primary"
          onClick={() => {
            setName('');
            setDesc('');
            setPickedRepos([]);
            setOpen(true);
          }}
        >
          + 新建项目
        </button>
      </div>
      {projects.isLoading && <div className="empty">加载中…</div>}
      {projects.data && projects.data.length === 0 && (
        <div className="panel empty">
          还没有项目，可以点击右上角“从本地目录导入”直接选择本地单仓或多仓目录并自动解析 Git 信息。
        </div>
      )}
      <div className="project-grid">
        {projects.data?.map((p) => (
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

      {open && (
        <Modal
          title="新建 / 导入项目"
          onClose={() => setOpen(false)}
          footer={
            <>
              <button onClick={() => setPickerOpen(true)}>📂 选择本地仓库…</button>
              <span className="grow" />
              <button onClick={() => setOpen(false)}>取消</button>
              <button className="primary" disabled={!name.trim() || create.isPending} onClick={() => create.mutate()}>
                {create.isPending ? '创建中…' : pickedRepos.length > 0 ? `创建并导入 (${pickedRepos.length})` : '创建'}
              </button>
            </>
          }
        >
          <label className="field">
            项目名称
            <input autoFocus value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          <label className="field">
            描述
            <textarea rows={2} value={desc} onChange={(e) => setDesc(e.target.value)} />
          </label>

          {pickedRepos.length > 0 && (
            <div className="col">
              <div className="row small muted">
                <b className="grow">已从本地解析到的 Git 仓库 ({pickedRepos.length})</b>
              </div>
              {pickedRepos.map((r, i) => (
                <div key={r.path + i} className="panel" style={{ padding: 10 }}>
                  <div className="row mb" style={{ marginBottom: 6 }}>
                    <span className="mono small dim grow ellipsis" title={r.path}>
                      {r.path}
                    </span>
                    <button
                      className="ghost sm"
                      onClick={() => setPickedRepos((list) => list.filter((_, idx) => idx !== i))}
                    >
                      移除 ✕
                    </button>
                  </div>
                  <div className="repo-form">
                    <label className="field">
                      目录名
                      <input value={r.name} onChange={(e) => updatePicked(i, 'name', e.target.value)} />
                    </label>
                    <label className="field">
                      平台
                      <select
                        value={r.provider}
                        onChange={(e) => updatePicked(i, 'provider', e.target.value as Provider)}
                      >
                        {PROVIDERS.map((x) => (
                          <option key={x} value={x}>
                            {x}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="field">
                      默认分支
                      <input
                        value={r.defaultBranch}
                        onChange={(e) => updatePicked(i, 'defaultBranch', e.target.value)}
                      />
                    </label>
                  </div>
                  <div className="repo-form" style={{ marginTop: 6 }}>
                    <label className="field">
                      Remote URL / 本地路径
                      <input value={r.remoteUrl} onChange={(e) => updatePicked(i, 'remoteUrl', e.target.value)} />
                    </label>
                    {r.provider !== 'plain' && (
                      <label className="field">
                        owner/repo
                      <input value={r.ownerRepo} onChange={(e) => updatePicked(i, 'ownerRepo', e.target.value)} />
                      </label>
                    )}
                    <label className="field">
                      测试命令
                      <input value={r.testCommand} onChange={(e) => updatePicked(i, 'testCommand', e.target.value)} />
                    </label>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Modal>
      )}

      {pickerOpen && <LocalDirPickerModal onClose={() => setPickerOpen(false)} onPick={handlePickedDir} />}
    </div>
  );
}
