// 与 docs/api.md 保持一致
export type Provider = 'github' | 'gitlab' | 'gitea' | 'plain';

export interface Repo {
  id: number;
  projectId: number;
  name: string;
  provider: Provider;
  remoteUrl: string;
  ownerRepo: string;
  defaultBranch: string;
  testCommand: string;
}

export interface Project {
  id: number;
  name: string;
  description: string;
  repos: Repo[];
  createdAt: string;
  updatedAt: string;
}

export type IssueStatus = 'backlog' | 'todo' | 'running' | 'review' | 'pr_open' | 'done' | 'failed';

export const ISSUE_STATUSES: IssueStatus[] = ['backlog', 'todo', 'running', 'review', 'pr_open', 'done', 'failed'];

export interface Issue {
  id: number;
  projectId: number;
  key: string;
  source: 'local' | 'remote';
  repoId: number | null;
  remoteNumber: number | null;
  remoteUrl: string;
  title: string;
  body: string;
  labels: string[];
  status: IssueStatus;
  activeRunId: number | null;
  awaiting: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface Comment {
  id: number;
  issueId: number;
  author: 'me' | 'agent' | 'system';
  body: string;
  createdAt: string;
}

export type RunStatus =
  | 'queued'
  | 'starting'
  | 'running'
  | 'awaiting_permission'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'submitted'
  | 'discarded';

export const ACTIVE_RUN_STATUSES: RunStatus[] = ['queued', 'starting', 'running', 'awaiting_permission'];

export interface Run {
  id: number;
  issueId: number;
  agent: string;
  status: RunStatus;
  branch: string;
  workdir: string;
  sessionId: string;
  summary: string;
  error: string;
  createdAt: string;
  startedAt: string | null;
  endedAt: string | null;
}

export type RunEventType =
  | 'message'
  | 'thought'
  | 'tool_call'
  | 'tool_call_update'
  | 'plan'
  | 'permission_request'
  | 'permission_resolved'
  | 'status'
  | 'stderr'
  | 'system'
  | 'user';

export interface RunEvent {
  id: number;
  runId: number;
  seq: number;
  type: RunEventType;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  data: any;
  createdAt: string;
}

export type PermissionOptionKind = 'allow_once' | 'allow_always' | 'reject_once' | 'reject_always';

export interface PermissionOption {
  optionId: string;
  name: string;
  kind: PermissionOptionKind;
}

export interface PlanEntry {
  content: string;
  status: string;
  priority: string;
}

export interface Health {
  ok: boolean;
  version: string;
  editorUrlTemplate: string;
}

export interface AgentInfo {
  name: string;
  command: string;
  available: boolean;
}

export type FileStatus = 'M' | 'A' | 'D' | 'R' | '?';

export interface ChangedFile {
  path: string;
  status: FileStatus;
  additions: number;
  deletions: number;
}

export interface RepoChanges {
  repoId: number;
  repoName: string;
  branch: string;
  files: ChangedFile[];
}

export interface SubmitResult {
  repoId: number;
  repoName: string;
  branch: string;
  commit: string;
  prUrl: string;
  error: string;
}

export interface SubmitRequest {
  mode: 'pr' | 'push';
  title?: string;
  body?: string;
  commitMessage?: string;
}

export interface RepoInput {
  name: string;
  provider: Provider;
  remoteUrl: string;
  ownerRepo: string;
  defaultBranch: string;
  testCommand: string;
}

export interface DirEntry {
  name: string;
  path: string;
  isGit: boolean;
}

export interface BrowseResult {
  path: string;
  parent: string;
  isGit: boolean;
  entries: DirEntry[];
}

export interface InspectedRepo {
  path: string;
  name: string;
  provider: Provider;
  remoteUrl: string;
  ownerRepo: string;
  defaultBranch: string;
  testCommand: string;
}

export interface InspectResult {
  path: string;
  suggestedName: string;
  repos: InspectedRepo[];
}

export type WsMessage =
  | { type: 'run_event'; runId: number; issueId: number; event: RunEvent }
  | { type: 'run_updated'; run: Run }
  | { type: 'issue_updated'; issue: Issue };
