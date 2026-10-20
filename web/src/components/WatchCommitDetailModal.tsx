import React, { useState, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api, qk } from '../api';
import type { BranchWatch, Repo, CommitInfo } from '../types';
import { toast } from '../toast';

interface WatchCommitDetailModalProps {
  watch: BranchWatch;
  repo?: Repo;
  onClose: () => void;
  onCreateIssue?: (title: string, body: string) => void;
}

export function getCommitWebUrl(repo?: Repo, sha?: string): string | null {
  if (!repo || !sha) return null;
  const ownerRepo = repo.ownerRepo?.trim();
  const remote = repo.remoteUrl?.trim() || '';

  if (repo.provider === 'github' && ownerRepo) {
    return `https://github.com/${ownerRepo}/commit/${sha}`;
  }
  if (repo.provider === 'gitlab' && ownerRepo) {
    return `https://gitlab.com/${ownerRepo}/-/commit/${sha}`;
  }
  if (repo.provider === 'gitea' && ownerRepo) {
    return `https://gitea.com/${ownerRepo}/commit/${sha}`;
  }

  // 尝试从 remoteUrl 正则解析 (如 git@github.com:owner/repo.git 或 https://github.com/owner/repo)
  const match = remote.match(/(?:https?:\/\/|git@)([^/:]+)[:/]([^/]+)\/([^/]+?)(?:\.git)?$/);
  if (match) {
    const host = match[1];
    const owner = match[2];
    const name = match[3];
    return `https://${host}/${owner}/${name}/commit/${sha}`;
  }
  return null;
}

export function getBranchWebUrl(repo?: Repo, branch?: string): string | null {
  if (!repo || !branch) return null;
  const ownerRepo = repo.ownerRepo?.trim();
  const remote = repo.remoteUrl?.trim() || '';

  if (repo.provider === 'github' && ownerRepo) {
    return `https://github.com/${ownerRepo}/tree/${encodeURIComponent(branch)}`;
  }
  if (repo.provider === 'gitlab' && ownerRepo) {
    return `https://gitlab.com/${ownerRepo}/-/tree/${encodeURIComponent(branch)}`;
  }
  if (repo.provider === 'gitea' && ownerRepo) {
    return `https://gitea.com/${ownerRepo}/src/branch/${encodeURIComponent(branch)}`;
  }

  const match = remote.match(/(?:https?:\/\/|git@)([^/:]+)[:/]([^/]+)\/([^/]+?)(?:\.git)?$/);
  if (match) {
    const host = match[1];
    const owner = match[2];
    const name = match[3];
    return `https://${host}/${owner}/${name}/tree/${encodeURIComponent(branch)}`;
  }
  return null;
}

