import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Crumbs, ISSUE_STATUS_LABEL, Modal } from '../components/common';
import { LocalDirPickerModal } from '../components/LocalDirPicker';
import { toast } from '../toast';
import { ISSUE_STATUSES, type InspectResult, type Issue, type IssueStatus, type Provider, type RepoInput } from '../types';

const PROVIDERS: Provider[] = ['github', 'gitlab', 'gitea', 'plain'];
const emptyRepo: RepoInput = {
  name: '',
  provider: 'github',
  remoteUrl: '',
  ownerRepo: '',
  defaultBranch: 'main',
  testCommand: '',
};

export default function ProjectPage() {
  const id = Number(useParams().id);
  const qc = useQueryClient();
  const project = useQuery({ queryKey: qk.project(id), queryFn: () => api.project(id) });
  const issues = useQuery({ queryKey: qk.issues(id), queryFn: () => api.issues(id) });

  const [repoOpen, setRepoOpen] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [repo, setRepo] = useState<RepoInput>(emptyRepo);
  const [issueOpen, setIssueOpen] = useState(false);
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');

  const refreshProject = () => {
    qc.invalidateQueries({ queryKey: qk.project(id) });
    qc.invalidateQueries({ queryKey: qk.projects });
  };

  const addRepo = useMutation({
    mutationFn: () => api.addRepo(id, { ...repo, ownerRepo: repo.provider === 'plain' ? '' : repo.ownerRepo }),
    onSuccess: () => {
      refreshProject();
      setRepo(emptyRepo);
      setRepoOpen(false);
      toast.success('仓库已添加');
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const handlePickedLocal = async (res: InspectResult) => {
    setPickerOpen(false);
    if (res.repos.length === 0) {
      toast.error('所选目录下未找到 Git 仓库');
      return;
    }
    if (res.repos.length === 1) {
      const r = res.repos[0];
      setRepo({
        name: r.name,
        provider: r.provider,
        remoteUrl: r.remoteUrl,
        ownerRepo: r.ownerRepo,
        defaultBranch: r.defaultBranch || 'main',
        testCommand: r.testCommand,
      });
      setRepoOpen(true);
      toast.info(`已自动解析 ${r.name} 的 Git 信息，请确认后点击添加`);
      return;
    }
    // 一次解析出多个子仓库时，直接批量导入
    let added = 0;
    for (const r of res.repos) {
      try {
        await api.addRepo(id, {
          name: r.name,
          provider: r.provider,
          remoteUrl: r.remoteUrl,
          ownerRepo: r.provider === 'plain' ? '' : r.ownerRepo,
          defaultBranch: r.defaultBranch || 'main',
          testCommand: r.testCommand,
        });
        added++;
      } catch (e) {
        toast.error(`${r.name}: ${(e as Error).message}`);
      }
    }
    if (added > 0) {
      refreshProject();
      toast.success(`已从本地目录导入 ${added} 个仓库`);
    }
  };
  const delRepo = useMutation({
    mutationFn: (repoId: number) => api.deleteRepo(id, repoId),
    onSuccess: refreshProject,
  });
  const sync = useMutation({
    mutationFn: () => api.syncProject(id),
    onSuccess: (r) => {
      toast.success(`已导入 ${r.imported} 个 Issue`);
      qc.invalidateQueries({ queryKey: qk.issues(id) });
    },
  });
  const createIssue = useMutation({
    mutationFn: () => api.createIssue(id, { title: title.trim(), body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      setTitle('');
      setBody('');
      setIssueOpen(false);
    },
  });
  const setStatus = useMutation({
    mutationFn: ({ issueId, status }: { issueId: number; status: IssueStatus }) =>
      api.updateIssue(issueId, { status }),
    onSuccess: (issue) => {
      qc.setQueryData<Issue[]>(qk.issues(id), (old) => old?.map((x) => (x.id === issue.id ? issue : x)));
      qc.setQueryData(qk.issue(issue.id), issue);
    },
  });

  const p = project.data;
  const byStatus = new Map<IssueStatus, Issue[]>(ISSUE_STATUSES.map((s) => [s, []]));
  for (const it of issues.data ?? []) (byStatus.get(it.status) ?? byStatus.get('backlog')!).push(it);

  const setR = <K extends keyof RepoInput>(k: K, v: RepoInput[K]) => setRepo((r) => ({ ...r, [k]: v }));

  return (
    <div className="page" style={{ maxWidth: 'none' }}>
      <Crumbs>
        <Link to="/">项目</Link>
        <span>/</span>
        <span className="ellipsis">{p?.name ?? '…'}</span>
      </Crumbs>

      <div className="row mb">
        <div className="grow">
          <h1>{p?.name ?? '加载中…'}</h1>
          {p?.description && <div className="muted small">{p.description}</div>}
        </div>
        <button onClick={() => sync.mutate()} disabled={sync.isPending}>
          {sync.isPending ? '同步中…' : '⟳ 同步平台 Issue'}
        </button>
        <button className="primary" onClick={() => setIssueOpen(true)}>
          + 新建 Issue
        </button>
      </div>

      <div className="panel mb">
        <div className="panel-h">
          <h3 className="grow">仓库</h3>
          <button className="sm" onClick={() => setPickerOpen(true)}>
            📂 选择本地仓库
          </button>
          <button className="sm" onClick={() => setRepoOpen(true)}>
            + 手动添加
          </button>
        </div>
        <div className="panel-b repo-list">
          {(p?.repos ?? []).length === 0 && <span className="dim">尚未添加仓库</span>}
          {p?.repos?.map((r) => (
            <span key={r.id} className="repo-chip" title={`${r.remoteUrl}\n测试命令：${r.testCommand || '无'}`}>
              <b>{r.name}</b>
              <span className="dim">{r.provider}</span>
              {r.ownerRepo && <span className="muted mono">{r.ownerRepo}</span>}
              <span className="dim mono">@{r.defaultBranch}</span>
              <button
                className="ghost sm"
                title="删除仓库"
                onClick={() => confirm(`确认删除仓库 ${r.name}？`) && delRepo.mutate(r.id)}
              >
                ✕
              </button>
            </span>
          ))}
        </div>
      </div>

      <div className="board">
        {ISSUE_STATUSES.map((s) => {
          const list = byStatus.get(s)!;
          return (
            <div key={s} className="board-col">
              <div className="board-col-h">
                <span className="grow">{ISSUE_STATUS_LABEL[s]}</span>
                <span className="badge">{list.length}</span>
              </div>
              <div className="board-col-b">
                {list.map((it) => (
                  <div key={it.id} className={`issue-card${it.awaiting ? ' awaiting' : ''}`}>
                    <div className="meta">
                      <span className="mono ellipsis grow">{it.key}</span>
                      {it.awaiting && <span className="badge awaiting">待审批</span>}
                    </div>
                    <Link to={`/issues/${it.id}`} className="title">
                      {it.title}
                    </Link>
                    <div className="meta">
                      {it.labels?.slice(0, 3).map((l) => (
                        <span key={l} className="badge">
                          {l}
                        </span>
                      ))}
                      <span className="grow" />
                      <select
                        className="sm"
                        value={it.status}
                        onChange={(e) => setStatus.mutate({ issueId: it.id, status: e.target.value as IssueStatus })}
                      >
                        {ISSUE_STATUSES.map((x) => (
                          <option key={x} value={x}>
                            {ISSUE_STATUS_LABEL[x]}
                          </option>
                        ))}
                      </select>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          );
        })}
      </div>

      {repoOpen && (
        <Modal
          title="添加仓库"
          onClose={() => setRepoOpen(false)}
          footer={
            <>
              <button onClick={() => setPickerOpen(true)}>📂 选择本地目录自动填充…</button>
              <span className="grow" />
              <button onClick={() => setRepoOpen(false)}>取消</button>
              <button
                className="primary"
                disabled={!repo.name.trim() || !repo.remoteUrl.trim() || addRepo.isPending}
                onClick={() => addRepo.mutate()}
              >
                添加
              </button>
            </>
          }
        >
          <div className="repo-form">
            <label className="field">
              目录名 name
              <input autoFocus value={repo.name} placeholder="frontend" onChange={(e) => setR('name', e.target.value)} />
            </label>
            <label className="field">
              平台 provider
              <select value={repo.provider} onChange={(e) => setR('provider', e.target.value as Provider)}>
                {PROVIDERS.map((x) => (
                  <option key={x} value={x}>
                    {x}
                  </option>
                ))}
              </select>
            </label>
            <label className="field">
              默认分支
              <input value={repo.defaultBranch} onChange={(e) => setR('defaultBranch', e.target.value)} />
            </label>
          </div>
          <label className="field">
            Remote URL / 本地路径
            <input
              value={repo.remoteUrl}
              placeholder="git@github.com:owner/name.git 或 /path/to/local/repo"
              onChange={(e) => setR('remoteUrl', e.target.value)}
            />
          </label>
          {repo.provider !== 'plain' && (
            <label className="field">
              owner/repo
              <input value={repo.ownerRepo} placeholder="owner/name" onChange={(e) => setR('ownerRepo', e.target.value)} />
            </label>
          )}
          <label className="field">
            测试命令（可选）
            <input value={repo.testCommand} placeholder="pnpm test" onChange={(e) => setR('testCommand', e.target.value)} />
          </label>
        </Modal>
      )}

      {pickerOpen && <LocalDirPickerModal onClose={() => setPickerOpen(false)} onPick={handlePickedLocal} />}

      {issueOpen && (
        <Modal
          title="新建 Issue"
          onClose={() => setIssueOpen(false)}
          footer={
            <>
              <button onClick={() => setIssueOpen(false)}>取消</button>
              <button
                className="primary"
                disabled={!title.trim() || createIssue.isPending}
                onClick={() => createIssue.mutate()}
              >
                创建
              </button>
            </>
          }
        >
          <label className="field">
            标题
            <input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} />
          </label>
          <label className="field">
            正文（Markdown）
            <textarea rows={12} className="mono" value={body} onChange={(e) => setBody(e.target.value)} />
          </label>
        </Modal>
      )}
    </div>
  );
}
