import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { fmtTime } from './common';
import { copyText } from '../clipboard';
import { toast } from '../toast';
import { getBranchWebUrl, getCommitWebUrl } from '../gitUrls';
import type { CommitDetail, CommitFile } from '../types';

function statusColor(status: string): string {
  if (status.startsWith('A')) return 'var(--success)';
  if (status.startsWith('D')) return 'var(--error)';
  if (status.startsWith('R') || status.startsWith('C')) return 'var(--accent)';
  return 'var(--warning)';
}

function statusLabel(status: string): string {
  if (status.startsWith('A')) return '新增';
  if (status.startsWith('D')) return '删除';
  if (status.startsWith('R')) return '重命名';
  if (status.startsWith('C')) return '复制';
  if (status.startsWith('T')) return '类型';
  return '修改';
}

function PatchLine({ line }: { line: string }) {
  let cls = 'diff-line';
  if (line.startsWith('@@')) cls += ' diff-hunk';
  else if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('diff --git') || line.startsWith('index '))
    cls += ' diff-meta';
  else if (line.startsWith('+')) cls += ' diff-add';
  else if (line.startsWith('-')) cls += ' diff-del';
  return <div className={cls}>{line || ' '}</div>;
}

function CommitFiles({ files }: { files: CommitFile[] }) {
  if (files.length === 0) return <span className="dim small">无文件变更</span>;
  return (
    <div className="commit-files">
      {files.map((f) => (
        <div className="commit-file-row" key={`${f.status}:${f.oldPath || ''}:${f.path}`}>
          <span className="commit-file-status" style={{ color: statusColor(f.status) }} title={f.status}>
            {statusLabel(f.status)}
          </span>
          <code className="mono small grow ellipsis" title={f.oldPath ? `${f.oldPath} → ${f.path}` : f.path}>
            {f.oldPath ? `${f.oldPath} → ` : ''}
            {f.path}
          </code>
          <span className="small" style={{ color: 'var(--success)' }}>+{f.additions}</span>
          <span className="small" style={{ color: 'var(--error)' }}>-{f.deletions}</span>
        </div>
      ))}
    </div>
  );
}

