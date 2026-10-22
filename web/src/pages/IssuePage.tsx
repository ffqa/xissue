import { useEffect, useRef, useState } from 'react';
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
import { AgentManagerModal } from '../components/AgentManagerModal';
import { formatChangeRecord } from '../components/CherryPickModal';
import { languageOf } from '../monaco';
import { fetchRunEvents } from '../realtime';
import { useTheme } from '../theme';
import { copyText } from '../clipboard';
import { toast } from '../toast';
import {
  ACTIVE_RUN_STATUSES,
  ISSUE_STATUSES,
  type DevMode,
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
  const [selectedProvider, setSelectedProvider] = useState<string>('');
  const [selectedModel, setSelectedModel] = useState<string>('');
  const [customModel, setCustomModel] = useState<string>('');
  const [prompt, setPrompt] = useState<string>('');
  const [newOpen, setNewOpen] = useState(false);
  const [agentMgrOpen, setAgentMgrOpen] = useState(false);
  const [archiveModalData, setArchiveModalData] = useState<{ dir: string; analysis: string; events: string } | null>(null);
  const [archiveTab, setArchiveTab] = useState<'analysis' | 'events'>('analysis');

  useEffect(() => {
    if (!agent && agentsQ.data && agentsQ.data.length > 0) {
      const avail = agentsQ.data.find((a) => a.available) ?? agentsQ.data[0];
      setAgent(avail.name);
    }
  }, [agent, agentsQ.data]);

  const modelsQ = useQuery({
    queryKey: qk.agentModels(agent),
    queryFn: () => api.agentModels(agent),
    enabled: !!agent && newOpen,
    staleTime: 5 * 60 * 1000,
  });

  useEffect(() => {
    if (!modelsQ.data) return;
    const { currentModel, customModels, providers, models } = modelsQ.data;
    if (
      selectedProvider &&
      (selectedProvider === '__custom__' ||
        selectedProvider === '__bound__' ||
        providers.includes(selectedProvider))
    ) {
      return;
    }
    if ((customModels?.length ?? 0) > 0) {
      setSelectedProvider('__bound__');
      if (currentModel && customModels.includes(currentModel)) {
        setSelectedModel(currentModel);
      } else {
        setSelectedModel(customModels[0]);
      }
      return;
    }
    if (currentModel && currentModel.includes('/')) {
      const [prov] = currentModel.split('/');
      if (providers.includes(prov)) {
        setSelectedProvider(prov);
        setSelectedModel(currentModel);
        return;
      }
    }
    if (providers.length > 0) {
      const p0 = providers[0];
      setSelectedProvider(p0);
      const firstModel = models.find((m) => m.provider === p0);
      setSelectedModel(firstModel ? firstModel.value : '');
    } else {
      setSelectedProvider('');
      setSelectedModel('');
    }
  }, [modelsQ.data, selectedProvider]);

  const handleAgentChange = (nextAgent: string) => {
    setAgent(nextAgent);
    setSelectedProvider('');
    setSelectedModel('');
    setCustomModel('');
  };

  const handleProviderChange = (nextProv: string) => {
    setSelectedProvider(nextProv);
    if (nextProv === '' || nextProv === '__custom__') {
      setSelectedModel('');
      return;
    }
    if (nextProv === '__bound__') {
      const bound = modelsQ.data?.customModels ?? [];
      const cur = modelsQ.data?.currentModel ?? '';
      setSelectedModel(cur && bound.includes(cur) ? cur : (bound[0] ?? ''));
      return;
    }
    const list = (modelsQ.data?.models ?? []).filter((m) => m.provider === nextProv);
    const cur = modelsQ.data?.currentModel ?? '';
    if (cur && list.some((m) => m.value === cur)) {
      setSelectedModel(cur);
    } else {
      setSelectedModel(list[0]?.value ?? '');
    }
  };

  const effectiveModel =
    selectedProvider === '__custom__'
      ? customModel.trim()
      : selectedProvider === ''
        ? ''
        : selectedModel.trim();

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
    mutationFn: () =>
      api.startRun(id, {
        agent,
        model: effectiveModel || undefined,
        prompt: prompt.trim() || undefined,
      }),
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

  const deleteRun = useMutation({
    mutationFn: (runId: number) => api.deleteRun(runId),
    onSuccess: (_, runId) => {
      toast.success(`Run #${runId} 及其工作区已删除`);
      if (selectedRunId === runId) {
        setSelectedRunId(null);
      }
      qc.invalidateQueries({ queryKey: qk.runs(id) });
      qc.invalidateQueries({ queryKey: qk.issue(id) });
    },
    onError: (e) => toast.error((e as Error).message),
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
                onChange={(e) => {
                  const next = e.target.value as IssueStatus;
                  if (next === 'running' && !isRunning) {
                    startRun.mutate();
                  } else {
                    updateIssue.mutate({ status: next });
                  }
                }}
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
                      #{r.id} · {r.agent}{r.model ? ` (${r.model})` : ''} · {fmtTime(r.createdAt)}
                    </option>
                  ))}
                </select>
                {currentRun && <RunStatusTag status={currentRun.status} />}
                {currentRun?.model && <span className="badge mono" title="使用的模型">{currentRun.model}</span>}
                {currentRun?.branch && (
                  <span className="badge mono">
                    {currentRun.branch}
                    {currentRun.targetBranch ? ` : ${currentRun.targetBranch}` : ''}
                  </span>
                )}
                {currentRun?.prUrl && (
                  <a
                    href={currentRun.prUrl}
                    target="_blank"
                    rel="noreferrer"
                    className={`badge ${
                      currentRun.prState === 'merged'
                        ? 'pr-merged'
                        : currentRun.prState === 'closed'
                          ? 'pr-closed'
                          : 'pr-open'
                    }`}
                  >
                    {currentRun.prState === 'merged'
                      ? '🟣 PR Merged ↗'
                      : currentRun.prState === 'closed'
                        ? '🔴 PR Closed ↗'
                        : '🟢 PR Open ↗'}
                  </a>
                )}
                {currentRun?.changeRecord && (
                  <button
                    type="button"
                    className="sm ghost"
                    title="复制标准改动记录到剪贴板"
                    onClick={() => {
                      void copyText(currentRun.changeRecord).then((ok) =>
                        ok ? toast.success('已复制标准改动记录到剪贴板') : toast.error('复制失败，请手动选择文本')
                      );
                    }}
                  >
                    📋 复制改动记录
                  </button>
                )}
                {currentRun && (
                  <button
                    type="button"
                    className="sm ghost"
                    title="查看保存在 ~/.xissue/issue/ 下的纯文本复盘与执行日志"
                    onClick={async () => {
                      try {
                        const arch = await api.runArchive(currentRun.id);
                        setArchiveModalData(arch);
                      } catch (e) {
                        toast.error((e as Error).message);
                      }
                    }}
                  >
                    📁 文本复盘归档
                  </button>
                )}
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
            {currentRun && !isRunning && (
              <button
                className="danger sm"
                disabled={deleteRun.isPending}
                onClick={() =>
                  confirm(`确认删除 Run #${currentRun.id} 及其本地工作区目录？`) && deleteRun.mutate(currentRun.id)
                }
              >
                🗑 删除此工作区
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
                  key={currentRun.id}
                  run={currentRun}
                  isRunning={isRunning}
                  issueTitle={issue?.title ?? ''}
                  editorTemplate={healthQ.data?.editorUrlTemplate}
                  targetBranch={project?.targetBranch}
                  devMode={project?.devMode}
                  autoPrOnConfirm={project?.autoPrOnConfirm}
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
              <button type="button" onClick={() => setAgentMgrOpen(true)}>
                ⚙ 配置 Agent 与模型…
              </button>
              <span className="grow" />
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
            <select value={agent} onChange={(e) => handleAgentChange(e.target.value)}>
              {(agentsQ.data ?? []).map((a) => (
                <option key={a.name} value={a.name} disabled={!a.available}>
                  {a.name} ({a.command}){a.defaultModel ? ` · 默认: ${a.defaultModel}` : ''}
                  {a.available ? '' : ' - 未安装'}
                </option>
              ))}
            </select>
          </label>

          <div className="row" style={{ gap: 10, alignItems: 'flex-end' }}>
            <label className="field" style={{ flex: 1 }}>
              Provider（模型服务商）
              <select
                value={selectedProvider}
                onChange={(e) => handleProviderChange(e.target.value)}
                disabled={modelsQ.isLoading}
              >
                <option value="">
                  默认（{modelsQ.data?.currentModel || '使用 Agent 默认配置'}）
                </option>
                {(modelsQ.data?.customModels?.length ?? 0) > 0 && (
                  <option value="__bound__">★ 已绑定常用模型 ({modelsQ.data!.customModels.length})</option>
                )}
                {(modelsQ.data?.providers ?? []).map((p) => (
                  <option key={p} value={p}>
                    {p}
                  </option>
                ))}
                <option value="__custom__">自定义输入 (provider/model)…</option>
              </select>
            </label>

            {selectedProvider === '__bound__' && (
              <label className="field" style={{ flex: 2 }}>
                选择已绑定模型
                <select value={selectedModel} onChange={(e) => setSelectedModel(e.target.value)}>
                  {(modelsQ.data?.customModels ?? []).map((m) => (
                    <option key={m} value={m}>
                      {m === modelsQ.data?.currentModel ? `${m} (当前默认)` : m}
                    </option>
                  ))}
                </select>
              </label>
            )}

            {selectedProvider && selectedProvider !== '__custom__' && selectedProvider !== '__bound__' && (
              <label className="field" style={{ flex: 2 }}>
                选择模型 (Model)
                <select value={selectedModel} onChange={(e) => setSelectedModel(e.target.value)}>
                  {(modelsQ.data?.models ?? [])
                    .filter((m) => m.provider === selectedProvider)
                    .map((m) => (
                      <option key={m.value} value={m.value}>
                        {m.value === modelsQ.data?.currentModel ? `${m.value} (当前默认)` : m.value}
                      </option>
                    ))}
                </select>
              </label>
            )}

            <button
              type="button"
              className="sm"
              title="重新从 Agent 探测可用 Provider 和模型列表"
              disabled={modelsQ.isFetching || !agent}
              onClick={async () => {
                try {
                  const fresh = await api.agentModels(agent, true);
                  qc.setQueryData(qk.agentModels(agent), fresh);
                  if (fresh.probeError && fresh.models.length === 0) {
                    toast.info(fresh.probeError);
                  } else {
                    toast.info(`已刷新 ${agent} 模型列表（共 ${fresh.models.length} 个）`);
                  }
                } catch (e) {
                  toast.error(`刷新模型失败: ${(e as Error).message}`);
                }
              }}
            >
              {modelsQ.isFetching ? '加载中…' : '↻ 刷新模型'}
            </button>
          </div>

          {modelsQ.data?.probeError && modelsQ.data?.models.length === 0 && (
            <div
              style={{
                padding: '8px 12px',
                background: 'rgba(234, 179, 8, 0.1)',
                border: '1px solid rgba(234, 179, 8, 0.3)',
                borderRadius: 6,
                fontSize: 12,
                color: 'var(--yellow)',
                lineHeight: 1.5,
                marginTop: -4,
              }}
            >
              ⚠️ <b>探测提示</b>：{modelsQ.data.probeError}
            </div>
          )}

          {selectedProvider === '__custom__' && (
            <label className="field">
              自定义 Provider / 模型标识
              <input
                className="mono"
                placeholder="例如：openai/gpt-5.4 或 google/gemini-2.5-pro"
                value={customModel}
                onChange={(e) => setCustomModel(e.target.value)}
              />
            </label>
          )}

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

      {agentMgrOpen && <AgentManagerModal onClose={() => setAgentMgrOpen(false)} />}

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

      {/* 文本复盘归档弹窗 */}
      {archiveModalData && (
        <Modal
          title={`📁 文本复盘归档 (Run #${currentRun?.id})`}
          size="lg"
          onClose={() => setArchiveModalData(null)}
          footer={
            <>
              <button
                type="button"
                onClick={() => {
                  void copyText(archiveModalData.dir).then((ok) =>
                    ok ? toast.success('已复制归档目录路径到剪贴板') : toast.error('复制失败，请手动选择路径')
                  );
                }}
              >
                📋 复制归档路径
              </button>
              <button
                type="button"
                onClick={() => {
                  const text = archiveTab === 'analysis' ? archiveModalData.analysis : archiveModalData.events;
                  void copyText(text).then((ok) =>
                    ok ? toast.success('已复制文本内容到剪贴板') : toast.error('复制失败，请手动选择文本')
                  );
                }}
              >
                📋 复制当前文本
              </button>
              <span className="grow" />
              <button className="primary" onClick={() => setArchiveModalData(null)}>
                关闭
              </button>
            </>
          }
        >
          <div className="col" style={{ gap: 10 }}>
            <div className="row small muted" style={{ wordBreak: 'break-all' }}>
              <span>本地归档目录：</span>
              <code className="mono" style={{ color: 'var(--accent)' }}>{archiveModalData.dir}</code>
            </div>
            <div className="tabs" style={{ background: 'transparent', padding: 0 }}>
              <button
                type="button"
                className={`tab${archiveTab === 'analysis' ? ' active' : ''}`}
                onClick={() => setArchiveTab('analysis')}
              >
                📄 分析摘要 (analysis.md)
              </button>
              <button
                type="button"
                className={`tab${archiveTab === 'events' ? ' active' : ''}`}
                onClick={() => setArchiveTab('events')}
              >
                📜 完整事件流 (events.log)
              </button>
            </div>
            <div
              style={{
                maxHeight: '60vh',
                overflow: 'auto',
                background: 'var(--bg-2)',
                border: '1px solid var(--border)',
                borderRadius: 6,
                padding: 14,
              }}
            >
              {archiveTab === 'analysis' ? (
                <Markdown>{archiveModalData.analysis || '暂无分析记录'}</Markdown>
              ) : (
                <pre className="mono small" style={{ margin: 0, whiteSpace: 'pre-wrap' }}>
                  {archiveModalData.events || '暂无事件日志'}
                </pre>
              )}
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}

// ---------- Diff 审查面板 ----------

function ReviewPane({
  run,
  isRunning,
  issueTitle,
  editorTemplate,
  devMode = 'single_main',
  targetBranch = 'main',
  autoPrOnConfirm = true,
}: {
  run: Run;
  isRunning: boolean;
  issueTitle: string;
  editorTemplate?: string;
  devMode?: DevMode;
  targetBranch?: string;
  autoPrOnConfirm?: boolean;
}) {
  const qc = useQueryClient();
  const [theme] = useTheme();
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
    refetchOnWindowFocus: false,
  });
  const workQ = useQuery({
    queryKey: sel ? qk.file(run.id, sel.repoId, sel.path, 'work') : ['noop'],
    queryFn: () => api.file(run.id, sel!.repoId, sel!.path, 'work'),
    enabled: !!sel,
    refetchOnWindowFocus: false,
  });

  const [draft, setDraft] = useState<string>('');
  const [dirty, setDirty] = useState(false);
  const listenerRef = useRef<{ dispose: () => void } | null>(null);
  const lastSelRef = useRef<string>('');

  useEffect(() => {
    return () => {
      listenerRef.current?.dispose();
    };
  }, []);

  useEffect(() => {
    const curKey = `${sel?.repoId}:${sel?.path}`;
    const fileSwitched = lastSelRef.current !== curKey;
    lastSelRef.current = curKey;

    if (fileSwitched || !dirty) {
      setDraft(workQ.data?.content ?? '');
      setDirty(false);
    }
  }, [workQ.data?.content, sel?.repoId, sel?.path]);

  const saveFile = useMutation({
    mutationFn: (args: { repoId: number; path: string; content: string }) =>
      api.saveFile(run.id, args.repoId, args.path, args.content),
    onSuccess: (_, args) => {
      setDirty(false);
      qc.setQueryData(qk.file(run.id, args.repoId, args.path, 'work'), { content: args.content });
      qc.invalidateQueries({ queryKey: qk.changes(run.id) });
      toast.success('文件已保存');
    },
  });

  const [subMode, setSubMode] = useState<'pr' | 'push' | 'merge_local' | null>(null);
  const [subTitle, setSubTitle] = useState(issueTitle);
  const [subCommit, setSubCommit] = useState(issueTitle);
  const [subTargetBranch, setSubTargetBranch] = useState(
    run.targetBranch || targetBranch || (devMode === 'main_dev' ? 'dev' : 'main'),
  );
  const [subItemsText, setSubItemsText] = useState('');
  const [subRemark, setSubRemark] = useState('');
  const [results, setResults] = useState<SubmitResult[] | null>(null);

  useEffect(() => {
    if (run.targetBranch) {
      setSubTargetBranch(run.targetBranch);
    } else if (targetBranch) {
      setSubTargetBranch(targetBranch);
    } else {
      setSubTargetBranch(devMode === 'main_dev' ? 'dev' : 'main');
    }
  }, [run.targetBranch, targetBranch, devMode]);

  useEffect(() => {
    setSubTitle(issueTitle);
    setSubCommit(issueTitle);
  }, [issueTitle]);

  // Auto-populate concise change items when opening submit modal
  const openSubmitModal = (mode: 'pr' | 'push' | 'merge_local', branch?: string) => {
    if (branch) {
      setSubTargetBranch(branch);
    }
    if (!subItemsText.trim()) {
      const lines: string[] = [];
      if (issueTitle.trim()) lines.push(issueTitle.trim());
      for (const g of groups) {
        for (const f of g.files.slice(0, 6)) {
          const act = f.status === 'A' ? '新增' : f.status === 'D' ? '删除' : '修改';
          lines.push(`${act} ${f.path} (+${f.additions}/-${f.deletions})`);
        }
      }
      setSubItemsText(lines.join('\n'));
    }
    setSubMode(mode);
  };

  const submit = useMutation({
    mutationFn: (mode: 'pr' | 'push' | 'merge_local') =>
      api.submit(run.id, {
        mode,
        title: subTitle.trim(),
        commitMessage: subCommit.trim(),
        targetBranch: subTargetBranch.trim() || undefined,
        changeItems: subItemsText
          .split('\n')
          .map((s) => s.trim())
          .filter(Boolean),
        remark: subRemark.trim(),
      }),
    onSuccess: (r) => {
      setResults(r.results);
      setSubMode(null);
      qc.invalidateQueries({ queryKey: qk.run(run.id) });
      qc.invalidateQueries({ queryKey: qk.runs(run.issueId) });
      qc.invalidateQueries({ queryKey: qk.issue(run.issueId) });
      qc.invalidateQueries({ queryKey: qk.comments(run.issueId) });
      toast.success('提交完成，已生成标准改动记录');
    },
    onError: (e) => toast.error((e as Error).message),
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

  const previewRecord = formatChangeRecord(
    subItemsText.split('\n'),
    run.branch,
    subTargetBranch || (devMode === 'main_dev' ? 'dev' : 'main'),
    subMode === 'merge_local'
      ? `(已直接合入本地 ${subTargetBranch || 'main'})`
      : subMode === 'pr'
      ? '(创建 PR 后自动填入链接)'
      : '无 (仅推送分支)',
    subRemark,
  );

  return (
    <div className="review">
      <div className="review-actions">
        <span className="small muted">共 {totalFiles} 个变更文件</span>
        <button className="sm" onClick={() => changesQ.refetch()}>
          ⟳ 刷新
        </button>
        {totalFiles > 0 && !run.prUrl && !run.changeRecord && (
          <div
            className="row"
            style={{
              gap: 6,
              background: 'var(--bg-2)',
              padding: '2px 8px',
              borderRadius: 6,
              border: '1px solid var(--border)',
            }}
            title={
              devMode === 'main_dev'
                ? 'main+dev 模式：PR 将默认提交到 dev，避免直接合入 main 触发自动打包'
                : '单一 main 模式：改动将直接合入本地 main 分支'
            }
          >
            <span className="small muted">模式:</span>
            <code className="mono small" style={{ color: 'var(--accent)' }}>
              {devMode === 'main_dev' ? `main+dev (PR → ${subTargetBranch || 'dev'})` : '单一 main (本地合入)'}
            </code>
          </div>
        )}
        <span className="grow" />

        {devMode === 'single_main' ? (
          <>
            <button
              className="primary sm"
              disabled={totalFiles === 0 || isRunning || submit.isPending}
              onClick={() => openSubmitModal('merge_local', 'main')}
              title="改完确认后直接合入本地 main 分支并标记完成"
            >
              {submit.isPending ? '合入中…' : '⚡ 确认并合入本地 main'}
            </button>
            <button
              className="sm ghost"
              disabled={totalFiles === 0 || isRunning || submit.isPending}
              onClick={() => openSubmitModal('pr', 'main')}
              title="在远端平台发起 Pull Request 到 main 分支"
            >
              提 PR 到 main
            </button>
          </>
        ) : (
          <>
            <button
              className="primary sm"
              disabled={totalFiles === 0 || isRunning || submit.isPending}
              onClick={() => openSubmitModal('pr', subTargetBranch || 'dev')}
              title={`改完确认后自动创建 PR 到 ${subTargetBranch || 'dev'}，避免直接合入 main 触发自动打包`}
            >
              {submit.isPending
                ? '创建中…'
                : autoPrOnConfirm
                ? `🚀 确认改动并提 PR (→ ${subTargetBranch || 'dev'})`
                : `提 PR (→ ${subTargetBranch || 'dev'})`}
            </button>
            <button
              className="sm ghost"
              disabled={totalFiles === 0 || isRunning || submit.isPending}
              onClick={() => openSubmitModal('pr', 'main')}
              title="用于紧急修复(Hotfix)，直接提 PR 到 main 分支"
            >
              提 PR 到 main
            </button>
          </>
        )}

        <button
          className="sm ghost"
          disabled={totalFiles === 0 || isRunning}
          onClick={() => openSubmitModal('push')}
          title="仅推送当前工作分支，不创建 PR"
        >
          仅推送
        </button>
        <button
          className="danger sm"
          disabled={isRunning || discard.isPending}
          onClick={() => confirm('确认丢弃本次 Run 的 worktree 和未推送分支？') && discard.mutate()}
        >
          丢弃
        </button>
      </div>

      {(results || run.changeRecord) && (
        <div className="results" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {results?.map((r) => (
            <div key={r.repoId} className="row" style={{ flexWrap: 'wrap' }}>
              <b>{r.repoName}</b>
              <span className="mono dim">
                {r.branch} : {r.targetBranch}
              </span>
              {r.commit && <span className="mono muted">{r.commit.slice(0, 8)}</span>}
              {r.prUrl && (
                <a href={r.prUrl} target="_blank" rel="noreferrer" className="badge pr-open">
                  {r.prUrl} ↗
                </a>
              )}
              {r.error && <span className="st-failed">{r.error}</span>}
            </div>
          ))}
          {(results?.[0]?.changeRecord || run.changeRecord) && (
            <div className="panel" style={{ padding: 10, background: 'var(--bg)' }}>
              <div className="row" style={{ marginBottom: 4 }}>
                <b className="small">📋 标准改动记录（备查）</b>
                <span className="grow" />
                <button
                  type="button"
                  className="sm ghost"
                  onClick={() => {
                    void copyText(results?.[0]?.changeRecord || run.changeRecord).then((ok) =>
                      ok ? toast.success('已复制标准改动记录') : toast.error('复制失败，请手动选择文本')
                    );
                  }}
                >
                  📋 复制
                </button>
              </div>
              <pre className="mono small" style={{ margin: 0, whiteSpace: 'pre-wrap' }}>
                {results?.[0]?.changeRecord || run.changeRecord}
              </pre>
            </div>
          )}
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
                <button
                  className="primary sm"
                  disabled={!dirty || saveFile.isPending}
                  onClick={() => saveFile.mutate({ repoId: sel.repoId, path: sel.path, content: draft })}
                >
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
                  theme={theme === 'dark' ? 'vs-dark' : 'light'}
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
                    listenerRef.current?.dispose();
                    const mod = editor.getModifiedEditor();
                    listenerRef.current = mod.onDidChangeModelContent(() => {
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
          title={
            subMode === 'merge_local'
              ? '⚡ 确认并合入本地目标分支（自动生成改动记录）'
              : subMode === 'pr'
              ? '🚀 提交并创建 Pull Request（自动生成改动记录）'
              : '☁️ 提交并推送分支（自动生成改动记录）'
          }
          size="lg"
          onClose={() => setSubMode(null)}
          footer={
            <>
              <button onClick={() => setSubMode(null)}>取消</button>
              <button
                className="primary"
                disabled={!subCommit.trim() || submit.isPending}
                onClick={() => submit.mutate(subMode)}
              >
                {submit.isPending
                  ? '处理中…'
                  : subMode === 'merge_local'
                  ? '确认并直接合入本地'
                  : subMode === 'pr'
                  ? '确认提交并创建 PR'
                  : '确认提交并推送'}
              </button>
            </>
          }
        >
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <div className="col" style={{ gap: 10 }}>
              <div
                className="row"
                style={{
                  gap: 8,
                  padding: '4px 0',
                  borderBottom: '1px solid var(--border)',
                  marginBottom: 2,
                }}
              >
                <span className="small muted">提交方式:</span>
                <button
                  type="button"
                  className={`sm ${subMode === 'merge_local' ? 'primary' : 'ghost'}`}
                  onClick={() => setSubMode('merge_local')}
                >
                  ⚡ 合入本地分支
                </button>
                <button
                  type="button"
                  className={`sm ${subMode === 'pr' ? 'primary' : 'ghost'}`}
                  onClick={() => setSubMode('pr')}
                >
                  🚀 创建 PR
                </button>
                <button
                  type="button"
                  className={`sm ${subMode === 'push' ? 'primary' : 'ghost'}`}
                  onClick={() => setSubMode('push')}
                >
                  ☁️ 仅推送
                </button>
              </div>

              <div className="row" style={{ gap: 8 }}>
                <label className="field grow">
                  Commit Message
                  <input value={subCommit} onChange={(e) => setSubCommit(e.target.value)} />
                </label>
                <label className="field" style={{ width: 170 }}>
                  目标基准分支
                  <input
                    className="mono"
                    value={subTargetBranch}
                    onChange={(e) => setSubTargetBranch(e.target.value)}
                    placeholder="main"
                  />
                  <div className="row" style={{ gap: 4, marginTop: 4 }}>
                    <button
                      type="button"
                      className="sm ghost"
                      style={{ fontSize: 11, padding: '0 4px', height: 20 }}
                      onClick={() => setSubTargetBranch('main')}
                    >
                      main
                    </button>
                    <button
                      type="button"
                      className="sm ghost"
                      style={{ fontSize: 11, padding: '0 4px', height: 20 }}
                      onClick={() => setSubTargetBranch('dev')}
                    >
                      dev
                    </button>
                  </div>
                </label>
              </div>
              {subMode === 'pr' && (
                <label className="field">
                  PR 标题
                  <input value={subTitle} onChange={(e) => setSubTitle(e.target.value)} />
                </label>
              )}
              <label className="field">
                本次改动要点（每行一条，自动编号 a. / b. / c.，简明扼要）
                <textarea
                  rows={5}
                  value={subItemsText}
                  onChange={(e) => setSubItemsText(e.target.value)}
                  placeholder={'修复 xxx 问题\n完善 yyy 逻辑'}
                />
              </label>
              <label className="field">
                备注（可选）
                <input
                  value={subRemark}
                  onChange={(e) => setSubRemark(e.target.value)}
                  placeholder="例如：关联上游 PR / 回归测试通过"
                />
              </label>
            </div>

            <div className="col" style={{ gap: 6 }}>
              <div className="row">
                <span className="small muted">标准改动记录实时预览（将写入 PR 描述与 Issue 备注）：</span>
                <span className="grow" />
                <button
                  type="button"
                  className="sm ghost"
                  onClick={() => {
                    void copyText(previewRecord).then((ok) =>
                      ok ? toast.success('已复制预览记录') : toast.error('复制失败，请手动选择文本')
                    );
                  }}
                >
                  📋 复制
                </button>
              </div>
              <pre
                className="mono small"
                style={{
                  flex: 1,
                  background: 'var(--bg)',
                  border: '1px solid var(--border)',
                  borderRadius: 6,
                  padding: 12,
                  whiteSpace: 'pre-wrap',
                  margin: 0,
                }}
              >
                {previewRecord}
              </pre>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}