export const WatchCommitDetailModal: React.FC<WatchCommitDetailModalProps> = ({
  watch,
  repo,
  onClose,
  onCreateIssue,
}) => {
  const [selectedSha, setSelectedSha] = useState<string>(watch.lastSha || '');

  const commitsQ = useQuery({
    queryKey: qk.repoCommits(watch.repoId, watch.branch),
    queryFn: () => api.repoCommits(watch.repoId, watch.branch, 25),
  });

  const commits = commitsQ.data ?? [];
  const activeCommit = commits.find((c) => c.sha === selectedSha) || commits[0];

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

  const branchUrl = getBranchWebUrl(repo, watch.branch);
  const commitUrl = getCommitWebUrl(repo, activeCommit?.sha || watch.lastSha);

  const handleCopySha = (sha: string) => {
    navigator.clipboard.writeText(sha);
    toast.success('已复制 Commit SHA');
  };

  const handleCreateIssueFromCommit = (c: CommitInfo) => {
    if (!onCreateIssue) return;
    const title = `[分支跟进] ${watch.repoName}:${watch.branch} 提交 ${c.shortSha}`;
    const body = `## 关联提交信息\n- **仓库**: \`${watch.repoName}\`\n- **分支**: \`${watch.branch}\`\n- **提交**: \`${c.sha}\`\n- **作者**: ${c.author}\n- **日期**: ${c.date || '未知'}\n\n### 提交说明\n${c.subject}\n`;
    onCreateIssue(title, body);
    onClose();
  };

  return (
    <div className="modal-backdrop" onClick={onClose} style={{ zIndex: 120 }}>
      <div
        className="modal"
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 'min(780px, 95vw)',
          maxHeight: '85vh',
          display: 'flex',
          flexDirection: 'column',
          padding: 20,
          gap: 14,
        }}
      >
        {/* 头部标题与平台直达链接 */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', borderBottom: '1px solid var(--border)', paddingBottom: 12 }}>
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span style={{ fontSize: 18 }}>📜</span>
              <b style={{ fontSize: 16 }}>监控分支提交记录</b>
              <span className="badge mono" style={{ background: 'var(--bg-2)' }}>
                {watch.repoName}:{watch.branch}
              </span>
            </div>
            <div className="small dim" style={{ marginTop: 4 }}>
              实时追踪远程分支提交变动，可直接查看详情并在平台中打开。
            </div>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {branchUrl && (
              <a
                href={branchUrl}
                target="_blank"
                rel="noreferrer"
                className="ghost sm btn"
                style={{ textDecoration: 'none', display: 'inline-flex', alignItems: 'center', gap: 4 }}
                title="在代码平台网页端查看此分支"
              >
                ↗ 在平台打开分支
              </a>
            )}
            <button className="ghost sm" onClick={onClose}>
              ✕
            </button>
          </div>
        </div>

        {/* 提交历史列表 */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8, flex: 1, minHeight: 0 }}>
          <div className="row" style={{ justifyContent: 'space-between', alignItems: 'center' }}>
            <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-2)' }}>
              最近提交历史 (共 {commits.length} 条):
            </span>
            {commitsQ.isFetching && <span className="small dim">正在刷新…</span>}
          </div>

          <div
            style={{
              maxHeight: 280,
              overflowY: 'auto',
              border: '1px solid var(--border)',
              borderRadius: 6,
              background: 'var(--bg-0)',
            }}
          >
            {commits.length === 0 ? (
              <div style={{ padding: 24, textAlign: 'center', color: 'var(--text-3)', fontSize: 13 }}>
                {commitsQ.isLoading ? '正在获取分支提交记录…' : '暂无提交记录或分支不存在'}
              </div>
            ) : (
              commits.map((c) => {
                const isSelected = (activeCommit?.sha === c.sha);
                const cWebUrl = getCommitWebUrl(repo, c.sha);
                return (
                  <div
                    key={c.sha}
                    onClick={() => setSelectedSha(c.sha)}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      padding: '8px 12px',
                      borderBottom: '1px solid var(--border)',
                      background: isSelected ? 'var(--bg-2)' : 'transparent',
                      cursor: 'pointer',
                      gap: 10,
                      fontSize: 12,
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, flex: 1 }}>
                      <code
                        className="mono"
                        style={{
                          color: isSelected ? 'var(--accent)' : 'var(--text-2)',
                          fontWeight: 600,
                          flexShrink: 0,
                        }}
                      >
                        {c.shortSha}
                      </code>
                      <span className="ellipsis" style={{ color: 'var(--text)' }} title={c.subject}>
                        {c.subject}
                      </span>
                    </div>

                    <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
                      <span className="dim small">{c.author}</span>
                      <span className="dim small" style={{ fontSize: 11 }}>
                        {c.date || ''}
                      </span>
                      {cWebUrl && (
                        <a
                          href={cWebUrl}
                          target="_blank"
                          rel="noreferrer"
                          onClick={(e) => e.stopPropagation()}
                          style={{ color: 'var(--accent)', textDecoration: 'none', fontSize: 13 }}
                          title="在网页端查看此 Commit"
                        >
                          ↗
                        </a>
                      )}
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>

        {/* 选中提交的详情卡片 */}
        {activeCommit && (
          <div
            style={{
              padding: 12,
              borderRadius: 6,
              background: 'var(--bg-2)',
              border: '1px solid var(--border)',
              display: 'flex',
              flexDirection: 'column',
              gap: 8,
            }}
          >
            <div className="row" style={{ justifyContent: 'space-between', alignItems: 'center' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span className="small muted">选中提交:</span>
                <code className="mono small" style={{ color: 'var(--accent)', fontWeight: 600 }}>
                  {activeCommit.sha}
                </code>
                <button
                  className="ghost sm"
                  style={{ padding: '1px 6px', fontSize: 11 }}
                  onClick={() => handleCopySha(activeCommit.sha)}
                >
                  📋 复制
                </button>
              </div>

              {commitUrl && (
                <a
                  href={commitUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="ghost sm btn"
                  style={{ textDecoration: 'none', padding: '2px 8px', fontSize: 11 }}
                >
                  ↗ 在平台查看此 Commit
                </a>
              )}
            </div>

            <div style={{ fontSize: 13, color: 'var(--text)', whiteSpace: 'pre-wrap', lineHeight: 1.5 }}>
              <b>{activeCommit.subject}</b>
            </div>

            <div className="row small dim" style={{ gap: 12, fontSize: 11 }}>
              <span>作者: {activeCommit.author}</span>
              <span>时间: {activeCommit.date}</span>
            </div>
          </div>
        )}

        {/* 底部按钮栏 */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', paddingTop: 8, borderTop: '1px solid var(--border)' }}>
          <div className="row" style={{ gap: 8 }}>
            {onCreateIssue && activeCommit && (
              <button
                className="ghost sm"
                onClick={() => handleCreateIssueFromCommit(activeCommit)}
                title="基于选中的 Commit 创建新 Issue 并跟进"
              >
                📝 基于此提交新建 Issue…
              </button>
            )}
          </div>

          <button className="primary sm" onClick={onClose}>
            关闭
          </button>
        </div>
      </div>
    </div>
  );
};
