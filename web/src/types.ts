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

export type DevMode = 'single_main' | 'main_dev';

export interface Project {
  id: number;
  name: string;
  description: string;
  devMode?: DevMode;
  targetBranch: string;
  autoPrOnConfirm: boolean;
  automationRules?: AutomationRule[];
  repos: Repo[];
  createdAt: string;
  updatedAt: string;
}

export interface RuleConditions {
  author?: string;
  pattern?: string;
  reviewState?: string;
}

export interface RuleAction {
  type: 'merge_pr' | 'set_issue_status' | 'pull_local' | 'notify';
  status?: string;
  strategy?: 'squash' | 'merge' | 'rebase';
  message?: string;
}

export interface AutomationRule {
  id: string;
  name: string;
  enabled: boolean;
  trigger: 'pr_comment' | 'pr_review';
  conditions: RuleConditions;
  actions: RuleAction[];
}

export interface HookStatus {
  name: string;
  event: string;
  path: string;
  exists: boolean;
  executable: boolean;
  description: string;
}

export interface PluginInfo {
  id: string;
  name: string;
  version: string;
  description: string;
  author: string;
  events: string[];
  path: string;
  enabled: boolean;
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
  prUrl: string;
  prState: string;
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
  model: string;
  status: RunStatus;
  branch: string;
  targetBranch: string;
  workdir: string;
  sessionId: string;
  summary: string;
  error: string;
  prUrl: string;
  prState: string;
  changeRecord: string;
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
  args: string[];
  env?: Record<string, string>;
  memoryMB: number;
  defaultModel: string;
  models: string[];
  available: boolean;
}

export interface AgentConfigInput {
  command: string;
  args: string[];
  env?: Record<string, string>;
  memoryMB: number;
  defaultModel: string;
  models: string[];
}

export interface ModelOption {
  value: string;
  name: string;
  provider: string;
}

export interface AgentModels {
  agent: string;
  currentModel: string;
  defaultModel: string;
  customModels: string[];
  providers: string[];
  models: ModelOption[];
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
  targetBranch: string;
  commit: string;
  prUrl: string;
  changeRecord: string;
  error: string;
}

export interface SubmitRequest {
  mode: 'pr' | 'push' | 'merge_local';
  title?: string;
  body?: string;
  commitMessage?: string;
  targetBranch?: string;
  changeItems?: string[];
  remark?: string;
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

export interface BranchWatch {
  id: number;
  projectId: number;
  repoId: number;
  repoName: string;
  branch: string;
  lastSha: string;
  lastMessage: string;
  lastAuthor: string;
  updatedAt: string;
}

export interface CommitInfo {
  sha: string;
  shortSha: string;
  subject: string;
  author: string;
  date: string;
}

export interface Notification {
  id: number;
  kind: 'pr_status' | 'branch_push' | 'run_done' | 'permission' | string;
  title: string;
  body: string;
  link: string;
  meta: string;
  read: boolean;
  createdAt: string;
}

export interface MCPTargetStatus {
  id: string;
  name: string;
  configPath: string;
  detected: boolean;
  installed: boolean;
}

export interface MCPStatus {
  binaryPath: string;
  httpUrl: string;
  targets: MCPTargetStatus[];
}

export interface CherryPickRequest {
  repoId: number;
  issueId?: number;
  sourceBranch: string;
  targetBranch: string;
  newBranch?: string;
  shas: string[];
  mode: 'pr' | 'push';
  title?: string;
  changeItems?: string[];
  remark?: string;
}

export interface CherryPickResult {
  issueId: number;
  issueKey: string;
  runId: number;
  branch: string;
  targetBranch: string;
  commit: string;
  prUrl: string;
  changeRecord: string;
}

export interface RemotePR {
  number: number;
  title: string;
  body: string;
  state: 'open' | 'merged' | 'closed';
  url: string;
  headBranch: string;
  baseBranch: string;
  author: string;
  labels: string[];
  createdAt: string;
  updatedAt: string;
  mergedAt?: string;
  repoId: number;
  repoName: string;
}

export type WsMessage =
  | { type: 'run_event'; runId: number; issueId: number; event: RunEvent }
  | { type: 'run_updated'; run: Run }
  | { type: 'issue_updated'; issue: Issue }
  | { type: 'notification'; notification: Notification }
  | { type: 'heartbeat' };

export interface MonitorLogEntry {
  timestamp: string;
  level: 'INFO' | 'WARN' | 'EVENT' | 'ERROR';
  source: string;
  message: string;
}

export interface MonitorStatus {
  running: boolean;
  intervalSeconds: number;
  lastCheckTime?: string;
  nextCheckTime?: string;
  lastDurationMs: number;
  watchesCount: number;
  openPRsCount: number;
  watchedBranches: BranchWatch[];
  lastTriggeredWatches: number;
  lastUpdatedPRs: number;
}


