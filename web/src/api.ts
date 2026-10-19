import type {
  AgentConfigInput,
  AgentInfo,
  AgentModels,
  BranchWatch,
  BrowseResult,
  CherryPickRequest,
  CherryPickResult,
  Comment,
  CommitInfo,
  Health,
  InspectResult,
  Issue,
  IssueStatus,
  MCPStatus,
  Notification,
  Project,
  Repo,
  RepoChanges,
  RepoInput,
  Run,
  RunEvent,
  SubmitRequest,
  SubmitResult,
} from './types';

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, credentials: 'same-origin', headers: {} };
  if (body !== undefined) {
    (init.headers as Record<string, string>)['Content-Type'] = 'application/json';
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
const del = <T>(p: string) => request<T>('DELETE', p);

const q = (params: Record<string, string | number>) =>
  new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)])).toString();

export const api = {
  health: () => get<Health>('/api/health'),
  restartServer: () => post<{ restarting: boolean }>('/api/restart'),
  agents: () => get<AgentInfo[]>('/api/agents'),
  reloadAgents: () => post<AgentInfo[]>('/api/agents/reload'),
  upsertAgent: (name: string, b: AgentConfigInput) => put<AgentInfo>(`/api/agents/${encodeURIComponent(name)}`, b),
  deleteAgent: (name: string) => del<void>(`/api/agents/${encodeURIComponent(name)}`),
  agentModels: (name: string, refresh?: boolean) =>
    get<AgentModels>(`/api/agents/${encodeURIComponent(name)}/models${refresh ? '?refresh=1' : ''}`),
  browseDir: (path?: string) => get<BrowseResult>(`/api/fs/browse${path ? `?${q({ path })}` : ''}`),
  inspectDir: (path: string) => get<InspectResult>(`/api/fs/inspect?${q({ path })}`),

  mcpStatus: () => get<MCPStatus>('/api/mcp/status'),
  mcpInstall: (targets?: string[]) => post<MCPStatus & { installed: string[] }>('/api/mcp/install', { targets }),

  notifications: (limit = 50) => get<Notification[]>(`/api/notifications?limit=${limit}`),
  markNotificationRead: (id: number) => post<void>(`/api/notifications/${id}/read`),
  markAllNotificationsRead: () => post<void>('/api/notifications/read-all'),

  projects: () => get<Project[]>('/api/projects'),
  createProject: (b: { name: string; description: string }) => post<Project>('/api/projects', b),
  project: (id: number) => get<Project>(`/api/projects/${id}`),
  updateProject: (id: number, b: { name?: string; description?: string }) => patch<Project>(`/api/projects/${id}`, b),
  deleteProject: (id: number) => del<void>(`/api/projects/${id}`),
  addRepo: (projectId: number, b: RepoInput) => post<Repo>(`/api/projects/${projectId}/repos`, b),
  deleteRepo: (projectId: number, repoId: number) => del<void>(`/api/projects/${projectId}/repos/${repoId}`),
  syncProject: (id: number) => post<{ imported: number }>(`/api/projects/${id}/sync`),
  refreshRepos: (id: number) => post<Project>(`/api/projects/${id}/repos/refresh`),

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

  repoBranches: (repoId: number) => get<string[]>(`/api/repos/${repoId}/branches`),
  repoCommits: (repoId: number, branch?: string, limit = 30) =>
    get<CommitInfo[]>(`/api/repos/${repoId}/commits?${q({ branch: branch ?? '', limit })}`),

  issues: (projectId: number) => get<Issue[]>(`/api/projects/${projectId}/issues`),
  createIssue: (projectId: number, b: { title: string; body: string; labels?: string[] }) =>
    post<Issue>(`/api/projects/${projectId}/issues`, b),
  issue: (id: number) => get<Issue>(`/api/issues/${id}`),
  updateIssue: (id: number, b: { title?: string; body?: string; status?: IssueStatus }) =>
    patch<Issue>(`/api/issues/${id}`, b),
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
};

/** react-query keys */
export const qk = {
  health: ['health'] as const,
  agents: ['agents'] as const,
  agentModels: (name: string) => ['agentModels', name] as const,
  mcpStatus: ['mcpStatus'] as const,
  notifications: ['notifications'] as const,
  projects: ['projects'] as const,
  project: (id: number) => ['project', id] as const,
  watches: (projectId: number) => ['watches', projectId] as const,
  repoBranches: (repoId: number) => ['repoBranches', repoId] as const,
  repoCommits: (repoId: number, branch: string) => ['repoCommits', repoId, branch] as const,
  issues: (projectId: number) => ['issues', projectId] as const,
  issue: (id: number) => ['issue', id] as const,
  comments: (issueId: number) => ['comments', issueId] as const,
  runs: (issueId: number) => ['runs', issueId] as const,
  run: (id: number) => ['run', id] as const,
  events: (runId: number) => ['events', runId] as const,
  changes: (runId: number) => ['changes', runId] as const,
  file: (runId: number, repoId: number, path: string, side: string) => ['file', runId, repoId, path, side] as const,
};

