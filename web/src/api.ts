import type {
  AgentConfigInput,
  AppSettings,
  AgentInfo,
  AgentModels,
  AutomationRule,
  BranchWatch,
  BrowseResult,
  CherryPickRequest,
  CherryPickResult,
  Comment,
  CommitDetail,
  CommitInfo,
  DevMode,
  Health,
  HookStatus,
  InspectResult,
  Issue,
  IssueStatus,
  MCPStatus,
  RepoPullResult,
  MonitorLogEntry,
  MonitorStatus,
  MonitorConfig,
  Notification,
  PluginConfigFile,
  PluginInfo,
  PluginReadme,
  Project,
  ProjectReadme,
  RemotePR,
  Repo,
  RepoPullStatus,
  RepoChanges,
  RepoInput,
  Run,
  RunEvent,
  SubmitRequest,
  SubmitResult,
  SystemStatus,
  DocItem,
  DocDetail,
  DocSearchResult,
} from './types';

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

// 自动捕获 URL 中的 ?token= 参数并持久化到 localStorage，然后从地址栏移除
if (typeof window !== 'undefined') {
  try {
    const params = new URLSearchParams(window.location.search);
    const tokenInUrl = params.get('token');
    if (tokenInUrl) {
      localStorage.setItem('xissue_token', tokenInUrl);
      params.delete('token');
      const newQuery = params.toString() ? `?${params.toString()}` : '';
      window.history.replaceState({}, '', `${window.location.pathname}${newQuery}${window.location.hash}`);
    }
  } catch {
    // ignore
  }
}

export function getStoredToken(): string {
  try {
    return localStorage.getItem('xissue_token') || '';
  } catch {
    return '';
  }
}

export function setStoredToken(token: string): void {
  try {
    if (token) {
      localStorage.setItem('xissue_token', token);
    } else {
      localStorage.removeItem('xissue_token');
    }
  } catch {
    // ignore
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {
    'X-XIssue': '1',
  };
  const token = getStoredToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  const init: RequestInit = { method, credentials: 'same-origin', headers };
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  let res: Response;
  try {
    res = await fetch(path, init);
  } catch (e) {
    throw new ApiError(0, `网络错误：${(e as Error).message}`);
  }
  const text = await res.text();
  let data: unknown = undefined;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = text;
    }
  }
  if (!res.ok) {
    if (res.status === 401) {
      // 401 时清理可能失效的 token
      setStoredToken('');
      throw new ApiError(401, '未授权访问 (401)：请在服务端配置文件核对 Token，或在 URL 附带 ?token=xxx 重新授权');
    }
    const msg =
      data && typeof data === 'object' && 'error' in data
        ? String((data as { error: unknown }).error)
        : typeof data === 'string' && data
          ? data
          : `${res.status} ${res.statusText}`;
    throw new ApiError(res.status, msg);
  }
  return data as T;
}

const get = <T>(p: string) => request<T>('GET', p);
const post = <T>(p: string, b?: unknown) => request<T>('POST', p, b ?? {});
const patch = <T>(p: string, b: unknown) => request<T>('PATCH', p, b);
const put = <T>(p: string, b: unknown) => request<T>('PUT', p, b);
const del = <T>(p: string, b?: unknown) => request<T>('DELETE', p, b);

const q = (params: Record<string, string | number>) =>
  new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)])).toString();

