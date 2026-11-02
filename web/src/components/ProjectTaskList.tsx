import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { PageShell, fmtTime } from './common';
import { toast } from '../toast';
import type { ProjectTestPlan, ProjectTodo } from '../types';

type Kind = 'todos' | 'test-plans';
type StatusFilter = '' | 'open' | 'done';

type TaskItem = Pick<ProjectTodo, 'id' | 'title' | 'detail' | 'status' | 'sourceIssueId' | 'sourceKey' | 'createdAt'> &
  Partial<Pick<ProjectTestPlan, 'baseTitle' | 'repeatIndex'>>;

// ProjectTaskList backs both routed pages: the project TODO checklist and the
// persistent test plan registry (repeated plans are numbered by the server).
export function ProjectTaskList({ projectId, kind }: { projectId: number; kind: Kind }) {
  const qc = useQueryClient();
  const isPlan = kind === 'test-plans';
  const [searchText, setSearchText] = useState('');
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState<StatusFilter>('');
  const [title, setTitle] = useState('');
  const [detail, setDetail] = useState('');
  const [showDetail, setShowDetail] = useState(false);

  useEffect(() => {
    const timer = window.setTimeout(() => setQuery(searchText.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [searchText]);

  const listQ = useQuery({
    queryKey: isPlan ? qk.projectTestPlans(projectId, query, status) : qk.projectTodos(projectId, query, status),
    queryFn: () => (isPlan ? api.projectTestPlans(projectId, query, status) : api.projectTodos(projectId, query, status)),
  });
  const items: TaskItem[] = listQ.data ?? [];

  const invalidate = () => qc.invalidateQueries({ queryKey: [isPlan ? 'projectTestPlans' : 'projectTodos'] });

  const createMut = useMutation({
    mutationFn: () =>
      isPlan
        ? api.createProjectTestPlan(projectId, { title: title.trim(), detail: detail.trim() })
        : api.createProjectTodo(projectId, { title: title.trim(), detail: detail.trim() }),
    onSuccess: (item) => {
      invalidate();
      setTitle('');
      setDetail('');
      setShowDetail(false);
      toast.success(`已添加「${item.title}」`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const toggleMut = useMutation({
    mutationFn: (item: TaskItem) =>
      isPlan
        ? api.updateProjectTestPlan(projectId, item.id, { status: item.status === 'done' ? 'open' : 'done' })
        : api.updateProjectTodo(projectId, item.id, { status: item.status === 'done' ? 'open' : 'done' }),
    onSuccess: invalidate,
    onError: (e) => toast.error((e as Error).message),
  });

  const deleteMut = useMutation({
    mutationFn: (id: number) =>
      isPlan ? api.deleteProjectTestPlan(projectId, id) : api.deleteProjectTodo(projectId, id),
    onSuccess: invalidate,
    onError: (e) => toast.error((e as Error).message),
  });

  const filters: { id: StatusFilter; label: string }[] = [
    { id: '', label: '全部' },
    { id: 'open', label: '未完成' },
    { id: 'done', label: '已完成' },
  ];

  return (
    <PageShell
      title={isPlan ? '🧪 测试计划' : '☑️ TODO 列表'}
      headerActions={<span className="small muted">{items.length} 条</span>}
    >
      <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
        <input
          value={searchText}
          onChange={(e) => setSearchText(e.target.value)}
          placeholder={isPlan ? '搜索测试计划 / 详情 / 来源…' : '搜索 TODO / 详情 / 来源…'}
          aria-label="搜索"
          style={{ maxWidth: 360, flex: '1 1 240px' }}
        />
        {filters.map((f) => (
          <button
            key={f.id || 'all'}
            type="button"
            className={`sm ${status === f.id ? 'primary' : 'ghost'}`}
            onClick={() => setStatus(f.id)}
          >
            {f.label}
          </button>
        ))}
        {searchText && (
          <button type="button" className="ghost sm" onClick={() => setSearchText('')} title="清除搜索">
            ✕ 清除
          </button>
        )}
        <span className="grow" />
        <button
          type="button"
          className="sm ghost"
          onClick={() => setShowDetail((v) => !v)}
          title="添加时附带详细说明"
        >
          {showDetail ? '隐藏说明' : '＋ 说明'}
        </button>
      </div>

      <div className="card" style={{ padding: 12, display: 'flex', flexDirection: 'column', gap: 8 }}>
        <div className="row" style={{ gap: 8 }}>
          <input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder={isPlan ? '新增测试计划，例如「回归登录」' : '新增 TODO，例如「登录流程」'}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && title.trim() && !createMut.isPending) createMut.mutate();
            }}
            style={{ flex: 1 }}
          />
          <button
            type="button"
            className="primary"
            disabled={!title.trim() || createMut.isPending}
            onClick={() => createMut.mutate()}
          >
            {createMut.isPending ? '添加中…' : isPlan ? '添加测试计划' : '添加 TODO'}
          </button>
        </div>
        {showDetail && (
          <textarea
            rows={2}
            value={detail}
            onChange={(e) => setDetail(e.target.value)}
            placeholder="可选：验收点、步骤、备注"
          />
        )}
        {isPlan && (
          <div className="small dim">同类型测试计划会自动编号为 x1、x2…，BaseTitle 相同便于按类型搜索。</div>
        )}
      </div>

      {listQ.isLoading && <div className="empty">加载中…</div>}
      {listQ.error && <div className="ev-stderr">{(listQ.error as Error).message}</div>}
      {!listQ.isLoading && !listQ.error && items.length === 0 && (
        <div className="empty">
          {query || status ? '没有匹配的条目。' : isPlan ? '还没有测试计划。' : '还没有 TODO。'}
        </div>
      )}

      <div className="col" style={{ gap: 6 }}>
        {items.map((item) => (
          <div key={item.id} className="card" style={{ padding: '10px 12px' }}>
            <div className="row" style={{ gap: 10, alignItems: 'flex-start' }}>
              <input
                type="checkbox"
                checked={item.status === 'done'}
                disabled={toggleMut.isPending}
                title={item.status === 'done' ? '标记为未完成' : '标记为已完成'}
                onChange={() => toggleMut.mutate(item)}
                style={{ marginTop: 3, flex: 'none' }}
              />
              <div className="grow" style={{ minWidth: 0 }}>
                <div className="row" style={{ gap: 6, flexWrap: 'wrap' }}>
                  <b
                    style={{
                      textDecoration: item.status === 'done' ? 'line-through' : undefined,
                      color: item.status === 'done' ? 'var(--text-3)' : undefined,
                    }}
                  >
                    {item.title}
                  </b>
                  {isPlan && (item.repeatIndex ?? 0) > 0 && (
                    <span className="badge" title={`同类型第 ${item.repeatIndex} 次：${item.baseTitle ?? ''}`}>
                      x{item.repeatIndex}
                    </span>
                  )}
                  {item.sourceKey &&
                    (item.sourceIssueId ? (
                      <Link className="badge mono" to={`/issues/${item.sourceIssueId}`} title="来源 Issue">
                        {item.sourceKey}
                      </Link>
                    ) : (
                      <span className="badge mono" title="来源 Issue（已删除或归档）">
                        {item.sourceKey}
                      </span>
                    ))}
                  <span className="small dim" style={{ marginLeft: 'auto' }}>
                    {fmtTime(item.createdAt)}
                  </span>
                </div>
                {item.detail && (
                  <div className="small muted" style={{ marginTop: 4, whiteSpace: 'pre-wrap' }}>
                    {item.detail}
                  </div>
                )}
              </div>
              <button
                type="button"
                className="ghost sm"
                title="删除"
                disabled={deleteMut.isPending}
                onClick={() => {
                  if (window.confirm(`确定删除「${item.title}」？`)) deleteMut.mutate(item.id);
                }}
              >
                🗑
              </button>
            </div>
          </div>
        ))}
      </div>
    </PageShell>
  );
}
