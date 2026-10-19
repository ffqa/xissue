import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Crumbs, fmtTime, Modal } from '../components/common';
import { LocalDirPickerModal } from '../components/LocalDirPicker';
import { CherryPickModal } from '../components/CherryPickModal';
import { toast } from '../toast';
import {
  type InspectResult,
  type Issue,
  type IssueStatus,
  type Provider,
  type RepoInput,
} from '../types';

const PROVIDERS: Provider[] = ['github', 'gitlab', 'gitea', 'plain'];
const emptyRepo: RepoInput = {
  name: '',
  provider: 'github',
  remoteUrl: '',
  ownerRepo: '',
  defaultBranch: 'main',
  testCommand: '',
};

interface StageColumn {
  id: 'todo' | 'running' | 'review' | 'done';
  title: string;
  subtitle: string;
  statuses: IssueStatus[];
}

const WORKFLOW_COLUMNS: StageColumn[] = [
  {
    id: 'todo',
    title: '📥 待办池 (Todo)',
    subtitle: '拖入「执行中」即可自动启动 Agent',
    statuses: ['backlog', 'todo'],
  },
  {
    id: 'running',
    title: '⚡ 执行中 (Running)',
    subtitle: '拖入此列自动启动 Agent 编码',
    statuses: ['running', 'failed'],
  },
  {
    id: 'review',
    title: '🔍 待验收 / PR (Review)',
    subtitle: '点击进入详情页审查 Diff 与提 PR',
    statuses: ['review', 'pr_open'],
  },
  {
    id: 'done',
    title: '✅ 已完成 (Done)',
    subtitle: 'PR 已合并 / 已归档',
    statuses: ['done'],
  },
];