export const api = {
  health: () => get<Health>('/api/health'),
  restartServer: () => post<{ restarting: boolean }>('/api/restart'),
  status: () => get<SystemStatus>('/api/status'),
  agents: () => get<AgentInfo[]>('/api/agents'),
  reloadAgents: () => post<AgentInfo[]>('/api/agents/reload'),
  upsertAgent: (name: string, b: AgentConfigInput) => put<AgentInfo>(`/api/agents/${encodeURIComponent(name)}`, b),
  deleteAgent: (name: string) => del<void>(`/api/agents/${encodeURIComponent(name)}`),
  agentModels: (name: string, refresh?: boolean) =>
    get<AgentModels>(`/api/agents/${encodeURIComponent(name)}/models${refresh ? '?refresh=1' : ''}`),
  browseDir: (path?: string) => get<BrowseResult>(`/api/fs/browse${path ? `?${q({ path })}` : ''}`),
  inspectDir: (path: string) => get<InspectResult>(`/api/fs/inspect?${q({ path })}`),

  mcpStatus: () => get<MCPStatus>('/api/mcp/status'),
  mcpInstall: (targets?: string[], transport?: 'stdio' | 'http') =>
    post<MCPStatus & { installed: string[]; transport: string }>('/api/mcp/install', { targets, transport }),

  notifications: (limit = 50) => get<Notification[]>(`/api/notifications?limit=${limit}`),
  markNotificationRead: (id: number) => post<void>(`/api/notifications/${id}/read`),
  markAllNotificationsRead: () => post<void>('/api/notifications/read-all'),

  projects: () => get<Project[]>('/api/projects'),
  createProject: (b: { name: string; description: string; devMode?: DevMode }) =>
    post<Project>('/api/projects', b),
  project: (id: number, refresh = true) => get<Project>(`/api/projects/${id}?refresh=${refresh ? 1 : 0}`),
  updateProject: (
    id: number,
    b: {
      name?: string;
      description?: string;
      devMode?: DevMode;
      targetBranch?: string;
      autoPrOnConfirm?: boolean;
      autoDevelop?: boolean;
      autoDevelopAgent?: string;
      autoDevelopLabels?: string[];
      autoDevelopText?: boolean;
      autoDevelopAuthors?: string[];
      autoDevelopDailyLimit?: number;
      automationRules?: AutomationRule[];
      pullBranches?: string[];
    },
  ) => patch<Project>(`/api/projects/${id}`, b),
  deleteProject: (id: number, b?: { name: string }) =>
    del<{ ok: boolean; deleted: string }>(`/api/projects/${id}`, b),
  addRepo: (projectId: number, b: RepoInput) => post<Repo>(`/api/projects/${projectId}/repos`, b),
  deleteRepo: (projectId: number, repoId: number) => del<void>(`/api/projects/${projectId}/repos/${repoId}`),
  syncProject: (id: number) => post<{ imported: number }>(`/api/projects/${id}/sync`),
  refreshRepos: (id: number) => post<Project>(`/api/projects/${id}/repos/refresh`),
  pullProject: (id: number) =>
    post<{ ok: boolean; pulledRepos: string[]; repos: RepoPullResult[]; message: string }>(
      `/api/projects/${id}/pull`,
    ),
  pullStatus: (id: number) => get<RepoPullStatus[]>(`/api/projects/${id}/pull-status`),

  detectHooks: (projectId: number) => get<HookStatus[]>(`/api/projects/${projectId}/hooks`),
  plugins: () => get<PluginInfo[]>('/api/plugins'),
  setPluginEnabled: (id: string, enabled: boolean) =>
    patch<PluginInfo[]>(`/api/plugins/${encodeURIComponent(id)}`, { enabled }),
  pluginReadme: (id: string) => get<PluginReadme>(`/api/plugins/${encodeURIComponent(id)}/readme`),
  pluginConfig: (id: string) => get<PluginConfigFile>(`/api/plugins/${encodeURIComponent(id)}/config`),
  savePluginConfig: (id: string, content: string) =>
    put<PluginConfigFile>(`/api/plugins/${encodeURIComponent(id)}/config`, { content }),

  projectPRs: (id: number, state: 'open' | 'merged' | 'closed' | 'all' = 'open') =>
    get<RemotePR[]>(`/api/projects/${id}/prs?state=${state}`),
  mergeProjectPR: (
    projectId: number,
    number: number,
    b: { repoId: number; method?: 'squash' | 'merge' | 'rebase' },
  ) =>
    post<{ ok: boolean; merged: boolean; message: string }>(
      `/api/projects/${projectId}/prs/${number}/merge`,
      b,
    ),
  closeProjectPR: (projectId: number, number: number, b: { repoId: number }) =>
    post<{ ok: boolean; closed: boolean }>(`/api/projects/${projectId}/prs/${number}/close`, b),

  watches: (projectId: number) => get<BranchWatch[]>(`/api/projects/${projectId}/watches`),
  addWatch: (projectId: number, b: { repoId: number; branch: string }) =>
    post<BranchWatch>(`/api/projects/${projectId}/watches`, b),
  deleteWatch: (projectId: number, watchId: number) =>
    del<void>(`/api/projects/${projectId}/watches/${watchId}`),
  checkWatches: (projectId: number) =>
    post<{ watchesTriggered: number; prsUpdated: number }>(`/api/projects/${projectId}/check-watches`),
  batchRunTodo: (projectId: number, b?: { agent?: string; model?: string }) =>
    post<{ started: number; runs: Run[] }>(`/api/projects/${projectId}/batch-run`, b ?? {}),
  cherryPick: (projectId: number, b: CherryPickRequest) =>
    post<CherryPickResult>(`/api/projects/${projectId}/cherry-pick`, b),

  repoBranches: (repoId: number, refresh = false) => get<string[]>(`/api/repos/${repoId}/branches?refresh=${refresh ? 1 : 0}`),
  repoCommits: (repoId: number, branch?: string, limit = 30, refresh = false) =>
    get<CommitInfo[]>(`/api/repos/${repoId}/commits?${q({ branch: branch ?? '', limit, refresh: refresh ? 1 : 0 })}`),
  repoCommitDetail: (repoId: number, sha: string) =>
    get<CommitDetail>(`/api/repos/${repoId}/commits/${encodeURIComponent(sha)}`),

  issues: (projectId: number) => get<Issue[]>(`/api/projects/${projectId}/issues`),
  createIssue: (projectId: number, b: { title: string; body: string; labels?: string[]; repoId?: number | null }) =>
    post<Issue>(`/api/projects/${projectId}/issues`, b),
  issue: (id: number) => get<Issue>(`/api/issues/${id}`),
  updateIssue: (id: number, b: { title?: string; body?: string; status?: IssueStatus }) =>
    patch<Issue>(`/api/issues/${id}`, b),
  deleteIssue: (id: number) => del<void>(`/api/issues/${id}`),
  batchDeleteIssues: (ids: number[]) => post<{ deleted: number }>('/api/issues/batch-delete', { ids }),
  publishIssue: (id: number, repoId: number) => post<Issue>(`/api/issues/${id}/publish`, { repoId }),
  comments: (issueId: number) => get<Comment[]>(`/api/issues/${issueId}/comments`),
  addComment: (issueId: number, body: string) => post<Comment>(`/api/issues/${issueId}/comments`, { body }),

  runs: (issueId: number) => get<Run[]>(`/api/issues/${issueId}/runs`),
  startRun: (issueId: number, b: { agent: string; model?: string; prompt?: string }) =>
    post<Run>(`/api/issues/${issueId}/runs`, b),
  run: (id: number) => get<Run>(`/api/runs/${id}`),
  deleteRun: (id: number) => del<void>(`/api/runs/${id}`),
  events: (runId: number, after?: number) =>
    get<RunEvent[]>(`/api/runs/${runId}/events${after !== undefined ? `?after=${after}` : ''}`),
  sendMessage: (runId: number, text: string) => post<void>(`/api/runs/${runId}/message`, { text }),
  respondPermission: (runId: number, requestId: string, optionId: string) =>
    post<void>(`/api/runs/${runId}/permissions/${encodeURIComponent(requestId)}`, { optionId }),
  cancelRun: (runId: number) => post<void>(`/api/runs/${runId}/cancel`),
  changes: (runId: number) => get<RepoChanges[]>(`/api/runs/${runId}/changes`),
  file: (runId: number, repoId: number, path: string, side: 'base' | 'work') =>
    get<{ content: string }>(`/api/runs/${runId}/file?${q({ repoId, path, side })}`),
  saveFile: (runId: number, repoId: number, path: string, content: string) =>
    put<void>(`/api/runs/${runId}/file?${q({ repoId, path })}`, { content }),
  submit: (runId: number, b: SubmitRequest) => post<{ results: SubmitResult[] }>(`/api/runs/${runId}/submit`, b),
  discard: (runId: number) => post<void>(`/api/runs/${runId}/discard`),
  runArchive: (runId: number) =>
    get<{ dir: string; analysis: string; events: string }>(`/api/runs/${runId}/archive`),

  monitorStatus: () => get<MonitorStatus>('/api/monitor/status'),
  settings: () => get<AppSettings>('/api/settings'),
  patchSettings: (b: Partial<AppSettings>) => patch<AppSettings>('/api/settings', b),
  monitorConfig: () => get<MonitorConfig>('/api/monitor/config'),
  patchMonitorConfig: (b: Partial<MonitorConfig>) => patch<MonitorConfig>('/api/monitor/config', b),
  testNotify: () => post<{ ok: boolean; message: string }>('/api/monitor/test-notify'),
  monitorLogs: (hours = 8) => get<MonitorLogEntry[]>(`/api/monitor/logs?hours=${hours}`),
  triggerMonitor: () => post<{ ok: boolean; watchesTriggered: number; prsUpdated: number; message: string }>('/api/monitor/trigger'),
  syncAllIssues: () =>
    post<{ ok: boolean; projects: number; imported: number; errors?: string[]; message: string }>(
      '/api/monitor/sync',
    ),
  pruneMonitorLogs: (hours = 8) => post<{ ok: boolean; prunedCount: number; message: string }>(`/api/monitor/prune?hours=${hours}`),

  // 知识库 (Wiki & Docs)
  projectDocs: (projectId: number) => get<DocItem[]>(`/api/projects/${projectId}/docs`),
  projectReadmes: (projectId: number) => get<ProjectReadme[]>(`/api/projects/${projectId}/readmes`),
  docDetail: (projectId: number, docId: string) =>
    get<DocDetail>(`/api/projects/${projectId}/docs/detail?${q({ docId })}`),
  syncWiki: (projectId: number, repoId?: number) =>
    post<{ ok: boolean; syncedCount: number; message: string }>(`/api/projects/${projectId}/docs/sync-wiki`, { repoId }),
  searchDocs: (projectId: number, query: string, limit = 20) =>
    post<DocSearchResult[]>(`/api/projects/${projectId}/docs/search`, { query, limit }),
  saveDoc: (projectId: number, docId: string, content: string) =>
    put<{ ok: boolean; message: string }>(`/api/projects/${projectId}/docs/save`, { docId, content }),
};

