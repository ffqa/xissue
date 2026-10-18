import type {
  AgentInfo,
  BrowseResult,
  Comment,
  Health,
  InspectResult,
  Issue,
  IssueStatus,
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
  agents: () => get<AgentInfo[]>('/api/agents'),
  browseDir: (path?: string) => get<BrowseResult>(`/api/fs/browse${path ? `?${q({ path })}` : ''}`),
  inspectDir: (path: string) => get<InspectResult>(`/api/fs/inspect?${q({ path })}`),

  projects: () => get<Project[]>('/api/projects'),
  createProject: (b: { name: string; description: string }) => post<Project>('/api/projects', b),
  project: (id: number) => get<Project>(`/api/projects/${id}`),
  updateProject: (id: number, b: { name?: string; description?: string }) => patch<Project>(`/api/projects/${id}`, b),
  deleteProject: (id: number) => del<void>(`/api/projects/${id}`),
  addRepo: (projectId: number, b: RepoInput) => post<Repo>(`/api/projects/${projectId}/repos`, b),
  deleteRepo: (projectId: number, repoId: number) => del<void>(`/api/projects/${projectId}/repos/${repoId}`),
  syncProject: (id: number) => post<{ imported: number }>(`/api/projects/${id}/sync`),

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
  startRun: (issueId: number, b: { agent: string; prompt?: string }) => post<Run>(`/api/issues/${issueId}/runs`, b),
  run: (id: number) => get<Run>(`/api/runs/${id}`),
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
};

/** react-query keys */
export const qk = {
  health: ['health'] as const,
  agents: ['agents'] as const,
  projects: ['projects'] as const,
  project: (id: number) => ['project', id] as const,
  issues: (projectId: number) => ['issues', projectId] as const,
  issue: (id: number) => ['issue', id] as const,
  comments: (issueId: number) => ['comments', issueId] as const,
  runs: (issueId: number) => ['runs', issueId] as const,
  run: (id: number) => ['run', id] as const,
  events: (runId: number) => ['events', runId] as const,
  changes: (runId: number) => ['changes', runId] as const,
  file: (runId: number, repoId: number, path: string, side: string) => ['file', runId, repoId, path, side] as const,
};
