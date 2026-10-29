import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { fmtTime, Modal } from './common';
import { toast } from '../toast';
import type { Repo } from '../types';

export function BranchCleanupModal({
  repos,
  onClose,
}: {
  repos: Repo[];
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const [repoId, setRepoId] = useState(repos[0]?.id ?? 0);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [progress, setProgress] = useState<{ done: number; total: number; current: string } | null>(null);

  const branchesQ = useQuery({
    queryKey: qk.mergedBranches(repoId),
    queryFn: () => api.mergedBranches(repoId, true),
    enabled: repoId > 0,
    retry: false,
  });
  const branches = branchesQ.data ?? [];

  useEffect(() => {
    setSelected(new Set());
  }, [repoId, branchesQ.dataUpdatedAt]);

  const deleteMut = useMutation({
    mutationFn: async (names: string[]) => {
      let done = 0;
      for (const name of names) {
        setProgress({ done, total: names.length, current: name });
        await api.deleteMergedBranches(repoId, [name]);
        done++;
        setProgress({ done, total: names.length, current: name });
        setSelected((prev) => {
          const next = new Set(prev);
          next.delete(name);
          return next;
        });
        qc.setQueryData(qk.mergedBranches(repoId), (old: typeof branches | undefined) =>
          (old ?? []).filter((branch) => branch.name !== name),
        );
      }
      return done;
    },
    onSuccess: (count) => {
      qc.invalidateQueries({ queryKey: qk.mergedBranches(repoId) });
      qc.invalidateQueries({ queryKey: qk.repoBranches(repoId) });
      qc.invalidateQueries({ queryKey: qk.repoCommits(repoId, '') });
      toast.success(`已删除 ${count} 个分支`);
      setSelected(new Set());
      setProgress(null);
    },
    onError: (e) => {
      qc.invalidateQueries({ queryKey: qk.mergedBranches(repoId) });
      setProgress(null);
      toast.error(`删除失败：${(e as Error).message}`);
    },
  });

  const allSelected = branches.length > 0 && branches.every((branch) => selected.has(branch.name));
  const toggleAll = () => {
    setSelected(allSelected ? new Set() : new Set(branches.map((branch) => branch.name)));
  };
  const toggle = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const selectedCount = selected.size;
  const deleteSelected = () => {
    if (selectedCount === 0) return;
    if (!confirm(`确认删除选中的 ${selectedCount} 个已合并远程分支？\n此操作会直接在 origin 删除分支，不可撤销。`)) return;
    deleteMut.mutate([...selected]);
  };

  const body = useMemo(() => {
    if (!repoId) return <div className="empty">请选择仓库</div>;
    if (branchesQ.isLoading) return <div className="empty">正在检查已合并分支…</div>;
    if (branchesQ.isError) {
      return (
        <div className="empty" style={{ color: 'var(--orange)' }}>
          无法读取分支：{(branchesQ.error as Error).message}
          <br />
          <span className="small">需要该仓库配置 Git 身份（user.email / user.name），且能访问 origin。</span>
        </div>
      );
    }
    if (branches.length === 0) {
      return <div className="empty">没有找到「已合并且尚未删除」的、由你提交的远程分支。</div>;
    }
    return (
      <div className="col" style={{ gap: 0 }}>
        <div className="row small muted" style={{ padding: '6px 8px', borderBottom: '1px solid var(--border)' }}>
          <input type="checkbox" checked={allSelected} onChange={toggleAll} />
          <span>全选</span>
          <span className="grow" />
          <span>显示 {branches.length} 个分支</span>
        </div>
        <div style={{ overflowY: 'auto', maxHeight: '52vh' }}>
          {branches.map((branch) => (
            <label
              key={branch.name}
              className="row"
              style={{ gap: 8, padding: '8px', borderBottom: '1px solid var(--border)', cursor: 'pointer', alignItems: 'flex-start' }}
            >
              <input type="checkbox" checked={selected.has(branch.name)} onChange={() => toggle(branch.name)} />
              <div className="col grow" style={{ gap: 2, minWidth: 0 }}>
                <div className="row" style={{ gap: 6 }}>
                  <code className="mono small">{branch.name}</code>
                  <span className="grow" />
                  <span className="small dim">{fmtTime(branch.date)}</span>
                </div>
                <div className="small muted ellipsis">{branch.subject || '(无提交说明)'}</div>
                <div className="small dim">
                  {branch.author} · {branch.email} · {branch.sha.slice(0, 8)}
                </div>
              </div>
            </label>
          ))}
        </div>
      </div>
    );
  }, [repoId, branchesQ.isLoading, branchesQ.isError, branchesQ.error, branches, selected, allSelected]);

  return (
    <Modal
      title="🧹 清理已合并分支"
      size="xl"
      onClose={deleteMut.isPending ? () => {} : onClose}
      style={{ height: 'min(760px, 90vh)' }}
      footer={
        <button onClick={onClose} disabled={deleteMut.isPending}>
          {deleteMut.isPending ? '删除进行中…' : '关闭'}
        </button>
      }
    >
      <div className="col" style={{ gap: 10, minHeight: 0 }}>
      <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
        <label className="field grow" style={{ margin: 0, minWidth: 240 }}>
          仓库
          <select value={repoId} onChange={(e) => setRepoId(Number(e.target.value))}>
            {repos.map((repo) => (
              <option key={repo.id} value={repo.id}>
                {repo.name} · {repo.provider} · {repo.ownerRepo || repo.remoteUrl}
              </option>
            ))}
          </select>
        </label>
        <button
          className="danger"
          disabled={deleteMut.isPending || selectedCount === 0}
          onClick={deleteSelected}
          style={{ alignSelf: 'flex-end' }}
        >
          {deleteMut.isPending ? '删除中…' : `删除选中 (${selectedCount})`}
        </button>
      </div>
      <div className="small dim">
        只列出「已经合并到 {repos.find((repo) => repo.id === repoId)?.defaultBranch || '默认分支'}」且提交者是你的远程分支。
      </div>
      {progress && (
        <div className="col" style={{ gap: 4 }}>
          <div className="small">
            正在删除 {Math.min(progress.done + 1, progress.total)}/{progress.total}：
            <code className="mono">{progress.current}</code>
          </div>
          <div className="status-progress" style={{ width: '100%', height: 6 }}>
            <span style={{ width: `${(progress.done / progress.total) * 100}%` }} />
          </div>
        </div>
      )}
      {body}
      </div>
    </Modal>
  );
}