function summarizeIssue(it: Issue): string {
  const raw = (it.body ?? '')
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/[#>*_`~-]/g, ' ')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/\s+/g, ' ')
    .trim();
  if (raw) {
    return raw.length > 90 ? raw.slice(0, 90) + '…' : raw;
  }
  if (it.prUrl) {
    return `PR (${it.prState || 'open'}): ${it.prUrl}`;
  }
  if (it.status === 'failed') {
    return '上次执行失败，进入详情查看日志或拖入「执行中」重试';
  }
  return '暂无摘要描述，点击卡片进入详情页查看';
}

export default function ProjectPage() {
  const id = Number(useParams().id);
  const nav = useNavigate();
  const qc = useQueryClient();
  const project = useQuery({ queryKey: qk.project(id), queryFn: () => api.project(id) });
  const issues = useQuery({ queryKey: qk.issues(id), queryFn: () => api.issues(id) });
  const agentsQ = useQuery({ queryKey: qk.agents, queryFn: api.agents });
  const watchesQ = useQuery({ queryKey: qk.watches(id), queryFn: () => api.watches(id) });

  const [repoOpen, setRepoOpen] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [repo, setRepo] = useState<RepoInput>(emptyRepo);

  const [issueOpen, setIssueOpen] = useState(false);
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [autoRunNew, setAutoRunNew] = useState(false);

  const [watchRepoId, setWatchRepoId] = useState<number>(0);
  const [watchBranch, setWatchBranch] = useState('');

  const [cpOpen, setCpOpen] = useState(false);
  const [cpInitRepoId, setCpInitRepoId] = useState<number | undefined>();
  const [cpInitBranch, setCpInitBranch] = useState<string | undefined>();

  const [draggingId, setDraggingId] = useState<number | null>(null);
  const [dragOverCol, setDragOverCol] = useState<StageColumn['id'] | null>(null);

  const [doneModalOpen, setDoneModalOpen] = useState(false);
  const [selectedDoneIds, setSelectedDoneIds] = useState<number[]>([]);
  const [doneSearchText, setDoneSearchText] = useState('');

  const deleteIssueMut = useMutation({
    mutationFn: (issueId: number) => api.deleteIssue(issueId),
    onSuccess: (_, issueId) => {
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      setSelectedDoneIds((prev) => prev.filter((x) => x !== issueId));
      toast.success('已删除任务');
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const batchDeleteMut = useMutation({
    mutationFn: (ids: number[]) => api.batchDeleteIssues(ids),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      setSelectedDoneIds([]);
      toast.success(`已成功批量删除 ${r.deleted} 个已完成任务`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const [settingsOpen, setSettingsOpen] = useState(false);
  const [cfgTargetBranch, setCfgTargetBranch] = useState('dev');
  const [cfgAutoPR, setCfgAutoPR] = useState(true);

  const updateProjectMut = useMutation({
    mutationFn: (b: { targetBranch?: string; autoPrOnConfirm?: boolean }) =>
      api.updateProject(id, b),
    onSuccess: (updated) => {
      qc.setQueryData(qk.project(id), updated);
      setSettingsOpen(false);
      toast.success('项目工作流设置已保存');
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const defaultAgent =
    (agentsQ.data ?? []).find((a) => a.available && a.name === 'opencode')?.name ||
    (agentsQ.data ?? []).find((a) => a.available)?.name ||
    'opencode';

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
      refreshProject();
      toast.success(`已从平台同步 ${r.imported} 个新 Issue 到待办池`);
      qc.invalidateQueries({ queryKey: qk.issues(id) });
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const refreshReposMut = useMutation({
    mutationFn: () => api.refreshRepos(id),
    onSuccess: (updated) => {
      qc.setQueryData(qk.project(id), updated);
      toast.success('已刷新并自动识别各仓库最新的远端 Git 平台配置');
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const checkWatchesMut = useMutation({
    mutationFn: () => api.checkWatches(id),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: qk.watches(id) });
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      qc.invalidateQueries({ queryKey: qk.notifications });
      toast.success(
        `已检查分支与 PR 状态：${r.watchesTriggered} 个分支有新推送，${r.prsUpdated} 个 PR 状态已更新`,
      );
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const batchRunMut = useMutation({
    mutationFn: () => api.batchRunTodo(id, { agent: defaultAgent }),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      toast.success(`已使用 ${defaultAgent} 自动启动 ${r.started} 个待办任务`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const quickRunMut = useMutation({
    mutationFn: (issueId: number) => api.startRun(issueId, { agent: defaultAgent }),
    onSuccess: (run) => {
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      toast.success(`已启动 Run #${run.id} (${run.agent})`);
      nav(`/issues/${run.issueId}`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const createIssue = useMutation({
    mutationFn: async () => {
      const iss = await api.createIssue(id, { title: title.trim(), body });
      if (autoRunNew) {
        await api.startRun(iss.id, { agent: defaultAgent });
      }
      return iss;
    },
    onSuccess: (iss) => {
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      setTitle('');
      setBody('');
      setIssueOpen(false);
      if (autoRunNew) {
        toast.success(`已创建 ${iss.key} 并立即启动 Agent 执行`);
        nav(`/issues/${iss.id}`);
      }
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const setStatus = useMutation({
    mutationFn: ({ issueId, status }: { issueId: number; status: IssueStatus }) =>
      api.updateIssue(issueId, { status }),
    onSuccess: (issue) => {
      qc.setQueryData<Issue[]>(qk.issues(id), (old) => old?.map((x) => (x.id === issue.id ? issue : x)));
      qc.setQueryData(qk.issue(issue.id), issue);
    },
  });

  const addWatchMut = useMutation({
    mutationFn: () => {
      const rid = watchRepoId || (project.data?.repos?.[0]?.id ?? 0);
      return api.addWatch(id, { repoId: rid, branch: watchBranch.trim() });
    },
    onSuccess: (w) => {
      qc.invalidateQueries({ queryKey: qk.watches(id) });
      setWatchBranch('');
      toast.success(`已开始监控分支 ${w.repoName}:${w.branch} (当前 ${w.lastSha.slice(0, 7)})`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const delWatchMut = useMutation({
    mutationFn: (watchId: number) => api.deleteWatch(id, watchId),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.watches(id) }),
  });

  const p = project.data;
  const allIssues = issues.data ?? [];
  const watches = watchesQ.data ?? [];

  const doneIssues = allIssues.filter((it) => it.status === 'done');
  const filteredDoneIssues = doneIssues.filter((it) => {
    if (!doneSearchText.trim()) return true;
    const q = doneSearchText.toLowerCase();
    const repoName = (p?.repos ?? []).find((r) => r.id === it.repoId)?.name || '';
    return (
      it.title.toLowerCase().includes(q) ||
      it.key.toLowerCase().includes(q) ||
      repoName.toLowerCase().includes(q)
    );
  });

  const allFilteredSelected =
    filteredDoneIssues.length > 0 &&
    filteredDoneIssues.every((it) => selectedDoneIds.includes(it.id));

  const toggleSelectAll = () => {
    if (allFilteredSelected) {
      setSelectedDoneIds((prev) =>
        prev.filter((id) => !filteredDoneIssues.some((it) => it.id === id)),
      );
    } else {
      const toAdd = filteredDoneIssues.map((it) => it.id);
      setSelectedDoneIds((prev) => Array.from(new Set([...prev, ...toAdd])));
    }
  };

  const toggleSelectOne = (issueId: number) => {
    setSelectedDoneIds((prev) =>
      prev.includes(issueId) ? prev.filter((x) => x !== issueId) : [...prev, issueId],
    );
  };

  const setR = <K extends keyof RepoInput>(k: K, v: RepoInput[K]) => setRepo((r) => ({ ...r, [k]: v }));

  const handleDropToColumn = (col: StageColumn, issueId: number) => {
    const target = allIssues.find((x) => x.id === issueId);
    if (!target) return;
    if (col.id === 'running') {
      if (target.status === 'running') return;
      quickRunMut.mutate(target.id);
      return;
    }
    const nextStatus: IssueStatus = col.id === 'todo' ? 'todo' : col.id === 'review' ? 'review' : 'done';
    if (target.status !== nextStatus) {
      setStatus.mutate({ issueId: target.id, status: nextStatus });
    }
  };

  return (
    <div className="page project-page">
      <div className="project-header-section">
        <Crumbs>
          <Link to="/">项目</Link>
          <span>/</span>
          <span className="ellipsis">{p?.name ?? '…'}</span>
        </Crumbs>

      <div className="row mb" style={{ flexWrap: 'wrap', gap: 8 }}>
        <div className="grow">
          <h1>{p?.name ?? '加载中…'}</h1>
          {p?.description && <div className="muted small">{p.description}</div>}
        </div>
        <button
          onClick={() => checkWatchesMut.mutate()}
          disabled={checkWatchesMut.isPending}
          title="立即拉取监控分支的新推送，并通过 gh CLI 检查所有已提交 PR（含跨库/上游 PR）的合并状态"
        >
          {checkWatchesMut.isPending ? '检查中…' : '🔄 检查 PR & 分支状态'}
        </button>
        <button
          onClick={() => {
            setCpInitRepoId(p?.repos?.[0]?.id);
            setCpInitBranch(undefined);
            setCpOpen(true);
          }}
          disabled={(p?.repos ?? []).length === 0}
          title="从指定分支勾选 Commit，Cherry-Pick 到目标分支创建 PR 并生成标准改动记录"
        >
          🍒 Cherry-Pick 提 PR
        </button>
        <button onClick={() => sync.mutate()} disabled={sync.isPending}>
          {sync.isPending ? '同步中…' : '⟳ 同步平台 Issue'}
        </button>
        <button
          className="ghost"
          onClick={() => {
            setCfgTargetBranch(p?.targetBranch || 'dev');
            setCfgAutoPR(p?.autoPrOnConfirm ?? true);
            setSettingsOpen(true);
          }}
          title="设置 PR 默认目标基准分支（如 dev）及验收自动提 PR 策略，避免直接合入 main 触发自动打包"
        >
          ⚙️ PR目标: <code className="mono" style={{ color: 'var(--accent)' }}>{p?.targetBranch || 'dev'}</code>
        </button>
        <button className="primary" onClick={() => setIssueOpen(true)}>
          + 新建 Issue
        </button>
      </div>

      {/* 仓库与分支监控双栏面板 */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(420px, 1fr))',
          gap: 12,
          marginBottom: 14,
        }}
      >
        <div className="panel">
          <div className="panel-h">
            <h3 className="grow">📦 关联仓库 ({(p?.repos ?? []).length})</h3>
            <button
              className="sm"
              title="自动检测本地仓库是否已添加远程或推送到 GitHub/GitLab 平台并自动更新识别"
              disabled={refreshReposMut.isPending}
              onClick={() => refreshReposMut.mutate()}
            >
              {refreshReposMut.isPending ? '检测中…' : '↻ 刷新识别'}
            </button>
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
              <span
                key={r.id}
                className="repo-chip"
                title={`${r.remoteUrl}\n测试命令：${r.testCommand || '无'}`}
              >
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

        {/* 分支监控 Watch + Notification */}
        <div className="panel">
          <div className="panel-h" style={{ flexWrap: 'wrap' }}>
            <h3 className="grow">👁 分支推送监控 ({watches.length})</h3>
            {(p?.repos ?? []).length > 0 && (
              <div className="row" style={{ gap: 6 }}>
                <select
                  className="sm"
                  style={{ width: 130 }}
                  value={watchRepoId || p?.repos?.[0]?.id || 0}
                  onChange={(e) => setWatchRepoId(Number(e.target.value))}
                >
                  {(p?.repos ?? []).map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.name}
                    </option>
                  ))}
                </select>
                <input
                  className="sm mono"
                  style={{ width: 140 }}
                  placeholder="分支名 (如 main/dev)"
                  value={watchBranch}
                  onChange={(e) => setWatchBranch(e.target.value)}
                />
                <button
                  className="sm"
                  disabled={addWatchMut.isPending}
                  onClick={() => addWatchMut.mutate()}
                >
                  + 监控分支
                </button>
              </div>
            )}
          </div>
          <div className="panel-b" style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {watches.length === 0 ? (
              <span className="dim small">
                添加要监控的分支（例如同事分支或上游 dev 分支），有人推送新提交时顶部 🔔 铃铛会自动提醒，并支持一键 Cherry-Pick 提 PR。
              </span>
            ) : (
              watches.map((w) => (
                <div
                  key={w.id}
                  className="row"
                  style={{
                    padding: '4px 8px',
                    borderRadius: 6,
                    border: '1px solid var(--border)',
                    background: 'var(--bg-2)',
                    gap: 8,
                    flexWrap: 'wrap',
                  }}
                >
                  <span className="mono small">
                    <b>{w.repoName}</b>:{w.branch}
                  </span>
                  <code className="mono small" style={{ color: 'var(--accent)' }}>
                    {w.lastSha ? w.lastSha.slice(0, 7) : '—'}
                  </code>
                  <span className="small muted ellipsis grow" title={w.lastMessage}>
                    {w.lastMessage || '无提交信息'}{' '}
                    {w.lastAuthor && <span className="dim">({w.lastAuthor})</span>}
                  </span>
                  <span className="small dim">{fmtTime(w.updatedAt)}</span>
                  <button
                    className="sm ghost"
                    title="从该分支选择提交 Cherry-Pick 到目标分支并提 PR"
                    onClick={() => {
                      setCpInitRepoId(w.repoId);
                      setCpInitBranch(w.branch);
                      setCpOpen(true);
                    }}
                  >
                    🍒 Cherry-Pick
                  </button>
                  <button
                    className="sm ghost"
                    title="取消监控"
                    onClick={() => delWatchMut.mutate(w.id)}
                  >
                    ✕
                  </button>
                </div>
              ))
            )}
          </div>
        </div>
      </div>
      </div>

      {/* 4 列全宽工作流看板：撑满全屏、外框固定、内部纵向滚动 */}
      <div className="board">
        {WORKFLOW_COLUMNS.map((col) => {
          const list = allIssues.filter((it) => col.statuses.includes(it.status));
          const isDoneCol = col.id === 'done';
          const displayList = isDoneCol ? list.slice(0, 10) : list;
          const isOver = dragOverCol === col.id;
          return (
            <div
              key={col.id}
              className={`board-col${isOver ? ' drag-over' : ''}`}
              onDragOver={(e) => {
                e.preventDefault();
                e.dataTransfer.dropEffect = 'move';
                if (dragOverCol !== col.id) setDragOverCol(col.id);
              }}
              onDragLeave={() => {
                if (dragOverCol === col.id) setDragOverCol(null);
              }}
              onDrop={(e) => {
                e.preventDefault();
                setDragOverCol(null);
                const raw = e.dataTransfer.getData('text/plain');
                const droppedId = Number(raw) || draggingId;
                setDraggingId(null);
                if (droppedId) {
                  handleDropToColumn(col, droppedId);
                }
              }}
            >
              <div className="board-col-h" style={{ flexDirection: 'column', alignItems: 'stretch', gap: 4 }}>
                <div className="row">
                  <span className="grow">{col.title}</span>
                  {col.id === 'todo' && list.length > 0 && (
                    <button
                      type="button"
                      className="sm primary"
                      disabled={batchRunMut.isPending}
                      onClick={() => batchRunMut.mutate()}
                      title={`使用 ${defaultAgent} 一键自动执行本列所有待办任务`}
                    >
                      {batchRunMut.isPending ? '启动中…' : '⚡ 自动执行待办'}
                    </button>
                  )}
                  {isDoneCol ? (
                    <span
                      className="badge badge-clickable"
                      onClick={() => setDoneModalOpen(true)}
                      title="点击查看全部历史已完成任务，支持搜索与多选批量管理/删除"
                    >
                      {list.length}
                    </span>
                  ) : (
                    <span className="badge">{list.length}</span>
                  )}
                </div>
                <div className="small dim" style={{ fontWeight: 400, textTransform: 'none' }}>
                  {col.subtitle}
                </div>
              </div>

              <div className="board-col-b">
                {list.length === 0 && (
                  <div className="empty small" style={{ padding: 20 }}>
                    {col.id === 'running' ? '将待办卡片拖入此处即可自动启动执行' : '暂无任务'}
                  </div>
                )}
                {displayList.map((it) => (
                  <Link
                    key={it.id}
                    to={`/issues/${it.id}`}
                    draggable
                    onDragStart={(e) => {
                      setDraggingId(it.id);
                      e.dataTransfer.effectAllowed = 'move';
                      e.dataTransfer.setData('text/plain', String(it.id));
                    }}
                    onDragEnd={() => {
                      setDraggingId(null);
                      setDragOverCol(null);
                    }}
                    className={`issue-card${it.awaiting ? ' awaiting' : ''}${
                      draggingId === it.id ? ' dragging' : ''
                    }`}
                  >
                    <div className="issue-card-head">
                      <span className="title">{it.title}</span>
                      {it.awaiting ? (
                        <span className="badge awaiting">待审批</span>
                      ) : (
                        <span className="mono dim small">{it.key}</span>
                      )}
                    </div>
                    <div className="issue-card-summary">{summarizeIssue(it)}</div>
                  </Link>
                ))}
                {isDoneCol && list.length > 10 && (
                  <div
                    className="board-col-more"
                    onClick={() => setDoneModalOpen(true)}
                    title="点击查看全部历史已完成任务"
                  >
                    仅展示最近 10 条 · 点击查看全部历史 ({list.length}) ➔
                  </div>
                )}
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

      {cpOpen && p && (
        <CherryPickModal
          projectId={id}
          repos={p.repos ?? []}
          initialRepoId={cpInitRepoId}
          initialSourceBranch={cpInitBranch}
          onClose={() => setCpOpen(false)}
        />
      )}

      {issueOpen && (
        <Modal
          title="新建 Issue"
          onClose={() => setIssueOpen(false)}
          footer={
            <>
              <label className="row small" style={{ gap: 6, cursor: 'pointer', marginRight: 'auto' }}>
                <input
                  type="checkbox"
                  checked={autoRunNew}
                  onChange={(e) => setAutoRunNew(e.target.checked)}
                />
                创建后立即启动 Agent ({defaultAgent}) 自动执行
              </label>
              <button onClick={() => setIssueOpen(false)}>取消</button>
              <button
                className="primary"
                disabled={!title.trim() || createIssue.isPending}
                onClick={() => createIssue.mutate()}
              >
                {autoRunNew ? '创建并立即执行' : '创建'}
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
            <textarea rows={10} className="mono" value={body} onChange={(e) => setBody(e.target.value)} />
          </label>
        </Modal>
      )}

      {doneModalOpen && (
        <Modal
          title={`✅ 已完成任务历史与批量管理 (${doneIssues.length})`}
          size="xl"
          onClose={() => {
            setDoneModalOpen(false);
            setSelectedDoneIds([]);
            setDoneSearchText('');
          }}
          footer={
            <div className="row" style={{ width: '100%', justifyContent: 'space-between' }}>
              <div className="small muted">
                已勾选 <b>{selectedDoneIds.length}</b> / {filteredDoneIssues.length} 项（总计 {doneIssues.length} 条已完成）
              </div>
              <div className="row" style={{ gap: 8 }}>
                {selectedDoneIds.length > 0 && (
                  <button
                    className="danger"
                    disabled={batchDeleteMut.isPending}
                    onClick={() => {
                      if (
                        window.confirm(
                          `确定要永久删除选中的 ${selectedDoneIds.length} 个已完成任务吗？此操作不可撤销。`,
                        )
                      ) {
                        batchDeleteMut.mutate(selectedDoneIds);
                      }
                    }}
                  >
                    {batchDeleteMut.isPending
                      ? '正在删除…'
                      : `🗑️ 批量删除选中项 (${selectedDoneIds.length})`}
                  </button>
                )}
                <button
                  onClick={() => {
                    setDoneModalOpen(false);
                    setSelectedDoneIds([]);
                    setDoneSearchText('');
                  }}
                >
                  关闭
                </button>
              </div>
            </div>
          }
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div className="row" style={{ gap: 10, flexWrap: 'wrap' }}>
              <input
                style={{ flex: 1, minWidth: 240 }}
                placeholder="按标题、Key、仓库名搜索已完成任务…"
                value={doneSearchText}
                onChange={(e) => setDoneSearchText(e.target.value)}
                autoFocus
              />
              <label
                className="row"
                style={{
                  cursor: 'pointer',
                  userSelect: 'none',
                  fontSize: 13,
                  gap: 6,
                  padding: '4px 8px',
                  borderRadius: 6,
                  border: '1px solid var(--border)',
                  background: 'var(--bg-2)',
                }}
              >
                <input
                  type="checkbox"
                  checked={allFilteredSelected}
                  onChange={toggleSelectAll}
                  disabled={filteredDoneIssues.length === 0}
                />
                <span>全选当前筛选列表 ({filteredDoneIssues.length})</span>
              </label>
            </div>

            <div style={{ maxHeight: '60vh', overflowY: 'auto', border: '1px solid var(--border)', borderRadius: 8 }}>
              {filteredDoneIssues.length === 0 ? (
                <div className="empty small" style={{ padding: 36 }}>
                  {doneSearchText ? '未找到符合条件的已完成任务' : '暂无已完成任务'}
                </div>
              ) : (
                <table className="history-table">
                  <thead>
                    <tr>
                      <th style={{ width: 42, textAlign: 'center' }}>
                        <input
                          type="checkbox"
                          checked={allFilteredSelected}
                          onChange={toggleSelectAll}
                        />
                      </th>
                      <th style={{ width: 110 }}>标识 Key</th>
                      <th>任务标题</th>
                      <th style={{ width: 140 }}>关联仓库</th>
                      <th style={{ width: 150 }}>完成时间</th>
                      <th style={{ width: 80, textAlign: 'right' }}>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredDoneIssues.map((it) => {
                      const isSelected = selectedDoneIds.includes(it.id);
                      const repoName = (p?.repos ?? []).find((r) => r.id === it.repoId)?.name || '未关联';
                      return (
                        <tr key={it.id} className={isSelected ? 'selected' : ''}>
                          <td style={{ textAlign: 'center' }}>
                            <input
                              type="checkbox"
                              checked={isSelected}
                              onChange={() => toggleSelectOne(it.id)}
                            />
                          </td>
                          <td>
                            <code className="mono small" style={{ color: 'var(--accent)' }}>
                              {it.key}
                            </code>
                          </td>
                          <td>
                            <Link
                              to={`/issues/${it.id}`}
                              target="_blank"
                              style={{ fontWeight: 500, color: 'var(--text)' }}
                              title="在新标签页查看详情"
                            >
                              {it.title}
                            </Link>
                          </td>
                          <td>
                            <span className="small muted">📦 {repoName}</span>
                          </td>
                          <td>
                            <span className="small dim">{fmtTime(it.updatedAt)}</span>
                          </td>
                          <td style={{ textAlign: 'right' }}>
                            <button
                              className="sm danger ghost"
                              title="永久删除此任务"
                              disabled={deleteIssueMut.isPending}
                              onClick={() => {
                                if (window.confirm(`确定要永久删除任务「${it.title}」吗？`)) {
                                  deleteIssueMut.mutate(it.id);
                                }
                              }}
                            >
                              🗑️ 删除
                            </button>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        </Modal>
      )}

      {settingsOpen && (
        <Modal
          title="⚙️ 项目工作流与分支合并策略设置"
          size="md"
          onClose={() => setSettingsOpen(false)}
          footer={
            <>
              <button onClick={() => setSettingsOpen(false)}>取消</button>
              <button
                className="primary"
                disabled={!cfgTargetBranch.trim() || updateProjectMut.isPending}
                onClick={() =>
                  updateProjectMut.mutate({
                    targetBranch: cfgTargetBranch.trim(),
                    autoPrOnConfirm: cfgAutoPR,
                  })
                }
              >
                {updateProjectMut.isPending ? '保存中…' : '保存设置'}
              </button>
            </>
          }
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            <label className="field">
              PR 默认目标基准分支 (Target Branch)
              <input
                className="mono"
                value={cfgTargetBranch}
                onChange={(e) => setCfgTargetBranch(e.target.value)}
                placeholder="dev"
                autoFocus
              />
              <span className="small dim">
                💡 推荐设为 <code>dev</code>。Issue 改完后 PR 将默认提到此分支，待集成验证后再由 dev 合入 main，避免 main 一提交就自动打包触发构建/发布。
              </span>
            </label>

            <div className="row" style={{ gap: 8 }}>
              <span className="small muted">快捷选择常用分支:</span>
              <button
                type="button"
                className="sm ghost"
                onClick={() => setCfgTargetBranch('dev')}
              >
                dev (推荐)
              </button>
              <button
                type="button"
                className="sm ghost"
                onClick={() => setCfgTargetBranch('main')}
              >
                main
              </button>
              {p?.repos?.[0]?.defaultBranch &&
                p.repos[0].defaultBranch !== 'dev' &&
                p.repos[0].defaultBranch !== 'main' && (
                  <button
                    type="button"
                    className="sm ghost"
                    onClick={() => setCfgTargetBranch(p.repos[0].defaultBranch)}
                  >
                    {p.repos[0].defaultBranch}
                  </button>
                )}
            </div>

            <label className="row" style={{ gap: 8, cursor: 'pointer', marginTop: 4 }}>
              <input
                type="checkbox"
                checked={cfgAutoPR}
                onChange={(e) => setCfgAutoPR(e.target.checked)}
              />
              <span style={{ fontSize: 13 }}>
                <b>改完验收通过后自动创建 PR</b>（向上述目标基准分支发起 PR，生成标准改动记录）
              </span>
            </label>
          </div>
        </Modal>
      )}
    </div>
  );
}
