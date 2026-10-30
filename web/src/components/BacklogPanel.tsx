import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { fmtTime } from './common';
import { toast } from '../toast';
import type { Issue } from '../types';

const PRIORITY: Record<string, string> = { low: '低', normal: '普通', high: '高', urgent: '紧急' };

export function BacklogPanel({ projectId, issues }: { projectId: number; issues: Issue[] }) {
  const qc = useQueryClient();
  const normalize = (list: Issue[]) =>
    list
      .filter((issue) => issue.status === 'backlog')
      .sort((a, b) => (a.backlogRank ?? 0) - (b.backlogRank ?? 0) || b.id - a.id);
  const [order, setOrder] = useState<Issue[]>(() => normalize(issues));
  const [dragging, setDragging] = useState<number | null>(null);

  useEffect(() => {
    setOrder(normalize(issues));
  }, [issues]);

  const reorder = useMutation({
    mutationFn: (ids: number[]) => api.reorderBacklog(projectId, ids),
    onError: (e) => {
      toast.error((e as Error).message);
      setOrder(normalize(issues));
    },
  });
  const start = useMutation({
    mutationFn: (id: number) => api.updateIssue(id, { status: 'todo' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.issues(projectId) });
      toast.success('已移到待办，可拖入执行中');
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const dropOn = (targetId: number) => {
    if (!dragging || dragging === targetId) return;
    const from = order.findIndex((issue) => issue.id === dragging);
    const to = order.findIndex((issue) => issue.id === targetId);
    if (from < 0 || to < 0) return;
    const next = [...order];
    const [moved] = next.splice(from, 1);
    next.splice(to, 0, moved);
    setOrder(next);
    setDragging(null);
    qc.setQueryData<Issue[]>(qk.issues(projectId), (old) =>
      (old ?? []).map((issue) => {
        const idx = next.findIndex((item) => item.id === issue.id);
        return idx >= 0 ? { ...issue, backlogRank: idx } : issue;
      }),
    );
    reorder.mutate(next.map((issue) => issue.id));
  };

  return (
    <div className="panel" style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      <div className="panel-h">
        <h3 className="grow">📋 Backlog（{order.length}）</h3>
      </div>
      <div style={{ overflowY: 'auto', padding: 10 }}>
        {order.length === 0 ? (
          <div className="empty small">Backlog 为空。把 Issue 状态改为 Backlog 后会出现在这里。</div>
        ) : (
          <div className="col" style={{ gap: 6 }}>
            {order.map((issue) => (
              <div
                key={issue.id}
                draggable
                onDragStart={() => setDragging(issue.id)}
                onDragEnd={() => setDragging(null)}
                onDragOver={(e) => e.preventDefault()}
                onDrop={() => dropOn(issue.id)}
                className={`issue-card${dragging === issue.id ? ' dragging' : ''}`}
                style={{ cursor: 'grab', gap: 2 }}
              >
                <div className="row" style={{ gap: 6 }}>
                  <span className="mono small dim">#{issue.id}</span>
                  <span className={`badge priority-${issue.priority || 'normal'}`}>{PRIORITY[issue.priority] ?? '普通'}</span>
                  <Link to={`/issues/${issue.id}`} className="grow issue-card-title" style={{ fontSize: 13 }}>{issue.title}</Link>
                  <span className="small dim">{fmtTime(issue.updatedAt)}</span>
                  <button className="sm" disabled={start.isPending} onClick={() => start.mutate(issue.id)}>移到 Todo</button>
                </div>
                {(issue.labels?.length ?? 0) > 0 && (
                  <div className="row" style={{ gap: 4, flexWrap: 'wrap' }}>
                    {issue.labels.slice(0, 5).map((label) => <span className="badge" key={label}>{label}</span>)}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
