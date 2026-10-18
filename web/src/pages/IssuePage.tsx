import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { DiffEditor } from '@monaco-editor/react';
import { api, qk } from '../api';
import {
  Crumbs,
  editorUrl,
  fmtTime,
  ISSUE_STATUS_LABEL,
  Markdown,
  Modal,
  RunStatusTag,
} from '../components/common';
import { EventStream } from '../components/EventStream';
import { languageOf } from '../monaco';
import { fetchRunEvents } from '../realtime';
import { toast } from '../toast';
import {
  ACTIVE_RUN_STATUSES,
  ISSUE_STATUSES,
  type IssueStatus,
  type RepoChanges,
  type Run,
  type SubmitResult,
} from '../types';

export default function IssuePage() {
  const id = Number(useParams().id);
  const qc = useQueryClient();

  const issueQ = useQuery({ queryKey: qk.issue(id), queryFn: () => api.issue(id) });
  const issue = issueQ.data;

  const projectQ = useQuery({
    queryKey: qk.project(issue?.projectId ?? 0),
    queryFn: () => api.project(issue!.projectId),
    enabled: !!issue,
  });
  const project = projectQ.data;

  const commentsQ = useQuery({ queryKey: qk.comments(id), queryFn: () => api.comments(id) });
  const runsQ = useQuery({ queryKey: qk.runs(id), queryFn: () => api.runs(id) });
  const agentsQ = useQuery({ queryKey: qk.agents, queryFn: api.agents });
  const healthQ = useQuery({ queryKey: qk.health, queryFn: api.health, staleTime: Infinity });

  const runs = runsQ.data ?? [];
  const [selectedRunId, setSelectedRunId] = useState<number | null>(null);

  useEffect(() => {
    if (selectedRunId === null && runs.length > 0) {
      setSelectedRunId(issue?.activeRunId ?? runs[0].id);
    } else if (selectedRunId !== null && runs.length > 0 && !runs.some((r) => r.id === selectedRunId)) {
      setSelectedRunId(runs[0].id);
    }
  }, [runs, issue?.activeRunId, selectedRunId]);

  const currentRun: Run | undefined = runs.find((r) => r.id === selectedRunId) ?? runs[0];

  const [tab, setTab] = useState<'log' | 'diff'>('log');
  const [agent, setAgent] = useState<string>('');
  const [prompt, setPrompt] = useState<string>('');
  const [newOpen, setNewOpen] = useState(false);

  useEffect(() => {
    if (!agent && agentsQ.data && agentsQ.data.length > 0) {
      const avail = agentsQ.data.find((a) => a.available) ?? agentsQ.data[0];
      setAgent(avail.name);
    }
  }, [agent, agentsQ.data]);

  const [commentText, setCommentText] = useState('');
  const [pubOpen, setPubOpen] = useState(false);
  const [pubRepoId, setPubRepoId] = useState<number>(0);

  useEffect(() => {
    if (!pubRepoId && project?.repos?.length) {
      const r = project.repos.find((x) => x.provider !== 'plain') ?? project.repos[0];
      setPubRepoId(r.id);
    }
  }, [pubRepoId, project?.repos]);

  const [editOpen, setEditOpen] = useState(false);
  const [editTitle, setEditTitle] = useState('');
  const [editBody, setEditBody] = useState('');

  const updateIssue = useMutation({
    mutationFn: (b: { title?: string; body?: string; status?: IssueStatus }) => api.updateIssue(id, b),
    onSuccess: (u) => {
      qc.setQueryData(qk.issue(id), u);
      qc.invalidateQueries({ queryKey: qk.issues(u.projectId) });
      setEditOpen(false);
    },
  });

  const addComment = useMutation({
    mutationFn: () => api.addComment(id, commentText.trim()),
    onSuccess: () => {
      setCommentText('');
      qc.invalidateQueries({ queryKey: qk.comments(id) });
    },
  });

  const publish = useMutation({
    mutationFn: () => api.publishIssue(id, pubRepoId),
    onSuccess: (u) => {
      qc.setQueryData(qk.issue(id), u);
      qc.invalidateQueries({ queryKey: qk.issues(u.projectId) });
      setPubOpen(false);
      toast.success(`已发布为 ${u.key}`);
    },
  });

  const startRun = useMutation({
    mutationFn: () => api.startRun(id, { agent, prompt: prompt.trim() || undefined }),
    onSuccess: (r) => {
      setSelectedRunId(r.id);
      setPrompt('');
      setNewOpen(false);
      setTab('log');
      qc.invalidateQueries({ queryKey: qk.runs(id) });
      qc.invalidateQueries({ queryKey: qk.issue(id) });
      toast.success(`Run #${r.id} 已启动`);
    },
  });

  const eventsQ = useQuery({
    queryKey: qk.events(currentRun?.id ?? 0),
    queryFn: fetchRunEvents(qc, currentRun?.id ?? 0),
    enabled: !!currentRun,
  });

  const [msgText, setMsgText] = useState('');
  const sendMsg = useMutation({
    mutationFn: () => api.sendMessage(currentRun!.id, msgText.trim()),
    onSuccess: () => {
      setMsgText('');
      setTab('log');
      qc.invalidateQueries({ queryKey: qk.run(currentRun!.id) });
      qc.invalidateQueries({ queryKey: qk.runs(id) });
    },
  });

  const cancelRun = useMutation({
    mutationFn: () => api.cancelRun(currentRun!.id),
    onSuccess: () => {
      toast.info('已发送取消请求');
    },
  });

  const isRunning = currentRun ? ACTIVE_RUN_STATUSES.includes(currentRun.status) : false;

  return (
    <div className="page-full">
      <Crumbs>
        <Link to="/">项目</Link>
        <span>/</span>
        {project ? <Link to={`/projects/${project.id}`}>{project.name}</Link> : <span>…</span>}
        <span>/</span>
        <span className="mono">{issue?.key ?? `#${id}`}</span>
      </Crumbs>

      <div className="issue-layout">
        {/* 左侧：Issue 信息与评论 */}
        <div className="issue-left">
          <div className="row" style={{ alignItems: 'flex-start' }}>
            <div className="grow">
              <div className="row small muted mb" style={{ gap: 6, marginBottom: 4 }}>
                <span className="mono">{issue?.key}</span>
                <span className="badge">{issue?.source === 'local' ? '本地' : '平台'}</span>
                {issue?.remoteUrl && (
                  <a href={issue.remoteUrl} target="_blank" rel="noreferrer" className="small">
                    打开原链接 ↗
                  </a>
                )}
                {issue?.awaiting && <span className="badge awaiting">待审批</span>}
              </div>
              <h1>{issue?.title ?? '加载中…'}</h1>
            </div>
            {issue && (
              <select
                value={issue.status}
                onChange={(e) => updateIssue.mutate({ status: e.target.value as IssueStatus })}
              >
                {ISSUE_STATUSES.map((s) => (
                  <option key={s} value={s}>
                    {ISSUE_STATUS_LABEL[s]}
                  </option>
                ))}
              </select>
            )}
          </div>

          <div className="row">
            <button
              className="sm"
              onClick={() => {
                if (!issue) return;
                setEditTitle(issue.title);
                setEditBody(issue.body);
                setEditOpen(true);
              }}
            >
              编辑
            </button>
            {issue?.source === 'local' && (project?.repos?.length ?? 0) > 0 && (
              <button className="sm" onClick={() => setPubOpen(true)}>
                发布到平台…
              </button>
            )}
          </div>

          <div className="panel">
            <div className="panel-b">
              {issue?.body ? <Markdown>{issue.body}</Markdown> : <span className="dim">无描述</span>}
            </div>
          </div>

          <div className="col" style={{ marginTop: 8 }}>
            <h3>评论 / 回顾记录 ({commentsQ.data?.length ?? 0})</h3>
            {(commentsQ.data ?? []).map((c) => (
              <div key={c.id} className={`comment ${c.author}`}>
                <div className="comment-h">
                  <b>{c.author === 'me' ? '我' : c.author === 'agent' ? 'Agent 总结' : '系统'}</b>
                  <span className="grow" />
                  <span>{fmtTime(c.createdAt)}</span>
                </div>
                <div className="comment-b">
                  <Markdown>{c.body}</Markdown>
                </div>
              </div>
            ))}
            <div className="col" style={{ gap: 6 }}>
              <textarea
                rows={3}
                placeholder="添加评论或备忘（支持 Markdown）…"
                value={commentText}
                onChange={(e) => setCommentText(e.target.value)}
              />
              <div className="row">
                <span className="grow" />
                <button
                  className="primary sm"
                  disabled={!commentText.trim() || addComment.isPending}
                  onClick={() => addComment.mutate()}
                >
                  发表评论
                </button>
              </div>
            </div>
          </div>
        </div>

        {/* 右侧：Run 与 Diff 审查 */}
        <div className="issue-right">
          <div className="run-bar">
            {runs.length > 0 ? (
              <>
                <span className="small muted">Run:</span>
                <select value={currentRun?.id ?? ''} onChange={(e) => setSelectedRunId(Number(e.target.value))}>
                  {runs.map((r) => (
                    <option key={r.id} value={r.id}>
                      #{r.id} · {r.agent} · {fmtTime(r.createdAt)}
                    </option>
                  ))}
                </select>
                {currentRun && <RunStatusTag status={currentRun.status} />}
                {currentRun?.branch && <span className="badge mono">{currentRun.branch}</span>}
                {currentRun?.workdir && healthQ.data?.editorUrlTemplate && (
                  <a
                    className="btn sm"
                    href={editorUrl(healthQ.data.editorUrlTemplate, currentRun.workdir) ?? '#'}
                    title={currentRun.workdir}
                  >
                    在编辑器打开工作区
                  </a>
                )}
              </>
            ) : (
              <span className="muted small">尚未运行过 Agent</span>
            )}
            <span className="grow" />
            {isRunning && (
              <button className="danger sm" onClick={() => cancelRun.mutate()} disabled={cancelRun.isPending}>
                取消运行
              </button>
            )}
            <button className="primary sm" onClick={() => setNewOpen(true)}>
              + 新启动 Run
            </button>
          </div>

          {currentRun ? (
            <>
              <div className="tabs">
                <button className={`tab${tab === 'log' ? ' active' : ''}`} onClick={() => setTab('log')}>
                  实时日志
                </button>
                <button className={`tab${tab === 'diff' ? ' active' : ''}`} onClick={() => setTab('diff')}>
                  代码变更 / 审查
                </button>
              </div>

              {tab === 'log' ? (
                <>
                  {currentRun.error && <div className="ev-stderr">{currentRun.error}</div>}
                  <EventStream runId={currentRun.id} events={eventsQ.data ?? []} />
                  <div className="run-input">
                    <textarea
                      rows={2}
                      placeholder={
                        isRunning
                          ? '向正在运行的 Agent 追加说明（Enter 发送，Shift+Enter 换行）…'
                          : '输入修改意见打回给 Agent 继续处理（将在原工作区恢复会话）…'
                      }
                      value={msgText}
                      onChange={(e) => setMsgText(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' && !e.shiftKey) {
                          e.preventDefault();
                          if (msgText.trim() && !sendMsg.isPending) sendMsg.mutate();
                        }
                      }}
                    />
                    <button
                      className="primary"
                      disabled={!msgText.trim() || sendMsg.isPending}
                      onClick={() => sendMsg.mutate()}
                    >
                      发送
                    </button>
                  </div>
                </>
              ) : (
                <ReviewPane
                  run={currentRun}
                  issueTitle={issue?.title ?? ''}
                  editorTemplate={healthQ.data?.editorUrlTemplate}
                />
              )}
            </>
          ) : (
            <div className="empty">
              点击右上角“+ 新启动 Run”把该 Issue 指派给 Agent（opencode / omp / codex）。
            </div>
          )}
        </div>
      </div>

      {/* 启动新 Run 弹窗 */}
      {newOpen && (
        <Modal
          title="启动新 Run"
          onClose={() => setNewOpen(false)}
          footer={
            <>
              <button onClick={() => setNewOpen(false)}>取消</button>
              <button
                className="primary"
                disabled={!agent || startRun.isPending}
                onClick={() => startRun.mutate()}
              >
                {startRun.isPending ? '启动中…' : '启动'}
              </button>
            </>
          }
        >
          <label className="field">
            选择 Agent
            <select value={agent} onChange={(e) => setAgent(e.target.value)}>
              {(agentsQ.data ?? []).map((a) => (
                <option key={a.name} value={a.name} disabled={!a.available}>
                  {a.name} ({a.command}){a.available ? '' : ' - 未安装'}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            附加说明（可选，会随 Issue 正文与各仓库信息一并传给 Agent）
            <textarea
              rows={5}
              placeholder="例如：只改 backend 仓库，先写单元测试…"
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
            />
          </label>
        </Modal>
      )}

      {/* 编辑 Issue 弹窗 */}
      {editOpen && (
        <Modal
          title="编辑 Issue"
          onClose={() => setEditOpen(false)}
          footer={
            <>
              <button onClick={() => setEditOpen(false)}>取消</button>
              <button
                className="primary"
                disabled={!editTitle.trim() || updateIssue.isPending}
                onClick={() => updateIssue.mutate({ title: editTitle.trim(), body: editBody })}
              >
                保存
              </button>
            </>
          }
        >
          <label className="field">
            标题
            <input value={editTitle} onChange={(e) => setEditTitle(e.target.value)} />
          </label>
          <label className="field">
            正文（Markdown）
            <textarea rows={12} className="mono" value={editBody} onChange={(e) => setEditBody(e.target.value)} />
          </label>
        </Modal>
      )}

      {/* 发布到平台弹窗 */}
      {pubOpen && (
        <Modal
          title="发布本地 Issue 到 Git 平台"
          onClose={() => setPubOpen(false)}
          footer={
            <>
              <button onClick={() => setPubOpen(false)}>取消</button>
              <button className="primary" disabled={!pubRepoId || publish.isPending} onClick={() => publish.mutate()}>
                {publish.isPending ? '发布中…' : '发布'}
              </button>
            </>
          }
        >
          <label className="field">
            目标仓库
            <select value={pubRepoId} onChange={(e) => setPubRepoId(Number(e.target.value))}>
              {(project?.repos ?? []).map((r) => (
                <option key={r.id} value={r.id} disabled={r.provider === 'plain'}>
                  {r.name} ({r.provider} {r.ownerRepo})
                </option>
              ))}
            </select>
          </label>
        </Modal>
      )}
    </div>
  );
}

// ---------- Diff 审查面板 ----------

function ReviewPane({
  run,
  issueTitle,
  editorTemplate,
}: {
  run: Run;
  issueTitle: string;
  editorTemplate?: string;
}) {
  const qc = useQueryClient();
  const changesQ = useQuery({ queryKey: qk.changes(run.id), queryFn: () => api.changes(run.id) });
  const groups: RepoChanges[] = changesQ.data ?? [];

  const [sel, setSel] = useState<{ repoId: number; repoName: string; path: string } | null>(null);

  useEffect(() => {
    if (sel) return;
    for (const g of groups) {
      if (g.files.length > 0) {
        setSel({ repoId: g.repoId, repoName: g.repoName, path: g.files[0].path });
        break;
      }
    }
  }, [groups, sel]);

  const baseQ = useQuery({
    queryKey: sel ? qk.file(run.id, sel.repoId, sel.path, 'base') : ['noop'],
    queryFn: () => api.file(run.id, sel!.repoId, sel!.path, 'base'),
    enabled: !!sel,
  });
  const workQ = useQuery({
    queryKey: sel ? qk.file(run.id, sel.repoId, sel.path, 'work') : ['noop'],
    queryFn: () => api.file(run.id, sel!.repoId, sel!.path, 'work'),
    enabled: !!sel,
  });

  const [draft, setDraft] = useState<string>('');
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(workQ.data?.content ?? '');
    setDirty(false);
  }, [workQ.data?.content, sel?.repoId, sel?.path]);

  const saveFile = useMutation({
    mutationFn: () => api.saveFile(run.id, sel!.repoId, sel!.path, draft),
    onSuccess: () => {
      setDirty(false);
      qc.setQueryData(qk.file(run.id, sel!.repoId, sel!.path, 'work'), { content: draft });
      qc.invalidateQueries({ queryKey: qk.changes(run.id) });
      toast.success('文件已保存');
    },
  });

  const [subMode, setSubMode] = useState<'pr' | 'push' | null>(null);
  const [subTitle, setSubTitle] = useState(issueTitle);
  const [subCommit, setSubCommit] = useState(issueTitle);
  const [subBody, setSubBody] = useState('');
  const [results, setResults] = useState<SubmitResult[] | null>(null);

  useEffect(() => {
    setSubTitle(issueTitle);
    setSubCommit(issueTitle);
  }, [issueTitle]);

  const submit = useMutation({
    mutationFn: (mode: 'pr' | 'push') =>
      api.submit(run.id, {
        mode,
        title: subTitle.trim(),
        commitMessage: subCommit.trim(),
        body: subBody,
      }),
    onSuccess: (r) => {
      setResults(r.results);
      setSubMode(null);
      qc.invalidateQueries({ queryKey: qk.run(run.id) });
      qc.invalidateQueries({ queryKey: qk.runs(run.issueId) });
      qc.invalidateQueries({ queryKey: qk.issue(run.issueId) });
      qc.invalidateQueries({ queryKey: qk.comments(run.issueId) });
      toast.success('提交完成');
    },
  });

  const discard = useMutation({
    mutationFn: () => api.discard(run.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.run(run.id) });
      qc.invalidateQueries({ queryKey: qk.runs(run.issueId) });
      qc.invalidateQueries({ queryKey: qk.changes(run.id) });
      toast.info('已丢弃工作区');
    },
  });

  const totalFiles = groups.reduce((n, g) => n + g.files.length, 0);
  const absPath = sel && run.workdir ? `${run.workdir}/${sel.repoName}/${sel.path}` : '';
  const openHref = absPath ? editorUrl(editorTemplate, absPath) : null;

  return (
    <div className="review">
      <div className="review-actions">
        <span className="small muted">共 {totalFiles} 个变更文件</span>
        <button className="sm" onClick={() => changesQ.refetch()}>
          ⟳ 刷新
        </button>
        <span className="grow" />
        <button className="sm" disabled={totalFiles === 0} onClick={() => setSubMode('push')}>
          仅提交并推送
        </button>
        <button className="primary sm" disabled={totalFiles === 0} onClick={() => setSubMode('pr')}>
          提交并提 PR
        </button>
        <button
          className="danger sm"
          onClick={() => confirm('确认丢弃本次 Run 的 worktree 和未推送分支？') && discard.mutate()}
        >
          丢弃
        </button>
      </div>

      {results && (
        <div className="results">
          {results.map((r) => (
            <div key={r.repoId} className="row">
              <b>{r.repoName}</b>
              <span className="mono dim">{r.branch}</span>
              {r.commit && <span className="mono muted">{r.commit.slice(0, 8)}</span>}
              {r.prUrl && (
                <a href={r.prUrl} target="_blank" rel="noreferrer">
                  {r.prUrl} ↗
                </a>
              )}
              {r.error && <span className="st-failed">{r.error}</span>}
            </div>
          ))}
        </div>
      )}

      <div className="review-body">
        <div className="file-tree">
          {groups.map((g) => (
            <div key={g.repoId}>
              <div className="file-group-h">
                <span className="grow">{g.repoName}</span>
                <span className="dim">{g.files.length}</span>
              </div>
              {g.files.length === 0 && <div className="small dim" style={{ padding: '2px 10px' }}>无变更</div>}
              {g.files.map((f) => {
                const active = sel?.repoId === g.repoId && sel?.path === f.path;
                return (
                  <div
                    key={f.path}
                    className={`file-item${active ? ' active' : ''}`}
                    onClick={() => setSel({ repoId: g.repoId, repoName: g.repoName, path: f.path })}
                  >
                    <span className={`fs fs-${f.status}`}>{f.status}</span>
                    <span className="grow ellipsis" title={f.path}>
                      {f.path}
                    </span>
                    {f.additions > 0 && <span className="add mono">+{f.additions}</span>}
                    {f.deletions > 0 && <span className="del mono">-{f.deletions}</span>}
                  </div>
                );
              })}
            </div>
          ))}
        </div>

        <div className="diff-pane">
          {sel ? (
            <>
              <div className="diff-h">
                <span className="mono grow ellipsis">
                  {sel.repoName}/{sel.path}
                </span>
                {dirty && <span className="badge">已修改未保存</span>}
                <button className="primary sm" disabled={!dirty || saveFile.isPending} onClick={() => saveFile.mutate()}>
                  保存修改
                </button>
                {openHref && (
                  <a className="btn sm" href={openHref}>
                    在编辑器中打开
                  </a>
                )}
              </div>
              <div className="diff-editor">
                <DiffEditor
                  theme="vs-dark"
                  language={languageOf(sel.path)}
                  original={baseQ.data?.content ?? ''}
                  modified={workQ.data?.content ?? ''}
                  options={{
                    readOnly: false,
                    originalEditable: false,
                    renderSideBySide: true,
                    minimap: { enabled: false },
                    fontSize: 12,
                    scrollBeyondLastLine: false,
                  }}
                  onMount={(editor) => {
                    const mod = editor.getModifiedEditor();
                    mod.onDidChangeModelContent(() => {
                      setDraft(mod.getValue());
                      setDirty(true);
                    });
                  }}
                />
              </div>
            </>
          ) : (
            <div className="empty">选择左侧文件查看 Diff 并可直接编辑</div>
          )}
        </div>
      </div>

      {subMode && (
        <Modal
          title={subMode === 'pr' ? '提交并创建 Pull Request' : '提交并推送分支'}
          onClose={() => setSubMode(null)}
          footer={
            <>
              <button onClick={() => setSubMode(null)}>取消</button>
              <button
                className="primary"
                disabled={!subCommit.trim() || submit.isPending}
                onClick={() => submit.mutate(subMode)}
              >
                {submit.isPending ? '处理中…' : '确认'}
              </button>
            </>
          }
        >
          <label className="field">
            Commit Message
            <input value={subCommit} onChange={(e) => setSubCommit(e.target.value)} />
          </label>
          {subMode === 'pr' && (
            <>
              <label className="field">
                PR 标题
                <input value={subTitle} onChange={(e) => setSubTitle(e.target.value)} />
              </label>
              <label className="field">
                PR 描述（可选，留空将自动附上 Issue 链接与跨仓库关联）
                <textarea rows={6} value={subBody} onChange={(e) => setSubBody(e.target.value)} />
              </label>
            </>
          )}
        </Modal>
      )}
    </div>
  );
}
