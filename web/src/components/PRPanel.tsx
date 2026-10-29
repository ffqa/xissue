import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { fmtTime } from './common';
import { toast } from '../toast';
import type { RemotePR } from '../types';
import { filterPRs, type PRFilter } from '../logic/prs';
import { useProjectPRs } from '../hooks/useProjectLists';

export function PRPanel({ projectId }: { projectId: number }) {
  const qc = useQueryClient();
  const [filterState, setFilterState] = useState<PRFilter>('open');
  const [mergeMethods, setMergeMethods] = useState<Record<number, 'squash' | 'merge' | 'rebase'>>({});

  // One query for every tab. ProjectPage keeps it warm and refreshes it every
  // 60s, so opening the modal shows the cached list immediately; switching to
  // Merged/Closed never waits for another provider request.
  // ProjectPage owns the single 60s timer for this shared query.
  const prsQ = useProjectPRs(projectId);
  const prs: RemotePR[] = prsQ.data ?? [];
  const filteredPRs = filterPRs(prs, filterState);

  const pullMut = useMutation({
    mutationFn: () => api.pullProject(projectId),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: qk.projectPRs(projectId) });
      qc.invalidateQueries({ queryKey: qk.issues(projectId) });
      qc.invalidateQueries({ queryKey: qk.watches(projectId) });
      toast.success(res.message || '已成功拉取远端最新代码并同步到本地！');
    },
    onError: (e) => toast.error(`拉取代码失败: ${(e as Error).message}`),
  });

  const mergeMut = useMutation({
    mutationFn: ({ repoId, number, method }: { repoId: number; number: number; method: 'squash' | 'merge' | 'rebase' }) =>
      api.mergeProjectPR(projectId, number, { repoId, method }),
    onSuccess: (res, vars) => {
      qc.invalidateQueries({ queryKey: qk.projectPRs(projectId) });
      qc.invalidateQueries({ queryKey: qk.issues(projectId) });
      qc.invalidateQueries({ queryKey: qk.watches(projectId) });
      qc.invalidateQueries({ queryKey: qk.project(projectId) });
      qc.invalidateQueries({ queryKey: qk.projectPullStatus(projectId) });
      toast.success(res.message || `PR #${vars.number} 已合并`);
    },
    onError: (e) => toast.error(`合并 PR 失败: ${(e as Error).message}`),
  });

  const closeMut = useMutation({
    mutationFn: ({ repoId, number }: { repoId: number; number: number }) =>
      api.closeProjectPR(projectId, number, { repoId }),
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: qk.projectPRs(projectId) });
      qc.invalidateQueries({ queryKey: qk.issues(projectId) });
      toast.info(`PR #${vars.number} 已关闭`);
    },
    onError: (e) => toast.error(`关闭 PR 失败: ${(e as Error).message}`),
  });

  const handleMerge = (pr: RemotePR) => {
    const method = mergeMethods[pr.number] || 'squash';
    const methodLabel = method === 'squash' ? 'Squash & Merge' : method === 'merge' ? 'Merge Commit' : 'Rebase';
    if (confirm(`确认使用 ${methodLabel} 合并 PR #${pr.number}《${pr.title}》吗？\n合并后将自动 pull 最新代码到本地。`)) {
      mergeMut.mutate({ repoId: pr.repoId, number: pr.number, method });
    }
  };

  const handleClose = (pr: RemotePR) => {
    if (confirm(`确认关闭 PR #${pr.number}《${pr.title}》吗？`)) {
      closeMut.mutate({ repoId: pr.repoId, number: pr.number });
    }
  };

  return (
    <div className="col" style={{ gap: 10, minHeight: 0, height: '100%' }}>
      <div className="row" style={{ gap: 6, flexWrap: 'wrap' }}>
        {(
          [
            { id: 'open', label: '待处理 (Open)' },
            { id: 'merged', label: '已合并 (Merged)' },
            { id: 'closed', label: '已关闭 (Closed)' },
            { id: 'all', label: '全部 (All)' },
          ] as const
        ).map((t) => (
          <button
            key={t.id}
            className={`sm ${filterState === t.id ? 'primary' : 'ghost'}`}
            onClick={() => setFilterState(t.id)}
          >
            {t.label}
          </button>
        ))}
        <span className="small muted">共 {filteredPRs.length} 个 PR</span>
        <span className="small dim">每 60 秒自动刷新</span>
        <span className="grow" />
        <button
          className="primary sm"
          disabled={pullMut.isPending}
          onClick={() => pullMut.mutate()}
          title="拉取远端仓库最新改动到本地工作区与镜像"
        >
          {pullMut.isPending ? '正在拉取…' : '📥 拉取最新代码'}
        </button>
        <button className="sm ghost" onClick={() => prsQ.refetch()} disabled={prsQ.isFetching}>
          {prsQ.isFetching ? '刷新中…' : '↻ 刷新'}
        </button>
      </div>

      <div className="panel pr-panel">
        <div className="pr-list">
          {prsQ.isLoading && <div className="empty">正在获取项目 Pull Request 列表…</div>}
          {!prsQ.isLoading && filteredPRs.length === 0 && (
            <div className="empty">
              当前状态下无 Pull Request。
              {filterState !== 'all' && (
                <button className="sm ghost" style={{ marginLeft: 8 }} onClick={() => setFilterState('all')}>
                  查看全部 PR
                </button>
              )}
            </div>
          )}

          {filteredPRs.map((pr) => {
            const isMerged = pr.state === 'merged';
            const isOpen = pr.state === 'open';
            return (
              <div key={`${pr.repoId}-${pr.number}`} className="pr-card">
                <div className="row" style={{ gap: 8, alignItems: 'flex-start' }}>
                  <span
                    className="tag"
                    style={{
                      background: isOpen
                        ? 'rgba(34, 197, 94, 0.15)'
                        : isMerged
                          ? 'rgba(168, 85, 247, 0.15)'
                          : 'rgba(156, 163, 175, 0.15)',
                      color: isOpen ? '#22c55e' : isMerged ? '#a855f7' : 'var(--muted)',
                      borderColor: isOpen ? '#22c55e' : isMerged ? '#a855f7' : 'var(--border)',
                      fontWeight: 600,
                    }}
                  >
                    #{pr.number} {pr.state.toUpperCase()}
                  </span>
                  <span className="repo-chip" style={{ margin: 0, padding: '2px 6px', fontSize: 11 }}>
                    📦 {pr.repoName}
                  </span>
                  {pr.ciState && pr.ciState !== 'unknown' && (
                    <span
                      className="tag"
                      title={`CI: ${pr.ciPassed ?? 0} 通过 · ${pr.ciFailed ?? 0} 失败 · ${pr.ciPending ?? 0} 进行中`}
                      style={{
                        margin: 0,
                        padding: '2px 6px',
                        fontSize: 11,
                        color: pr.ciState === 'failure' ? 'var(--red, #ef4444)' : pr.ciState === 'pending' ? 'var(--yellow, #eab308)' : 'var(--green, #22c55e)',
                        borderColor: 'currentColor',
                      }}
                    >
                      CI {pr.ciState === 'failure' ? '✗' : pr.ciState === 'pending' ? '…' : '✓'}
                    </span>
                  )}
                  <span style={{ fontWeight: 600, fontSize: 14, flex: 1, lineHeight: 1.4 }}>{pr.title}</span>
                  <a
                    href={pr.url}
                    target="_blank"
                    rel="noreferrer"
                    className="small ghost"
                    style={{ textDecoration: 'none', color: 'var(--accent)', whiteSpace: 'nowrap' }}
                    title="在代码托管平台中打开"
                  >
                    ↗ 打开平台
                  </a>
                </div>

                <div className="row" style={{ gap: 12, fontSize: 12, color: 'var(--muted)', flexWrap: 'wrap' }}>
                  <div className="row" style={{ gap: 4 }}>
                    <span>分支:</span>
                    <code className="mono small" style={{ color: 'var(--text)' }}>{pr.headBranch}</code>
                    <span>→</span>
                    <code className="mono small" style={{ color: 'var(--accent)' }}>{pr.baseBranch}</code>
                  </div>
                  <div>创建者: {pr.author}</div>
                  <div>创建时间: {fmtTime(pr.createdAt)}</div>
                  {pr.mergedAt && <div>合并时间: {fmtTime(pr.mergedAt)}</div>}
                </div>

                {pr.body && <div className="pr-body">{pr.body}</div>}

                {isOpen && (
                  <div className="row pr-actions" style={{ gap: 8, alignItems: 'center' }}>
                    <span className="small muted">合并方式:</span>
                    <select
                      className="sm"
                      style={{ height: 26, fontSize: 12, width: 210, flex: 'none' }}
                      value={mergeMethods[pr.number] || 'squash'}
                      onChange={(e) =>
                        setMergeMethods((prev) => ({
                          ...prev,
                          [pr.number]: e.target.value as 'squash' | 'merge' | 'rebase',
                        }))
                      }
                    >
                      <option value="squash">Squash and merge (推荐)</option>
                      <option value="merge">Create a merge commit</option>
                      <option value="rebase">Rebase and merge</option>
                    </select>
                    <button
                      className="primary sm"
                      disabled={mergeMut.isPending}
                      onClick={() => handleMerge(pr)}
                      title="合并此 PR，并刷新镜像/尝试快进本地目标分支"
                    >
                      {mergeMut.isPending ? '合并中…' : '⚡ 合并并刷新本地'}
                    </button>
                    <button
                      className="danger sm"
                      disabled={closeMut.isPending}
                      onClick={() => handleClose(pr)}
                      title="关闭此 Pull Request"
                    >
                      关闭 PR
                    </button>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