export function CommitsPanel({
  projectId,
  initialRepoId,
  initialBranch,
  initialSha,
}: {
  projectId: number;
  initialRepoId?: number;
  initialBranch?: string;
  initialSha?: string;
}) {
  const [detailExpanded, setDetailExpanded] = useState(false);
  const [filesExpanded, setFilesExpanded] = useState(false);
  const [fullDiff, setFullDiff] = useState(false);
  const selectedRef = useRef<HTMLDivElement | null>(null);
  const qc = useQueryClient();

  const projectQ = useQuery({ queryKey: qk.project(projectId), queryFn: () => api.project(projectId, false), enabled: !!projectId });
  const project = projectQ.data;
  const repos = project?.repos ?? [];

  const [repoId, setRepoId] = useState(initialRepoId || 0);
  const repo = repos.find((r) => r.id === repoId) || repos[0];
  const [branch, setBranch] = useState(initialBranch || '');
  const [shaFromUrl, setShaFromUrl] = useState(initialSha || '');
  const requestedBranch = branch || repo?.defaultBranch || '';

  const branchesQ = useQuery({
    queryKey: qk.repoBranches(repoId),
    queryFn: () => api.repoBranches(repoId),
    enabled: !!repoId,
  });
  const branches = branchesQ.data ?? [];
  const activeBranch =
    branches.length > 0 && !branches.includes(requestedBranch)
      ? branches.includes(repo?.defaultBranch || '')
        ? repo!.defaultBranch
        : branches[0]
      : requestedBranch;

  useEffect(() => {
    if (!repos.length) return;
    if (!repoId || !repos.some((r) => r.id === repoId)) {
      setRepoId(repos[0].id);
      setBranch(repos[0].defaultBranch);
      setShaFromUrl('');
    }
  }, [repoId, repos]);

  useEffect(() => {
    const next = initialBranch || repo?.defaultBranch;
    if (!branch && next) setBranch(next);
  }, [branch, repo?.defaultBranch, initialBranch]);

  const commitsQ = useQuery({
    queryKey: qk.repoCommits(repoId, activeBranch),
    queryFn: () => api.repoCommits(repoId, activeBranch, 50),
    enabled: !!repoId && !!activeBranch,
  });
  const commits = commitsQ.data ?? [];
  const refreshMut = useMutation({
    mutationFn: () => api.repoCommits(repoId, activeBranch, 50, true),
    onSuccess: (data) => {
      qc.setQueryData(qk.repoCommits(repoId, activeBranch), data);
      toast.success('已同步远端提交');
    },
    onError: (e) => toast.error((e as Error).message),
  });
  const selectedSha = shaFromUrl || commits[0]?.sha || '';
  const detailQ = useQuery({
    queryKey: qk.repoCommitDetail(repoId, selectedSha),
    queryFn: () => api.repoCommitDetail(repoId, selectedSha),
    enabled: !!repoId && !!selectedSha,
  });
  const detail: CommitDetail | undefined = detailQ.data;

  const patchLines = useMemo(() => (detail?.patch ? detail.patch.split('\n') : []), [detail?.patch]);

  const total = useMemo(() => {
    let add = 0;
    let del = 0;
    for (const f of detail?.files ?? []) {
      add += f.additions;
      del += f.deletions;
    }
    return { add, del };
  }, [detail]);

  useEffect(() => {
    selectedRef.current?.scrollIntoView({ block: 'nearest' });
    setDetailExpanded(false);
    setFilesExpanded(false);
    setFullDiff(false);
  }, [selectedSha, commits.length]);

  const selectRepo = (nextRepoId: number, nextBranch?: string, nextSha?: string) => {
    setRepoId(nextRepoId);
    setBranch(nextBranch || '');
    setShaFromUrl(nextSha || '');
  };
  const selectBranch = (nextBranch: string) => {
    setBranch(nextBranch);
    setShaFromUrl('');
  };
  const selectCommit = (sha: string) => {
    setShaFromUrl(sha);
  };

  if (!projectId || (!projectQ.isLoading && !project)) {
    return <div className="page empty">项目不存在</div>;
  }
  if (!project) return <div className="page empty">正在加载项目…</div>;

  const branchUrl = getBranchWebUrl(repo, activeBranch);
  const commitUrl = detail ? getCommitWebUrl(repo, detail.sha) : null;

  return (
    <div className="commits-modal-body">
      {!repo ? (
        <div className="empty">该项目还没有仓库</div>
      ) : (
        <div className="commits-layout">
          <div className="panel commits-list-panel">
            <div className="commits-toolbar">
              <select
                className="sm"
                value={repoId || ''}
                onChange={(e) => {
                  const nextRepoId = Number(e.target.value);
                  const nextRepo = repos.find((r) => r.id === nextRepoId);
                  selectRepo(Number(e.target.value), nextRepo?.defaultBranch || 'main');
                }}
                title="切换仓库"
              >
                {repos.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.name}
                  </option>
                ))}
              </select>
              <div className="row" style={{ gap: 6 }}>
                <select
                  className="sm mono grow"
                  value={activeBranch}
                  onChange={(e) => selectBranch(e.target.value)}
                  title="切换分支"
                >
                  {branches.length === 0 && activeBranch && <option value={activeBranch}>{activeBranch}</option>}
                  {branches.map((b) => (
                    <option key={b} value={b}>
                      {b}
                    </option>
                  ))}
                </select>
                <button className="sm" onClick={() => refreshMut.mutate()} disabled={refreshMut.isPending || commitsQ.isFetching}>
                  {refreshMut.isPending ? '同步中…' : '↻ 同步'}
                </button>
                {branchUrl && (
                  <a className="ghost sm btn" href={branchUrl} target="_blank" rel="noreferrer" style={{ textDecoration: 'none', whiteSpace: 'nowrap' }}>
                    ↗ 平台
                  </a>
                )}
              </div>
            </div>
            <div className="panel-h">
              <b className="grow">最近提交 ({commits.length})</b>
              {commitsQ.isFetching && <span className="small dim">同步中…</span>}
            </div>
            <div className="panel-b commits-list">
              {commitsQ.isLoading ? (
                <div className="empty">正在获取提交记录…</div>
              ) : commitsQ.isError ? (
                <div className="empty">加载失败：{(commitsQ.error as Error).message}</div>
              ) : commits.length === 0 ? (
                <div className="empty">暂无提交记录</div>
              ) : (
                commits.map((c) => {
                  const selected = c.sha === selectedSha;
                  return (
                    <div
                      key={c.sha}
                      ref={selected ? selectedRef : undefined}
                      className={`commit-list-item${selected ? ' selected' : ''}${c.sha === shaFromUrl ? ' target' : ''}`}
                      onClick={() => selectCommit(c.sha)}
                    >
                      <div className="row" style={{ gap: 8 }}>
                        <code className="mono small commit-sha">{c.shortSha}</code>
                        <span className="grow ellipsis small" title={c.subject}>
                          {c.subject}
                        </span>
                      </div>
                      <div className="row small dim" style={{ gap: 8, marginTop: 3 }}>
                        <span className="ellipsis">{c.author}</span>
                        <span className="grow" />
                        <span>{fmtTime(c.date)}</span>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          </div>

          <div className="panel commit-detail-panel">
            {!selectedSha ? (
              <div className="empty">从左侧选择一条提交查看详情</div>
            ) : detailQ.isLoading ? (
              <div className="empty">正在加载提交详情…</div>
            ) : detailQ.isError ? (
              <div className="empty">加载失败：{(detailQ.error as Error).message}</div>
            ) : detail ? (
              <>
                <div className="commit-detail-head">
                  <div className="row" style={{ gap: 8, alignItems: 'flex-start' }}>
                    <div className="grow">
                      <h3 style={{ margin: 0, lineHeight: 1.4 }}>{detail.subject}</h3>
                      <div className="row small dim" style={{ gap: 10, marginTop: 6, flexWrap: 'wrap' }}>
                        <span>{detail.author} &lt;{detail.authorEmail}&gt;</span>
                        <span>{detail.date}</span>
                        <code className="mono">{detail.sha.slice(0, 12)}</code>
                        <button
                          className="ghost sm"
                          style={{ padding: '0 6px', fontSize: 11 }}
                          onClick={() =>
                            void copyText(detail.sha).then((ok) =>
                              ok ? toast.success('已复制 Commit SHA') : toast.error('复制失败')
                            )
                          }
                        >
                          📋 复制 SHA
                        </button>
                        {commitUrl && (
                          <a href={commitUrl} target="_blank" rel="noreferrer" className="small" style={{ color: 'var(--accent)' }}>
                            ↗ 在平台查看
                          </a>
                        )}
                      </div>
                    </div>
                    <div className="small dim" style={{ whiteSpace: 'nowrap' }}>
                      +{total.add} / -{total.del}
                    </div>
                  </div>
                  {detail.body.trim() && detail.body.trim() !== detail.subject.trim() && (
                    <>
                      <pre className={`commit-body${detailExpanded ? '' : ' clamped'}`}>{detail.body}</pre>
                      {(detail.body.length > 240 || detail.body.split('\n').length > 4) && (
                        <button className="ghost sm" style={{ marginTop: 4, padding: '0 6px' }} onClick={() => setDetailExpanded((v) => !v)}>
                          {detailExpanded ? '收起提交说明' : '展开提交说明'}
                        </button>
                      )}
                    </>
                  )}
                  {detail.parents.length > 0 && (
                    <div className="small dim" style={{ marginTop: 6 }}>
                      父提交：{detail.parents.map((p) => p.slice(0, 10)).join(', ')}
                    </div>
                  )}
                </div>

                <div className="commit-section-title">
                  变更文件 ({detail.files.length})
                  <span className="grow" />
                  {detail.files.length > 10 && (
                    <button className="ghost sm" style={{ padding: '0 6px' }} onClick={() => setFilesExpanded((v) => !v)}>
                      {filesExpanded ? '收起文件列表' : `展开全部 ${detail.files.length} 个`}
                    </button>
                  )}
                </div>
                <CommitFiles files={filesExpanded ? detail.files : detail.files.slice(0, 10)} />

                <div className="commit-section-title" style={{ marginTop: 12 }}>
                  Diff
                  {detail.truncated && <span className="small" style={{ color: 'var(--warning)' }}>（已截断）</span>}
                </div>
                <div className="diff-view">
                  {detail.patch ? (
                    <>
                      {(fullDiff ? patchLines : patchLines.slice(0, 800)).map((line, i) => (
                        <PatchLine key={i} line={line} />
                      ))}
                      {!fullDiff && patchLines.length > 800 && (
                        <div className="diff-truncated">
                          已渲染前 800 行，避免超大 diff 卡顿。
                          <button className="sm ghost" onClick={() => setFullDiff(true)}>
                            显示全部 {patchLines.length} 行
                          </button>
                        </div>
                      )}
                    </>
                  ) : (
                    <div className="dim small" style={{ padding: 12 }}>
                      无文本差异（可能为二进制文件或合并提交）
                    </div>
                  )}
                </div>
              </>
            ) : null}
          </div>
        </div>
      )}
    </div>
  );
}