/** react-query keys */
export const qk = {
  health: ['health'] as const,
  status: ['status'] as const,
  monitorStatus: ['monitorStatus'] as const,
  settings: ['settings'] as const,
  monitorConfig: ['monitorConfig'] as const,
  monitorLogs: (hours?: number) => ['monitorLogs', hours ?? 8] as const,
  agents: ['agents'] as const,
  agentModels: (name: string) => ['agentModels', name] as const,
  mcpStatus: ['mcpStatus'] as const,
  notifications: ['notifications'] as const,
  projects: ['projects'] as const,
  project: (id: number) => ['project', id] as const,
  projectHooks: (projectId: number) => ['projectHooks', projectId] as const,
  plugins: ['plugins'] as const,
  pluginReadme: (id: string) => ['pluginReadme', id] as const,
  pluginConfig: (id: string) => ['pluginConfig', id] as const,
  projectPRs: (projectId: number, state?: string) => ['projectPRs', projectId, state ?? 'open'] as const,
  projectPullStatus: (projectId: number) => ['projectPullStatus', projectId] as const,
  projectDocs: (projectId: number) => ['projectDocs', projectId] as const,
  projectReadmes: (projectId: number) => ['projectReadmes', projectId] as const,
  docDetail: (projectId: number, docId: string) => ['docDetail', projectId, docId] as const,
  watches: (projectId: number) => ['watches', projectId] as const,
  repoBranches: (repoId: number) => ['repoBranches', repoId] as const,
  repoCommits: (repoId: number, branch: string) => ['repoCommits', repoId, branch] as const,
  repoCommitDetail: (repoId: number, sha: string) => ['repoCommitDetail', repoId, sha] as const,
  issues: (projectId: number) => ['issues', projectId] as const,
  issue: (id: number) => ['issue', id] as const,
  comments: (issueId: number) => ['comments', issueId] as const,
  runs: (issueId: number) => ['runs', issueId] as const,
  run: (id: number) => ['run', id] as const,
  events: (runId: number) => ['events', runId] as const,
  changes: (runId: number) => ['changes', runId] as const,
  file: (runId: number, repoId: number, path: string, side: string) => ['file', runId, repoId, path, side] as const,
};

