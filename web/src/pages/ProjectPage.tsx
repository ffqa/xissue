import { useEffect, useState } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { PluginSlot } from '../components/PluginFrame';
import { LabelPicker, splitLabels } from '../components/LabelPicker';
import { availableRemoteAgentNames, filterRemoteCapable, pickDefaultAgent } from '../logic/agents';
import { filterPRs } from '../logic/prs';
import { useProjectIssues, useProjectPRPages } from '../hooks/useProjectLists';
import { Crumbs, fmtTime, ISSUE_STATUS_LABEL, Modal } from '../components/common';
import { LocalDirPickerModal } from '../components/LocalDirPicker';
import { CherryPickModal } from '../components/CherryPickModal';
import { PRModal } from '../components/PRModal';
import { BranchCleanupModal } from '../components/BranchCleanupModal';
import { BacklogPanel } from '../components/BacklogPanel';
import { CommitsModal } from '../components/CommitsModal';
import { DeleteProjectModal } from '../components/DeleteProjectModal';
import { KnowledgeBaseModal } from '../components/KnowledgeBaseModal';
import { ProjectReadmeModal } from '../components/ProjectReadmeModal';
import { getCommitWebUrl, getBranchWebUrl } from '../gitUrls';
import { toast } from '../toast';
import { copyText } from '../clipboard';
import {
  type AutomationRule,
  type DevMode,
  type HookStatus,
  type InspectResult,
  type Issue,
  type IssuePriority,
  type IssueStatus,
  ISSUE_PRIORITIES,
  type Provider,
  type RepoInput,
  type RuleAction,
} from '../types';

const PROVIDERS: Provider[] = ['github', 'gitlab', 'gitea', 'plain'];
const PRIORITY_LABEL: Record<IssuePriority, string> = {
  low: '低',
  normal: '普通',
  high: '高',
  urgent: '紧急',
};
const emptyRepo: RepoInput = {
  name: '',
  provider: 'github',
  remoteUrl: '',
  ownerRepo: '',
  defaultBranch: 'main',
  testCommand: '',
};

interface DraftAttachment {
  file: File;
  preview?: string;
}

function issueCanSyncText(provider?: Provider): string {
  if (!provider || provider === 'plain') return '该仓库不支持远程 Issue。';
  return `同步到 ${provider.toUpperCase()} 远程 Issue。`;
}


interface StageColumn {
  id: 'todo' | 'running' | 'review' | 'done';
  title: string;
  subtitle: string;
  statuses: IssueStatus[];
}

const WORKFLOW_COLUMNS: StageColumn[] = [
  {
    id: 'todo',
    title: '📥 待办池 (Todo)',
    subtitle: '拖入「执行中」即可自动启动 Agent',
    statuses: ['todo'],
  },
  {
    id: 'running',
    title: '⚡ 执行中 (Running)',
    subtitle: '拖入此列自动启动 Agent 编码',
    statuses: ['running', 'failed'],
  },
  {
    id: 'review',
    title: '🔍 待验收 / PR (Review)',
    subtitle: '点击进入详情页审查 Diff 与提 PR',
    statuses: ['review', 'pr_open'],
  },
  {
    id: 'done',
    title: '✅ 已完成 (Done)',
    subtitle: 'PR 已合并 / 已归档',
    statuses: ['done'],
  },
];

function summarizeIssue(it: Issue): string {
  const raw = (it.body ?? '')
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/[#>*_`~-]/g, ' ')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/\s+/g, ' ')
    .trim();
  if (raw) {
    return raw.length > 90 ? raw.slice(0, 90) + '…' : raw;
  }
  if (it.prUrl) {
    return `PR (${it.prState || 'open'}): ${it.prUrl}`;
  }
  if (it.status === 'failed') {
    return '上次执行失败，进入详情查看日志或拖入「执行中」重试';
  }
  return '暂无摘要描述，点击卡片进入详情页查看';
}

export default function ProjectPage() {
  const id = Number(useParams().id);
  const nav = useNavigate();
  const qc = useQueryClient();
  const project = useQuery({ queryKey: qk.project(id), queryFn: () => api.project(id) });
  const issues = useProjectIssues(id);
  const agentsQ = useQuery({ queryKey: qk.agents, queryFn: api.agents });
  const settingsQ = useQuery({ queryKey: qk.settings, queryFn: api.settings });
  const watchesQ = useQuery({ queryKey: qk.watches(id), queryFn: () => api.watches(id) });
  const runnersQ = useQuery({ queryKey: qk.runners, queryFn: api.runners, refetchInterval: 15000 });

  const [repoOpen, setRepoOpen] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [repo, setRepo] = useState<RepoInput>(emptyRepo);

  const [issueOpen, setIssueOpen] = useState(false);
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [autoRunNew, setAutoRunNew] = useState(false);
  const [issueRepoId, setIssueRepoId] = useState(0);
  const [issueLabels, setIssueLabels] = useState('');
  const [issuePriority, setIssuePriority] = useState<IssuePriority>('normal');
  const [syncRemote, setSyncRemote] = useState(false);
  const [issueFiles, setIssueFiles] = useState<DraftAttachment[]>([]);
  const [attachDrag, setAttachDrag] = useState(false);

  const [watchRepoId, setWatchRepoId] = useState<number>(0);
  const [watchBranch, setWatchBranch] = useState('');
  const [search, setSearch] = useSearchParams();

  const [cpOpen, setCpOpen] = useState(false);
  const [cpInitRepoId, setCpInitRepoId] = useState<number | undefined>();
  const [cpInitBranch, setCpInitBranch] = useState<string | undefined>();

  const [draggingId, setDraggingId] = useState<number | null>(null);
  const [dragOverCol, setDragOverCol] = useState<StageColumn['id'] | null>(null);

  const [doneModalOpen, setDoneModalOpen] = useState(false);
  const [selectedDoneIds, setSelectedDoneIds] = useState<number[]>([]);
  const [doneSearchText, setDoneSearchText] = useState('');

  const [deleteProjectOpen, setDeleteProjectOpen] = useState(false);
  const [prModalOpen, setPrModalOpen] = useState(false);
  const [branchCleanupOpen, setBranchCleanupOpen] = useState(false);
  const [projectView, setProjectView] = useState<'board' | 'backlog'>('board');
  const [commitsModal, setCommitsModal] = useState<{ repoId?: number; branch?: string; sha?: string } | null>(null);
  const [knowledgeOpen, setKnowledgeOpen] = useState(false);
  const [readmeOpen, setReadmeOpen] = useState(false);

  const deleteIssueMut = useMutation({
    mutationFn: (issueId: number) => api.deleteIssue(issueId),
    onSuccess: (_, issueId) => {
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      setSelectedDoneIds((prev) => prev.filter((x) => x !== issueId));
      toast.success('已删除任务');
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const batchDeleteMut = useMutation({
    mutationFn: (ids: number[]) => api.batchDeleteIssues(ids),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      setSelectedDoneIds([]);
      toast.success(`已成功批量删除 ${r.deleted} 个已完成任务`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const [settingsOpen, setSettingsOpen] = useState(false);
  const hooksQ = useQuery({ queryKey: qk.projectHooks(id), queryFn: () => api.detectHooks(id), enabled: settingsOpen });
  const pullStatusQ = useQuery({
    queryKey: qk.projectPullStatus(id),
    queryFn: () => api.pullStatus(id),
    enabled: settingsOpen,
  });

  const [settingsTab, setSettingsTab] = useState<'mode' | 'automation'>('mode');
  const [cfgDevMode, setCfgDevMode] = useState<DevMode>('single_main');
  const [cfgTargetBranch, setCfgTargetBranch] = useState('main');
  const [cfgRunnerID, setCfgRunnerID] = useState('');
  const [cfgAutoPR, setCfgAutoPR] = useState(true);
  const [cfgAutoDevelop, setCfgAutoDevelop] = useState(false);
  const [cfgAutoDevelopAgent, setCfgAutoDevelopAgent] = useState('');
  const [cfgAutoDevelopLabels, setCfgAutoDevelopLabels] = useState('bug, fix, error, crash, regression, enhancement, feature, 需求, 缺陷, 优化, 功能, 报错, 崩溃');
  const [cfgAutoDevelopText, setCfgAutoDevelopText] = useState(false);
  const [cfgAutoDevelopAuthors, setCfgAutoDevelopAuthors] = useState('');
  const [cfgAutoDevelopDailyLimit, setCfgAutoDevelopDailyLimit] = useState(5);
  const [cfgAutomationRules, setCfgAutomationRules] = useState<AutomationRule[]>([]);
  const [cfgPullBranches, setCfgPullBranches] = useState('');

  // 规则配置表单状态
  const [newRuleOpen, setNewRuleOpen] = useState(false);
  const [newRuleName, setNewRuleName] = useState('监控分支新推送自动建 Issue');
  const [newRuleTrigger, setNewRuleTrigger] = useState<'pr_comment' | 'pr_review' | 'branch_push'>('branch_push');
  const [newRuleBranch, setNewRuleBranch] = useState('');
  const [newRuleAuthor, setNewRuleAuthor] = useState('');
  const [newRulePattern, setNewRulePattern] = useState('');
  const [newRuleMergePR, setNewRuleMergePR] = useState(false);
  const [newRuleSetDone, setNewRuleSetDone] = useState(false);
  const [newRulePullLocal, setNewRulePullLocal] = useState(true);
  const [newRuleNotify, setNewRuleNotify] = useState(true);
  const [newRuleCreateIssue, setNewRuleCreateIssue] = useState(true);
  const [newRuleTriggerRun, setNewRuleTriggerRun] = useState(false);
  const [newRuleAgent, setNewRuleAgent] = useState('');
  const [newRulePrompt, setNewRulePrompt] = useState('');
  const [copySuccess, setCopySuccess] = useState(false);

  const updateProjectMut = useMutation({
    mutationFn: (b: {
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
      runnerId?: string;
    }) => api.updateProject(id, b),
    onSuccess: (updated) => {
      qc.setQueryData(qk.project(id), updated);
      setSettingsOpen(false);
      toast.success('项目设置与自动化规则已保存');
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const saveRulesMut = useMutation({
    scope: { id: 'project-automation-rules' },
    mutationFn: (rules: AutomationRule[]) => api.updateProject(id, { automationRules: rules }),
    onSuccess: (updated) => {
      qc.setQueryData(qk.project(id), updated);
    },
    onError: (e) => toast.error(`规则自动保存失败: ${(e as Error).message}`),
  });

  // Rules are persisted as soon as they are added/toggled/deleted, so closing
  // the settings dialog (or navigating away) can never discard them.
  const persistRules = (rules: AutomationRule[]) => {
    setCfgAutomationRules(rules);
    saveRulesMut.mutate(rules);
  };

  const remoteProject = Boolean(project.data?.runnerId);
  // The global default agent is authoritative. Only fall back to opencode (or
  // the first usable agent) when it is unset or unavailable on this node.
  const configuredAgent = settingsQ.data?.defaultAgent?.trim() ?? '';
  const remoteRunner = (runnersQ.data?.runners ?? []).find((r) => r.id === project.data?.runnerId);
  const remoteAgentNames = availableRemoteAgentNames(remoteRunner?.agents);
  const usableAgents = remoteProject
    ? filterRemoteCapable(agentsQ.data ?? [], remoteRunner?.agents, Boolean(remoteRunner?.agentsKnown))
    : (agentsQ.data ?? []).filter((a) => a.available);
  const defaultAgent = pickDefaultAgent(configuredAgent, usableAgents);
  const selectedIssueRepo = (project.data?.repos ?? []).find((repo) => repo.id === issueRepoId);
  const issueRepoCanSync = Boolean(selectedIssueRepo && selectedIssueRepo.provider !== 'plain');

  const refreshProject = () => {
    qc.invalidateQueries({ queryKey: qk.project(id) });
    qc.invalidateQueries({ queryKey: qk.projects });
    qc.invalidateQueries({ queryKey: qk.projectReadmes(id) });
  };

  const addRepo = useMutation({
    mutationFn: () => api.addRepo(id, { ...repo, ownerRepo: repo.provider === 'plain' ? '' : repo.ownerRepo }),
    onSuccess: () => {
      refreshProject();
      setRepo(emptyRepo);
      setRepoOpen(false);
      toast.success('仓库已添加');
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const handlePickedLocal = async (res: InspectResult) => {
    setPickerOpen(false);
    if (res.repos.length === 0) {
      toast.error('所选目录下未找到 Git 仓库');
      return;
    }
    if (res.repos.length === 1) {
      const r = res.repos[0];
      setRepo({
        name: r.name,
        provider: r.provider,
        remoteUrl: r.remoteUrl,
        ownerRepo: r.ownerRepo,
        defaultBranch: r.defaultBranch || 'main',
        testCommand: r.testCommand,
      });
      setRepoOpen(true);
      toast.info(`已自动解析 ${r.name} 的 Git 信息，请确认后点击添加`);
      return;
    }
    let added = 0;
    for (const r of res.repos) {
      try {
        await api.addRepo(id, {
          name: r.name,
          provider: r.provider,
          remoteUrl: r.remoteUrl,
          ownerRepo: r.provider === 'plain' ? '' : r.ownerRepo,
          defaultBranch: r.defaultBranch || 'main',
          testCommand: r.testCommand,
        });
        added++;
      } catch (e) {
        toast.error(`${r.name}: ${(e as Error).message}`);
      }
    }
    if (added > 0) {
      refreshProject();
      toast.success(`已从本地目录导入 ${added} 个仓库`);
    }
  };

  const delRepo = useMutation({
    mutationFn: (repoId: number) => api.deleteRepo(id, repoId),
    onSuccess: refreshProject,
  });

  const sync = useMutation({
    mutationFn: () => api.syncProject(id),
    onSuccess: (r) => {
      refreshProject();
      toast.success(`已从平台同步 ${r.imported} 个新 Issue 到待办池`);
      qc.invalidateQueries({ queryKey: qk.issues(id) });
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const refreshReposMut = useMutation({
    mutationFn: () => api.refreshRepos(id),
    onSuccess: (updated) => {
      qc.setQueryData(qk.project(id), updated);
      toast.success('已刷新并自动识别各仓库最新的远端 Git 平台配置');
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const checkWatchesMut = useMutation({
    mutationFn: () => api.checkWatches(id),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: qk.watches(id) });
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      qc.invalidateQueries({ queryKey: qk.projectPRs(id) });
      qc.invalidateQueries({ queryKey: qk.projectPRPages(id) });
      qc.invalidateQueries({ queryKey: qk.notifications });
      toast.success(
        `已检查分支与 PR 状态：${r.watchesTriggered} 个分支有新推送，${r.prsUpdated} 个 PR 状态已更新`,
      );
    },
    onError: (e) => toast.error((e as Error).message),
  });

  // Keep the full PR list warm while the project page is open. The PR modal
  // reads the same cache, so opening it or switching to Merged/Closed is
  // instant; open PRs still refresh every 60s and update the badge.
  const projectPRsQ = useProjectPRPages(id, 60_000);
  const projectPRs = projectPRsQ.data?.pages.flatMap((page) => page.prs ?? []) ?? [];
  const openPRCount = filterPRs(projectPRs, 'open').length;

  const pullMut = useMutation({
    mutationFn: () => api.pullProject(id),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: qk.projectPRs(id) });
      qc.invalidateQueries({ queryKey: qk.projectPRPages(id) });
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      qc.invalidateQueries({ queryKey: qk.watches(id) });
      qc.invalidateQueries({ queryKey: qk.projectPullStatus(id) });
      if (res.ok) {
        toast.success(res.message || '已成功拉取远端最新代码并同步到本地！');
      } else {
        toast.info(res.message || '没有可更新的本地仓库');
      }
    },
    onError: (e) => toast.error(`拉取代码失败: ${(e as Error).message}`),
  });

  const batchRunMut = useMutation({
    mutationFn: () => api.batchRunTodo(id, { agent: defaultAgent }),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      toast.success(`已使用 ${defaultAgent} 自动启动 ${r.started} 个待办任务`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const quickRunMut = useMutation({
    mutationFn: (issueId: number) => api.startRun(issueId, { agent: defaultAgent }),
    onSuccess: (run) => {
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      toast.success(`已启动 Run #${run.id} (${run.agent})`);
      nav(`/issues/${run.issueId}`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const addIssueFiles = (files: FileList | null) => {
    if (!files) return;
    setIssueFiles((prev) => [
      ...prev,
      ...Array.from(files).map((file) => ({
        file,
        preview: file.type.startsWith('image/') ? URL.createObjectURL(file) : undefined,
      })),
    ]);
  };

  const removeIssueFile = (index: number) => {
    setIssueFiles((prev) => {
      const removed = prev[index];
      if (removed?.preview) URL.revokeObjectURL(removed.preview);
      return prev.filter((_, i) => i !== index);
    });
  };

  const closeIssueModal = () => {
    issueFiles.forEach((draft) => draft.preview && URL.revokeObjectURL(draft.preview));
    setIssueFiles([]);
    setAttachDrag(false);
    setIssueOpen(false);
  };

  const createIssue = useMutation({
    mutationFn: async () => {
      const iss = await api.createIssue(id, {
        title: title.trim(),
        body,
        labels: splitLabels(issueLabels),
        priority: issuePriority,
        repoId: issueRepoId || undefined,
        syncRemote: syncRemote && issueRepoId > 0,
      });
      let attachmentError = '';
      for (const draft of issueFiles) {
        try {
          await api.uploadAttachment(iss.id, draft.file);
        } catch (e) {
          attachmentError = (e as Error).message;
        }
      }
      if (autoRunNew) {
        await api.startRun(iss.id, { agent: defaultAgent });
      }
      return { iss, attachmentError };
    },
    onSuccess: ({ iss, attachmentError }) => {
      qc.invalidateQueries({ queryKey: qk.issues(id) });
      qc.invalidateQueries({ queryKey: qk.issueEvents(iss.id) });
      qc.invalidateQueries({ queryKey: qk.attachments(iss.id) });
      issueFiles.forEach((draft) => draft.preview && URL.revokeObjectURL(draft.preview));
      setIssueFiles([]);
      setTitle('');
      setBody('');
      setIssueLabels('');
      setIssuePriority('normal');
      setIssueRepoId(0);
      setSyncRemote(false);
      setIssueOpen(false);
      if (iss.syncError) {
        toast.error(`已创建 ${iss.key}，但同步远程失败：${iss.syncError}`);
      } else if (iss.source === 'remote') {
        toast.success(`已创建并同步远程 Issue ${iss.key}`);
      }
      if (attachmentError) toast.error(`附件上传失败：${attachmentError}`);
      if (autoRunNew) {
        toast.success(`已创建 ${iss.key} 并立即启动 Agent 执行`);
        nav(`/issues/${iss.id}`);
      }
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const setStatus = useMutation({
    mutationFn: ({ issueId, status }: { issueId: number; status: IssueStatus }) =>
      api.updateIssue(issueId, { status }),
    onSuccess: (issue) => {
      qc.setQueryData<Issue[]>(qk.issues(id), (old) => old?.map((x) => (x.id === issue.id ? issue : x)));
      qc.setQueryData(qk.issue(issue.id), issue);
    },
  });

  const addWatchMut = useMutation({
    mutationFn: () => {
      const rid = watchRepoId || (project.data?.repos?.[0]?.id ?? 0);
      return api.addWatch(id, { repoId: rid, branch: watchBranch.trim() });
    },
    onSuccess: (w) => {
      qc.invalidateQueries({ queryKey: qk.watches(id) });
      setWatchBranch('');
      toast.success(`已开始监控分支 ${w.repoName}:${w.branch} (当前 ${w.lastSha.slice(0, 7)})`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const delWatchMut = useMutation({
    mutationFn: (watchId: number) => api.deleteWatch(id, watchId),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.watches(id) }),
  });

  const p = project.data;
  const allIssues = issues.data ?? [];
  const watches = watchesQ.data ?? [];

  // Deep links from notifications open the matching modal over the project
  // instead of navigating to a separate full-width page.
  useEffect(() => {
    const watchID = Number(search.get('watch'));
    const wantsCommits = search.get('commits') === '1';
    const wantsPRs = search.get('prs') === '1';
    if (!watchID && !wantsCommits && !wantsPRs) return;
    if (watchID && watches.length === 0) return;
    if (wantsPRs) setPrModalOpen(true);
    if (wantsCommits || watchID) {
      const watch = watchID ? watches.find((w) => w.id === watchID) : undefined;
      setCommitsModal({
        repoId: watch?.repoId ?? (Number(search.get('repo')) || undefined),
        branch: watch?.branch ?? (search.get('branch') || undefined),
        sha: watch?.lastSha ?? (search.get('sha') || undefined),
      });
    }
    const next = new URLSearchParams(search);
    next.delete('watch');
    next.delete('commits');
    next.delete('repo');
    next.delete('branch');
    next.delete('sha');
    next.delete('prs');
    setSearch(next, { replace: true });
  }, [search, watches, setSearch]);

  const doneIssues = allIssues.filter((it) => it.status === 'done');
  const filteredDoneIssues = doneIssues.filter((it) => {
    if (!doneSearchText.trim()) return true;
    const q = doneSearchText.toLowerCase();
    const repoName = (p?.repos ?? []).find((r) => r.id === it.repoId)?.name || '';
    return (
      it.title.toLowerCase().includes(q) ||
      it.key.toLowerCase().includes(q) ||
      repoName.toLowerCase().includes(q)
    );
  });

  const allFilteredSelected =
    filteredDoneIssues.length > 0 &&
    filteredDoneIssues.every((it) => selectedDoneIds.includes(it.id));

  const toggleSelectAll = () => {
    if (allFilteredSelected) {
      setSelectedDoneIds((prev) =>
        prev.filter((id) => !filteredDoneIssues.some((it) => it.id === id)),
      );
    } else {
      const toAdd = filteredDoneIssues.map((it) => it.id);
      setSelectedDoneIds((prev) => Array.from(new Set([...prev, ...toAdd])));
    }
  };

  const toggleSelectOne = (issueId: number) => {
    setSelectedDoneIds((prev) =>
      prev.includes(issueId) ? prev.filter((x) => x !== issueId) : [...prev, issueId],
    );
  };

  const setR = <K extends keyof RepoInput>(k: K, v: RepoInput[K]) => setRepo((r) => ({ ...r, [k]: v }));

  const handleDropToColumn = (col: StageColumn, issueId: number) => {
    const target = allIssues.find((x) => x.id === issueId);
    if (!target) return;
    if (col.id === 'running') {
      if (target.status === 'running') return;
      quickRunMut.mutate(target.id);
      return;
    }
    const nextStatus: IssueStatus = col.id === 'todo' ? 'todo' : col.id === 'review' ? 'review' : 'done';
    if (target.status !== nextStatus) {
      setStatus.mutate({ issueId: target.id, status: nextStatus });
    }
  };

  return (
    <div className="page project-page">
      <div className="project-header-section">
        <Crumbs>
          <Link to="/">项目</Link>
          <span>/</span>
          <span className="ellipsis">{p?.name ?? '…'}</span>
        </Crumbs>

      <div className="row" style={{ flexWrap: 'wrap', gap: 8 }}>
        <div className="grow" style={{ minWidth: 0 }}>
          <div className="row" style={{ gap: 10, alignItems: 'baseline', minWidth: 0 }}>
            <h1 style={{ flex: 'none' }}>{p?.name ?? '加载中…'}</h1>
            {p?.description && (
              <span className="muted small ellipsis" style={{ flex: 1, minWidth: 0 }}>{p.description}</span>
            )}
          </div>
          {p?.runnerId && (() => {
            const r = (runnersQ.data?.runners ?? []).find((x) => x.id === p.runnerId);
            return (
              <div className="small" style={{ marginTop: 4 }}>
                🖥️ 执行节点：<span className="mono">{r?.name || p.runnerId}</span>{' '}
                <span style={{ color: r ? 'var(--success)' : 'var(--error)' }}>{r ? '在线' : '离线'}</span>
              </div>
            );
          })()}
        </div>
        <div className="row" style={{ gap: 2 }}>
          <button className={projectView === 'board' ? 'primary' : 'ghost'} onClick={() => setProjectView('board')}>看板</button>
          <button className={projectView === 'backlog' ? 'primary' : 'ghost'} onClick={() => setProjectView('backlog')}>Backlog</button>
        </div>
        <button
          className="ghost"
          disabled={checkWatchesMut.isPending}
          onClick={() => checkWatchesMut.mutate()}
          title="立即拉取监控分支新推送，并检查所有 PR 合并状态"
        >
          {checkWatchesMut.isPending ? '检查中…' : '🔄 检查状态'}
        </button>
        <button
          className="ghost"
          disabled={(p?.repos ?? []).length === 0}
          onClick={() => {
            setCpInitRepoId(p?.repos?.[0]?.id);
            setCpInitBranch(undefined);
            setCpOpen(true);
          }}
          title="从指定分支选择 Commit，Cherry-Pick 提 PR"
        >
          🍒 Cherry-Pick
        </button>

        <button
          className="ghost"
          onClick={() => setPrModalOpen(true)}
          title="查看并处理项目的所有 Pull Requests"
          style={{ position: 'relative' }}
        >
          🔀 PR
          {openPRCount > 0 && (
            <span
              style={{
                marginLeft: 6,
                background: '#22c55e',
                color: '#fff',
                borderRadius: 10,
                padding: '1px 6px',
                fontSize: 11,
                fontWeight: 700,
              }}
            >
              {openPRCount}
            </span>
          )}
        </button>

        <PluginSlot
          name="project.toolbar"
          inline
          context={{ projectId: id, projectName: p?.name, runnerId: p?.runnerId }}
        />
        <button
          className="ghost"
          onClick={() => setReadmeOpen(true)}
          title="查看项目各仓库的 README"
        >
          📖 README
        </button>

        <button
          className="ghost"
          onClick={() => setKnowledgeOpen(true)}
          title="查看与搜索项目知识库、本地文档与 GitHub Wiki"
        >
          📚 知识库
        </button>

        <button
          className="ghost"
          onClick={() => {
            const curMode = p?.devMode || 'single_main';
            setCfgDevMode(curMode);
            setCfgTargetBranch(p?.targetBranch || (curMode === 'main_dev' ? 'dev' : 'main'));
            setCfgRunnerID(p?.runnerId || '');
            setCfgAutoPR(p?.autoPrOnConfirm ?? true);
            setCfgAutoDevelop(p?.autoDevelop ?? false);
            setCfgAutoDevelopAgent(p?.autoDevelopAgent ?? '');
            setCfgAutoDevelopLabels((Array.isArray(p?.autoDevelopLabels) ? p.autoDevelopLabels : ['bug', 'fix', 'error', 'crash', 'regression', 'enhancement', 'feature', '需求', '缺陷', '优化', '功能', '报错', '崩溃']).join(', '));
            setCfgAutoDevelopText(p?.autoDevelopText ?? false);
            setCfgAutoDevelopAuthors((p?.autoDevelopAuthors ?? []).join(', '));
            setCfgAutoDevelopDailyLimit(p?.autoDevelopDailyLimit || 5);
            setCfgAutomationRules(p?.automationRules ?? []);
            setCfgPullBranches((p?.pullBranches ?? []).join(', '));
            setSettingsTab('mode');
            setSettingsOpen(true);
          }}
          title={`项目设置：${p?.devMode === 'main_dev' ? 'main+dev (PR)' : '单一 main'} 模式，自动化与 Hooks`}
        >
          ⚙️ 设置
          {(p?.automationRules?.filter((r) => r.enabled)?.length ?? 0) > 0 && (
            <span
              style={{
                marginLeft: 5,
                background: 'rgba(110, 123, 242, 0.2)',
                color: 'var(--accent)',
                padding: '1px 6px',
                borderRadius: 999,
                fontSize: 11,
                fontWeight: 700,
              }}
            >
              {p?.automationRules?.filter((r) => r.enabled)?.length}
            </span>
          )}
        </button>

      </div>

      <PluginSlot
        name="project.header"
        context={{ projectId: id, projectName: p?.name, runnerId: p?.runnerId }}
        marginBottom={0}
      />

      {/* 仓库与分支监控双栏面板 */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(420px, 1fr))',
          gap: 12,
        }}
      >
        <div className="panel">
          <div className="panel-h">
            <h3 className="grow">📦 关联仓库 ({(p?.repos ?? []).length})</h3>
            <button
              className="sm"
              title="自动检测本地仓库是否已添加远程或推送到 GitHub/GitLab 平台并自动更新识别"
              disabled={refreshReposMut.isPending}
              onClick={() => refreshReposMut.mutate()}
            >
              {refreshReposMut.isPending ? '刷新中…' : '↻ 刷新'}
            </button>
            <button
              className="sm"
              disabled={(p?.repos ?? []).length === 0}
              onClick={() => setBranchCleanupOpen(true)}
              title="清理已经合并到默认分支、但尚未删除的远程分支（只显示你的提交）"
            >
              🧹 清理已合并分支
            </button>
            <button className="sm" onClick={() => setPickerOpen(true)}>
              📂 选择仓库
            </button>
            <button className="sm" onClick={() => setRepoOpen(true)}>
              + 添加
            </button>
          </div>
          <div className="panel-b repo-list">
            {(p?.repos ?? []).length === 0 && <span className="dim">尚未添加仓库</span>}
            {p?.repos?.map((r) => (
              <span
                key={r.id}
                className="repo-chip"
                title={`${r.remoteUrl}\n测试命令：${r.testCommand || '无'}`}
              >
                <b>{r.name}</b>
                <span className="dim">{r.provider}</span>
                {r.ownerRepo && <span className="muted mono">{r.ownerRepo}</span>}
                <span className="dim mono">@{r.defaultBranch}</span>
                <button
                  className="ghost sm"
                  title="删除仓库"
                  onClick={() => confirm(`确认删除仓库 ${r.name}？`) && delRepo.mutate(r.id)}
                >
                  ✕
                </button>
              </span>
            ))}
          </div>
        </div>

        {/* 分支监控 Watch + Notification */}
        <div className="panel">
          <div className="panel-h" style={{ flexWrap: 'wrap' }}>
            <h3 className="grow">👁 最新推送 ({watches.length})</h3>
            {(p?.repos ?? []).length > 0 && (
              <div className="row" style={{ gap: 6 }}>
                <select
                  className="sm"
                  style={{ width: 130 }}
                  value={watchRepoId || p?.repos?.[0]?.id || 0}
                  onChange={(e) => setWatchRepoId(Number(e.target.value))}
                >
                  {(p?.repos ?? []).map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.name}
                    </option>
                  ))}
                </select>
                <input
                  className="sm mono"
                  style={{ width: 140 }}
                  placeholder="分支名 (如 main/dev)"
                  value={watchBranch}
                  onChange={(e) => setWatchBranch(e.target.value)}
                />
                <button
                  className="sm"
                  disabled={addWatchMut.isPending}
                  onClick={() => addWatchMut.mutate()}
                >
                  + 监控
                </button>
              </div>
            )}
          </div>
          <div className="panel-b" style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {watches.length === 0 ? (
              <span className="dim small">
                添加要监控的分支（例如同事分支或上游 dev 分支），有新代码推送时自动记录、推送通知，并可触发自动化规则，支持点击查看提交详情与平台直达。
              </span>
            ) : (
              watches.map((w) => {
                const repoObj = p?.repos?.find((r) => r.id === w.repoId);
                const branchUrl = getBranchWebUrl(repoObj, w.branch);
                const commitUrl = getCommitWebUrl(repoObj, w.lastSha);

                return (
                  <div
                    key={w.id}
                    className="row"
                    style={{
                      padding: '5px 10px',
                      borderRadius: 6,
                      border: '1px solid var(--border)',
                      background: 'var(--bg-2)',
                      gap: 8,
                      alignItems: 'center',
                      flexWrap: 'wrap',
                    }}
                  >
                    <span className="mono small" style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                      <b>{w.repoName}</b>:
                      {branchUrl ? (
                        <a
                          href={branchUrl}
                          target="_blank"
                          rel="noreferrer"
                          style={{ color: 'var(--accent)', textDecoration: 'none', fontWeight: 600 }}
                          title="在代码平台网页端打开此分支"
                        >
                          {w.branch} ↗
                        </a>
                      ) : (
                        <span>{w.branch}</span>
                      )}
                    </span>

                    {w.lastSha ? (
                      commitUrl ? (
                        <a
                          href={commitUrl}
                          target="_blank"
                          rel="noreferrer"
                          className="mono small"
                          style={{
                            color: 'var(--accent)',
                            textDecoration: 'none',
                            background: 'var(--bg-1)',
                            padding: '1px 6px',
                            borderRadius: 4,
                            border: '1px solid var(--border)',
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: 2,
                          }}
                          title={`在平台打开此 Commit (${w.lastSha})`}
                        >
                          {w.lastSha.slice(0, 7)} ↗
                        </a>
                      ) : (
                        <code
                          className="mono small"
                          style={{
                            color: 'var(--accent)',
                            background: 'var(--bg-1)',
                            padding: '1px 6px',
                            borderRadius: 4,
                            border: '1px solid var(--border)',
                            cursor: 'pointer',
                          }}
                          title={w.lastSha}
                          onClick={() => setCommitsModal({ repoId: w.repoId, branch: w.branch, sha: w.lastSha })}
                        >
                          {w.lastSha.slice(0, 7)}
                        </code>
                      )
                    ) : (
                      <span className="mono small dim">—</span>
                    )}

                    <span
                      className="small muted ellipsis grow"
                      style={{ cursor: 'pointer' }}
                      title={`点击查看详情: ${w.lastMessage || '无提交信息'}`}
                      onClick={() => setCommitsModal({ repoId: w.repoId, branch: w.branch, sha: w.lastSha })}
                    >
                      {w.lastMessage || '无提交信息'}{' '}
                      {w.lastAuthor && <span className="dim">({w.lastAuthor})</span>}
                    </span>

                    <span className="small dim">{fmtTime(w.updatedAt)}</span>

                    <button
                      className="sm ghost"
                      title="查看该分支的历史提交记录与详细信息"
                      onClick={() => setCommitsModal({ repoId: w.repoId, branch: w.branch, sha: w.lastSha })}
                    >
                      📜 提交记录
                    </button>

                    <button
                      className="sm ghost"
                      title="取消监控"
                      onClick={() => delWatchMut.mutate(w.id)}
                    >
                      ✕
                    </button>
                  </div>
                );
              })
            )}
          </div>
        </div>
      </div>
      </div>

      {projectView === 'backlog' ? (
        <BacklogPanel projectId={id} issues={allIssues} />
      ) : (
      <>
      {/* 4 列全宽工作流看板：撑满全屏、外框固定、内部纵向滚动 */}
      <div className="board">
        {WORKFLOW_COLUMNS.map((col) => {
          const list = allIssues.filter((it) => col.statuses.includes(it.status));
          const isDoneCol = col.id === 'done';
          const displayList = isDoneCol ? list.slice(0, 10) : list;
          const isOver = dragOverCol === col.id;
          return (
            <div
              key={col.id}
              className={`board-col${isOver ? ' drag-over' : ''}`}
              onDragOver={(e) => {
                e.preventDefault();
                e.dataTransfer.dropEffect = 'move';
                if (dragOverCol !== col.id) setDragOverCol(col.id);
              }}
              onDragLeave={() => {
                if (dragOverCol === col.id) setDragOverCol(null);
              }}
              onDrop={(e) => {
                e.preventDefault();
                setDragOverCol(null);
                const raw = e.dataTransfer.getData('text/plain');
                const droppedId = Number(raw) || draggingId;
                setDraggingId(null);
                if (droppedId) {
                  handleDropToColumn(col, droppedId);
                }
              }}
            >
              <div className="board-col-h" style={{ flexDirection: 'column', alignItems: 'stretch', gap: 2 }}>
                <div className="row">
                  <span className="grow">{col.title}</span>
                  {col.id === 'todo' && (
                    <div className="row" style={{ gap: 4, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                      <button
                        type="button"
                        className="sm ghost"
                        onClick={() => {
                          const onlyRepo = (p?.repos ?? []).length === 1 ? p?.repos?.[0] : undefined;
                          setIssueRepoId(onlyRepo?.id ?? 0);
                          setSyncRemote(Boolean(onlyRepo && onlyRepo.provider !== 'plain'));
                          setIssueLabels('');
                          setIssuePriority('normal');
                          setIssueFiles([]);
                          setIssueOpen(true);
                        }}
                        title="新建一个待办 Issue"
                      >
                        + 新建
                      </button>
                      <button
                        type="button"
                        className="sm ghost"
                        disabled={sync.isPending}
                        onClick={() => sync.mutate()}
                        title="立即从远端平台同步 Issue 到待办池"
                      >
                        {sync.isPending ? '同步中…' : '↻ 同步'}
                      </button>
                      {list.length > 0 && (
                        <button
                          type="button"
                          className="sm primary"
                          disabled={batchRunMut.isPending}
                          onClick={() => batchRunMut.mutate()}
                          title={`使用 ${defaultAgent} 一键自动执行本列所有待办任务`}
                        >
                          {batchRunMut.isPending ? '启动中…' : '⚡ 自动执行待办'}
                        </button>
                      )}
                    </div>
                  )}
                  {isDoneCol ? (
                    <span
                      className="badge badge-clickable"
                      onClick={() => setDoneModalOpen(true)}
                      title="点击查看全部历史已完成任务，支持搜索与多选批量管理/删除"
                    >
                      {list.length}
                    </span>
                  ) : (
                    <span className="badge">{list.length}</span>
                  )}
                </div>
                <div className="small dim" style={{ fontWeight: 400, textTransform: 'none' }}>
                  {col.subtitle}
                </div>
              </div>

              <div className="board-col-b">
                {list.length === 0 && (
                  <div className="empty small" style={{ padding: 20 }}>
                    {col.id === 'running' ? '将待办卡片拖入此处即可自动启动执行' : '暂无任务'}
                  </div>
                )}
                {displayList.map((it) => (
                  <div
                    key={it.id}
                    draggable
                    onDragStart={(e) => {
                      setDraggingId(it.id);
                      e.dataTransfer.effectAllowed = 'move';
                      e.dataTransfer.setData('text/plain', String(it.id));
                    }}
                    onDragEnd={() => {
                      setDraggingId(null);
                      setDragOverCol(null);
                    }}
                    className={`issue-card${it.awaiting ? ' awaiting' : ''}${
                      draggingId === it.id ? ' dragging' : ''
                    }`}
                  >
                    <div className="issue-card-top">
                      <span className="issue-card-repo">
                        {it.repoId
                          ? (p?.repos ?? []).find((r) => r.id === it.repoId)?.name || 'repo'
                          : '未绑定仓库'}
                        {it.repoId && (
                          <span
                            className={`issue-card-sync ${it.source === 'remote' ? 'synced' : 'pending'}`}
                            title={it.source === 'remote' ? `已同步远程${it.remoteNumber ? ` #${it.remoteNumber}` : ''}` : '尚未同步到远程平台'}
                          >
                            {it.source === 'remote' ? '☁️' : '⚠️'}
                          </span>
                        )}
                      </span>
                      <span className="issue-card-links">
                        <Link to={`/issues/${it.id}`} className="issue-card-link" title="打开 Issue 详情">
                          详情
                        </Link>
                        {it.remoteUrl && (
                          <a
                            className="issue-card-link"
                            href={it.remoteUrl}
                            target="_blank"
                            rel="noreferrer"
                            title="在远程平台打开"
                          >
                            外链 ↗
                          </a>
                        )}
                      </span>
                    </div>
                    <div className="issue-card-title" title={`${it.key} · ${ISSUE_STATUS_LABEL[it.status]}`}>{it.title}</div>
                    <div className="issue-card-summary">{summarizeIssue(it)}</div>
                    <div className="issue-card-footer">
                      <div className="issue-card-meta">
                        {(it.labels ?? []).slice(0, 3).map((label) => (
                          <span className="badge" key={label}>{label}</span>
                        ))}
                        {(it.labels?.length ?? 0) > 3 && <span className="dim">+{it.labels.length - 3}</span>}
                        {((col.id === 'todo' && it.status === 'backlog') ||
                          (col.id === 'running' && it.status === 'failed') ||
                          (col.id === 'review' && it.status === 'pr_open')) && (
                          <span className="issue-card-status">{ISSUE_STATUS_LABEL[it.status]}</span>
                        )}
                        <span className={`badge priority-${it.priority || 'normal'}`}>
                          {PRIORITY_LABEL[it.priority] ?? '普通'}
                        </span>
                        {it.awaiting && <span className="badge awaiting">待审批</span>}
                        {it.parentIssueId && <span className="badge">子任务</span>}
                      </div>
                      <span className="issue-card-time">{fmtTime(it.updatedAt)}</span>
                    </div>
                  </div>
                ))}
                {isDoneCol && list.length > 10 && (
                  <div
                    className="board-col-more"
                    onClick={() => setDoneModalOpen(true)}
                    title="点击查看全部历史已完成任务"
                  >
                    仅展示最近 10 条 · 点击查看全部历史 ({list.length}) ➔
                  </div>
                )}
              </div>
            </div>
          );
        })}
      </div>
      </>
      )}

      {repoOpen && (
        <Modal
          title="添加仓库"
          onClose={() => setRepoOpen(false)}
          footer={
            <>
              <button onClick={() => setPickerOpen(true)}>📂 选择本地目录自动填充…</button>
              <span className="grow" />
              <button onClick={() => setRepoOpen(false)}>取消</button>
              <button
                className="primary"
                disabled={!repo.name.trim() || !repo.remoteUrl.trim() || addRepo.isPending}
                onClick={() => addRepo.mutate()}
              >
                添加
              </button>
            </>
          }
        >
          <div className="repo-form">
            <label className="field">
              目录名 name
              <input autoFocus value={repo.name} placeholder="frontend" onChange={(e) => setR('name', e.target.value)} />
            </label>
            <label className="field">
              平台 provider
              <select value={repo.provider} onChange={(e) => setR('provider', e.target.value as Provider)}>
                {PROVIDERS.map((x) => (
                  <option key={x} value={x}>
                    {x}
                  </option>
                ))}
              </select>
            </label>
            <label className="field">
              默认分支
              <input value={repo.defaultBranch} onChange={(e) => setR('defaultBranch', e.target.value)} />
            </label>
          </div>
          <label className="field">
            Remote URL / 本地路径
            <input
              value={repo.remoteUrl}
              placeholder="git@github.com:owner/name.git 或 /path/to/local/repo"
              onChange={(e) => setR('remoteUrl', e.target.value)}
            />
          </label>
          {repo.provider !== 'plain' && (
            <label className="field">
              owner/repo
              <input value={repo.ownerRepo} placeholder="owner/name" onChange={(e) => setR('ownerRepo', e.target.value)} />
            </label>
          )}
          <label className="field">
            测试命令（可选）
            <input value={repo.testCommand} placeholder="pnpm test" onChange={(e) => setR('testCommand', e.target.value)} />
          </label>
        </Modal>
      )}

      {pickerOpen && <LocalDirPickerModal onClose={() => setPickerOpen(false)} onPick={handlePickedLocal} />}

      {cpOpen && p && (
        <CherryPickModal
          projectId={id}
          repos={p.repos ?? []}
          initialRepoId={cpInitRepoId}
          initialSourceBranch={cpInitBranch}
          onClose={() => setCpOpen(false)}
        />
      )}

      {issueOpen && (
        <Modal
          title="新建 Issue"
          onClose={closeIssueModal}
          footer={
            <>
              <label className="row small" style={{ gap: 6, cursor: 'pointer', marginRight: 'auto' }}>
                <input
                  type="checkbox"
                  checked={autoRunNew}
                  onChange={(e) => setAutoRunNew(e.target.checked)}
                />
                创建后立即启动 Agent ({defaultAgent}) 自动执行
              </label>
              <button onClick={closeIssueModal}>取消</button>
              <button
                className="primary"
                disabled={!title.trim() || createIssue.isPending}
                onClick={() => createIssue.mutate()}
              >
                {autoRunNew ? '创建并立即执行' : '创建'}
              </button>
            </>
          }
        >
          {(p?.repos ?? []).length > 0 && (
            <label className="field">
              目标仓库
              <select
                value={issueRepoId}
                onChange={(e) => {
                  const next = Number(e.target.value);
                  setIssueRepoId(next);
                  const repo = (p?.repos ?? []).find((r) => r.id === next);
                  setSyncRemote(Boolean(repo && repo.provider !== 'plain'));
                }}
              >
                <option value={0}>不绑定仓库 / 由 Agent 判断</option>
                {(p?.repos ?? []).map((repo) => (
                  <option key={repo.id} value={repo.id}>
                    {repo.name} · {repo.provider} · {repo.ownerRepo || repo.remoteUrl}
                  </option>
                ))}
              </select>
              <span className="small dim">
                选择后只检出该仓库，Agent 只能修改它；不选则加载项目全部仓库。
              </span>
            </label>
          )}

          {(p?.repos ?? []).length > 0 && issueRepoId === 0 && (
            <span className="small dim">选择仓库后可勾选同步到远程平台。</span>
          )}
          {issueRepoId > 0 && (
            <label className="row small" style={{ gap: 8, cursor: issueRepoCanSync ? 'pointer' : 'not-allowed' }}>
              <input
                type="checkbox"
                checked={syncRemote && issueRepoCanSync}
                disabled={!issueRepoCanSync}
                onChange={(e) => setSyncRemote(e.target.checked)}
              />
              <span>
                {issueCanSyncText(selectedIssueRepo?.provider)}
                {issueRepoCanSync && ' 创建后自动绑定远程 Issue，避免后续同步重复导入。'}
              </span>
            </label>
          )}

          <label className="field">
            标题
            <input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} />
          </label>

          <div className="row" style={{ gap: 12, alignItems: 'flex-start', flexWrap: 'wrap' }}>
            <div className="grow" style={{ minWidth: 260 }}>
              <LabelPicker value={issueLabels} onChange={setIssueLabels} />
            </div>
            <label className="field" style={{ minWidth: 140 }}>
              紧要程度
              <select value={issuePriority} onChange={(e) => setIssuePriority(e.target.value as IssuePriority)}>
                {ISSUE_PRIORITIES.map((item) => (
                  <option key={item.id} value={item.id}>{item.label}</option>
                ))}
              </select>
            </label>
          </div>

          <label className="field">
            正文（Markdown）
            <textarea rows={8} className="mono" value={body} onChange={(e) => setBody(e.target.value)} />
          </label>

          <div
            className={`attachment-drop${attachDrag ? ' drag-over' : ''}`}
            onDragOver={(e) => {
              e.preventDefault();
              setAttachDrag(true);
            }}
            onDragLeave={() => setAttachDrag(false)}
            onDrop={(e) => {
              e.preventDefault();
              setAttachDrag(false);
              addIssueFiles(e.dataTransfer.files);
            }}
          >
            <label className="field" style={{ margin: 0 }}>
              附件 / 贴图（可拖入，单个最大 10MB）
              <input type="file" multiple onChange={(e) => { addIssueFiles(e.target.files); e.target.value = ''; }} />
            </label>
          </div>
          {issueFiles.length > 0 && (
            <div className="col" style={{ gap: 6 }}>
              {issueFiles.map((draft, index) => (
                <div key={`${draft.file.name}-${index}`} className="row small" style={{ gap: 8 }}>
                  {draft.preview ? (
                    <img src={draft.preview} alt={draft.file.name} style={{ width: 46, height: 46, objectFit: 'cover', borderRadius: 4 }} />
                  ) : (
                    <span>📎</span>
                  )}
                  <span className="grow ellipsis">{draft.file.name}</span>
                  <span className="dim">{Math.ceil(draft.file.size / 1024)} KB</span>
                  <button type="button" className="ghost sm" onClick={() => removeIssueFile(index)}>移除</button>
                </div>
              ))}
            </div>
          )}
        </Modal>
      )}

      {doneModalOpen && (
        <Modal
          title={`✅ 已完成任务历史与批量管理 (${doneIssues.length})`}
          size="xl"
          onClose={() => {
            setDoneModalOpen(false);
            setSelectedDoneIds([]);
            setDoneSearchText('');
          }}
          footer={
            <div className="row" style={{ width: '100%', justifyContent: 'space-between' }}>
              <div className="small muted">
                已勾选 <b>{selectedDoneIds.length}</b> / {filteredDoneIssues.length} 项（总计 {doneIssues.length} 条已完成）
              </div>
              <div className="row" style={{ gap: 8 }}>
                {selectedDoneIds.length > 0 && (
                  <button
                    className="danger"
                    disabled={batchDeleteMut.isPending}
                    onClick={() => {
                      if (
                        window.confirm(
                          `确定要永久删除选中的 ${selectedDoneIds.length} 个已完成任务吗？此操作不可撤销。`,
                        )
                      ) {
                        batchDeleteMut.mutate(selectedDoneIds);
                      }
                    }}
                  >
                    {batchDeleteMut.isPending
                      ? '正在删除…'
                      : `🗑️ 批量删除选中项 (${selectedDoneIds.length})`}
                  </button>
                )}
                <button
                  onClick={() => {
                    setDoneModalOpen(false);
                    setSelectedDoneIds([]);
                    setDoneSearchText('');
                  }}
                >
                  关闭
                </button>
              </div>
            </div>
          }
        >
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div className="row" style={{ gap: 10, flexWrap: 'wrap' }}>
              <input
                style={{ flex: 1, minWidth: 240 }}
                placeholder="按标题、Key、仓库名搜索已完成任务…"
                value={doneSearchText}
                onChange={(e) => setDoneSearchText(e.target.value)}
                autoFocus
              />
              <label
                className="row"
                style={{
                  cursor: 'pointer',
                  userSelect: 'none',
                  fontSize: 13,
                  gap: 6,
                  padding: '4px 8px',
                  borderRadius: 6,
                  border: '1px solid var(--border)',
                  background: 'var(--bg-2)',
                }}
              >
                <input
                  type="checkbox"
                  checked={allFilteredSelected}
                  onChange={toggleSelectAll}
                  disabled={filteredDoneIssues.length === 0}
                />
                <span>全选当前筛选列表 ({filteredDoneIssues.length})</span>
              </label>
            </div>

            <div style={{ maxHeight: '60vh', overflowY: 'auto', border: '1px solid var(--border)', borderRadius: 8 }}>
              {filteredDoneIssues.length === 0 ? (
                <div className="empty small" style={{ padding: 36 }}>
                  {doneSearchText ? '未找到符合条件的已完成任务' : '暂无已完成任务'}
                </div>
              ) : (
                <table className="history-table">
                  <thead>
                    <tr>
                      <th style={{ width: 42, textAlign: 'center' }}>
                        <input
                          type="checkbox"
                          checked={allFilteredSelected}
                          onChange={toggleSelectAll}
                        />
                      </th>
                      <th style={{ width: 110 }}>标识 Key</th>
                      <th>任务标题</th>
                      <th style={{ width: 140 }}>关联仓库</th>
                      <th style={{ width: 150 }}>完成时间</th>
                      <th style={{ width: 80, textAlign: 'right' }}>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredDoneIssues.map((it) => {
                      const isSelected = selectedDoneIds.includes(it.id);
                      const repoName = (p?.repos ?? []).find((r) => r.id === it.repoId)?.name || '未关联';
                      return (
                        <tr key={it.id} className={isSelected ? 'selected' : ''}>
                          <td style={{ textAlign: 'center' }}>
                            <input
                              type="checkbox"
                              checked={isSelected}
                              onChange={() => toggleSelectOne(it.id)}
                            />
                          </td>
                          <td>
                            <code className="mono small" style={{ color: 'var(--accent)' }}>
                              {it.key}
                            </code>
                          </td>
                          <td>
                            <Link
                              to={`/issues/${it.id}`}
                              target="_blank"
                              style={{ fontWeight: 500, color: 'var(--text)' }}
                              title="在新标签页查看详情"
                            >
                              {it.title}
                            </Link>
                          </td>
                          <td>
                            <span className="small muted">📦 {repoName}</span>
                          </td>
                          <td>
                            <span className="small dim">{fmtTime(it.updatedAt)}</span>
                          </td>
                          <td style={{ textAlign: 'right' }}>
                            <button
                              className="sm danger ghost"
                              title="永久删除此任务"
                              disabled={deleteIssueMut.isPending}
                              onClick={() => {
                                if (window.confirm(`确定要永久删除任务「${it.title}」吗？`)) {
                                  deleteIssueMut.mutate(it.id);
                                }
                              }}
                            >
                              🗑️ 删除
                            </button>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        </Modal>
      )}

      {settingsOpen && (
        <Modal
          title="⚙️ 项目设置与自动化系统 (ABC 体系)"
          size="lg"
          onClose={() => setSettingsOpen(false)}
          footer={
            <>
              <button onClick={() => setSettingsOpen(false)}>取消</button>
              <button
                className="primary"
                disabled={!cfgTargetBranch.trim() || updateProjectMut.isPending}
                onClick={() =>
                  updateProjectMut.mutate({
                    devMode: cfgDevMode,
                    targetBranch: cfgTargetBranch.trim(),
                    autoPrOnConfirm: cfgAutoPR,
                    autoDevelop: cfgAutoDevelop,
                    autoDevelopAgent: cfgAutoDevelopAgent,
                    autoDevelopLabels: cfgAutoDevelopLabels.split(',').map((s) => s.trim()).filter(Boolean),
                    autoDevelopText: cfgAutoDevelopText,
                    autoDevelopAuthors: cfgAutoDevelopAuthors.split(',').map((s) => s.trim().replace(/^@/, '')).filter(Boolean),
                    autoDevelopDailyLimit: cfgAutoDevelopDailyLimit,
                    automationRules: cfgAutomationRules,
                    pullBranches: cfgPullBranches.split(',').map((s) => s.trim()).filter(Boolean),
                    runnerId: cfgRunnerID,
                  })
                }
              >
                {updateProjectMut.isPending ? '保存中…' : '保存设置'}
              </button>
            </>
          }
        >
          {/* 顶部 Tab 切换 */}
          <div
            style={{
              display: 'flex',
              gap: 8,
              borderBottom: '1px solid var(--border)',
              paddingBottom: 10,
              marginBottom: 14,
            }}
          >
            <button
              type="button"
              className={settingsTab === 'mode' ? 'primary sm' : 'ghost sm'}
              onClick={() => setSettingsTab('mode')}
              style={{ padding: '6px 14px' }}
            >
              🌿 工作流与分支模式
            </button>
            <button
              type="button"
              className={settingsTab === 'automation' ? 'primary sm' : 'ghost sm'}
              onClick={() => setSettingsTab('automation')}
              style={{ padding: '6px 14px' }}
            >
              ⚡ 自动化与插件体系 (ABC)
              {cfgAutomationRules.filter((r) => r.enabled).length > 0 && (
                <span
                  style={{
                    marginLeft: 6,
                    background: settingsTab === 'automation' ? 'rgba(255,255,255,0.2)' : 'var(--accent)',
                    color: '#fff',
                    borderRadius: 999,
                    padding: '1px 6px',
                    fontSize: 11,
                  }}
                >
                  {cfgAutomationRules.filter((r) => r.enabled).length}
                </span>
              )}
            </button>
          </div>

          {settingsTab === 'mode' ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
              <div>
                <label style={{ fontWeight: 600, marginBottom: 8, display: 'block', fontSize: 13 }}>
                  选择项目开发模式 (Dev Mode)
                </label>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
                  <div
                    onClick={() => {
                      setCfgDevMode('single_main');
                      if (cfgTargetBranch === 'dev') setCfgTargetBranch('main');
                    }}
                    style={{
                      padding: '12px 14px',
                      borderRadius: 8,
                      border: `2px solid ${cfgDevMode === 'single_main' ? 'var(--accent)' : 'var(--border)'}`,
                      background: cfgDevMode === 'single_main' ? 'var(--bg-2)' : 'var(--bg-1)',
                      cursor: 'pointer',
                      transition: 'all 0.15s ease',
                    }}
                  >
                    <div className="row" style={{ gap: 6, fontWeight: 600, marginBottom: 4 }}>
                      <input
                        type="radio"
                        name="dev_mode"
                        checked={cfgDevMode === 'single_main'}
                        onChange={() => {
                          setCfgDevMode('single_main');
                          if (cfgTargetBranch === 'dev') setCfgTargetBranch('main');
                        }}
                      />
                      <span>🌱 单一 main 模式</span>
                    </div>
                    <div className="small muted" style={{ lineHeight: 1.4 }}>
                      改完后<b>默认直接提到本地 main</b>，也可选择提交 PR 到 main。适合单人开发或未配置 CI 打包的项目。
                    </div>
                  </div>

                  <div
                    onClick={() => {
                      setCfgDevMode('main_dev');
                      if (cfgTargetBranch === 'main') setCfgTargetBranch('dev');
                    }}
                    style={{
                      padding: '12px 14px',
                      borderRadius: 8,
                      border: `2px solid ${cfgDevMode === 'main_dev' ? 'var(--accent)' : 'var(--border)'}`,
                      background: cfgDevMode === 'main_dev' ? 'var(--bg-2)' : 'var(--bg-1)',
                      cursor: 'pointer',
                      transition: 'all 0.15s ease',
                    }}
                  >
                    <div className="row" style={{ gap: 6, fontWeight: 600, marginBottom: 4 }}>
                      <input
                        type="radio"
                        name="dev_mode"
                        checked={cfgDevMode === 'main_dev'}
                        onChange={() => {
                          setCfgDevMode('main_dev');
                          if (cfgTargetBranch === 'main') setCfgTargetBranch('dev');
                        }}
                      />
                      <span>🌿 main 和 dev 模式</span>
                    </div>
                    <div className="small muted" style={{ lineHeight: 1.4 }}>
                      <b>必须提 PR</b>！默认提交 PR 到 <code>dev</code> (pr→dev)，避免 main 一提交就自动打包；后续由 dev 合入 main。
                    </div>
                  </div>
                </div>
              </div>

              <label className="field">
                默认目标基准分支 (Target Branch)
                <input
                  className="mono"
                  value={cfgTargetBranch}
                  onChange={(e) => setCfgTargetBranch(e.target.value)}
                  placeholder={cfgDevMode === 'main_dev' ? 'dev' : 'main'}
                />
                <span className="small dim">
                  {cfgDevMode === 'main_dev'
                    ? '💡 双分支模式下推荐为 dev。改动将提交 PR 到此分支，不碰 main，防止触发自动构建。'
                    : '💡 单一 main 模式下默认为 main。改完直接合入此本地分支。'}
                </span>
              </label>

              <label className="field">
                执行节点 (Runner)
                <select
                  className="mono"
                  value={cfgRunnerID}
                  onChange={(e) => setCfgRunnerID(e.target.value)}
                >
                  <option value="">本机执行（内置 Runner）</option>
                  {cfgRunnerID && !(runnersQ.data?.runners ?? []).some((r) => r.id === cfgRunnerID) && (
                    <option value={cfgRunnerID}>{cfgRunnerID}（离线 / 未连接）</option>
                  )}
                  {(runnersQ.data?.runners ?? []).map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.name} {r.online ? '· 在线' : '· 离线'} ({r.version || 'unknown'})
                    </option>
                  ))}
                </select>
                <span className="small dim">
                  选择已连接的远程 Runner 后，本项目的新 Run 会在该节点执行；离线 Runner 无法启动。
                  {remoteProject && remoteRunner?.agentsKnown && (
                    <>
                      {' '}该节点可用 Agent：
                      {remoteAgentNames.size > 0 ? [...remoteAgentNames].join('、') : '未探测到（请在该节点安装 Agent CLI）'}
                    </>
                  )}
                </span>
              </label>

              <div className="row" style={{ gap: 8 }}>
                <span className="small muted">快捷选择常用分支:</span>
                <button
                  type="button"
                  className="sm ghost"
                  onClick={() => setCfgTargetBranch('dev')}
                >
                  dev
                </button>
                <button
                  type="button"
                  className="sm ghost"
                  onClick={() => setCfgTargetBranch('main')}
                >
                  main
                </button>
                {p?.repos?.[0]?.defaultBranch &&
                  p.repos[0].defaultBranch !== 'dev' &&
                  p.repos[0].defaultBranch !== 'main' && (
                    <button
                      type="button"
                      className="sm ghost"
                      onClick={() => setCfgTargetBranch(p.repos[0].defaultBranch)}
                    >
                      {p.repos[0].defaultBranch}
                    </button>
                  )}
              </div>

              <label className="field">
                拉取代码的分支（可选）
                <input
                  className="mono"
                  value={cfgPullBranches}
                  onChange={(e) => setCfgPullBranches(e.target.value)}
                  placeholder="main, dev（留空 = 拉取全部远端分支）"
                />
                <span className="small dim">
                  💡 只拉取这里列出的分支，避免把已合并/废弃的远端分支重新抓回本地；留空保持原有全量拉取。
                </span>
              </label>

              <label className="row" style={{ gap: 8, cursor: 'pointer', marginTop: 4 }}>
                <input
                  type="checkbox"
                  checked={cfgAutoPR}
                  onChange={(e) => setCfgAutoPR(e.target.checked)}
                />
                <span style={{ fontSize: 13 }}>
                  <b>改完验收通过后自动提交/提 PR</b>（根据当前开发模式执行合入或发起 PR，并记录变更）
                </span>
              </label>

              <div
                style={{
                  padding: '12px 14px',
                  borderRadius: 8,
                  border: `1px solid ${cfgAutoDevelop ? 'var(--accent)' : 'var(--border)'}`,
                  background: 'var(--bg-2)',
                }}
              >
                <label className="row" style={{ gap: 8, cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={cfgAutoDevelop}
                    onChange={(e) => setCfgAutoDevelop(e.target.checked)}
                  />
                  <span style={{ fontSize: 13 }}>
                    <b>🤖 自动预开发远程 Issue（只发 PR，不合并）</b>
                  </span>
                </label>
                <div className="small dim" style={{ marginTop: 6, lineHeight: 1.5 }}>
                  新 Issue 同步后先自动判定 Bug / 需求 / 建议：Bug 与需求会打上对应标签并按下方条件启动 Agent
                  预开发，完成后自动创建 PR；建议只打标不启动，等待作者或维护者补充。Agent 仍会先判断是否真实可实施，信息不足时不改代码。
                </div>
                {cfgAutoDevelop && (
                  <div
                    style={{
                      display: 'grid',
                      gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
                      gap: 12,
                      marginTop: 12,
                    }}
                  >
                    <label className="field" style={{ margin: 0 }}>
                      预开发 Agent
                      <select
                        className="sm"
                        value={cfgAutoDevelopAgent}
                        onChange={(e) => setCfgAutoDevelopAgent(e.target.value)}
                      >
                        <option value="">默认（{defaultAgent}）</option>
                        {(agentsQ.data ?? []).map((ag) => (
                          <option key={ag.name} value={ag.name} disabled={!ag.available && !remoteProject}>
                            {ag.name}
                            {ag.available ? '' : remoteProject ? '（本机未安装，远程执行）' : '（不可用）'}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="field" style={{ margin: 0 }}>
                      触发标签 / 关键字（逗号分隔）
                      <input
                        className="sm mono"
                        value={cfgAutoDevelopLabels}
                        onChange={(e) => setCfgAutoDevelopLabels(e.target.value)}
                        placeholder="bug, feature, 需求, 优化"
                      />
                      <span className="small dim" style={{ fontSize: 11, marginTop: 2 }}>
                        匹配 Issue 标签；留空表示不按标签触发。公开仓库建议只按标签匹配。
                      </span>
                    </label>
                    <label className="field" style={{ margin: 0 }}>
                      仅这些作者（自动开发时必填，逗号分隔）
                      <input
                        className="sm mono"
                        value={cfgAutoDevelopAuthors}
                        onChange={(e) => setCfgAutoDevelopAuthors(e.target.value)}
                        placeholder="alice, bob"
                      />
                      <span className="small dim" style={{ fontSize: 11, marginTop: 2 }}>
                        GitHub Issue 作者 login；留空时本项目不会自动开发。
                      </span>
                    </label>
                    <label className="field" style={{ margin: 0 }}>
                      每日自动预开发上限
                      <input
                        type="number"
                        min={1}
                        max={100}
                        className="sm mono"
                        value={cfgAutoDevelopDailyLimit}
                        onChange={(e) => setCfgAutoDevelopDailyLimit(Number(e.target.value) || 5)}
                      />
                      <span className="small dim" style={{ fontSize: 11, marginTop: 2 }}>
                        每项目每天最多启动的自动预开发 Run 数，防止批量 Issue 耗尽 Token。
                      </span>
                    </label>
                    <label className="row" style={{ gap: 8, cursor: 'pointer', gridColumn: '1 / -1' }}>
                      <input
                        type="checkbox"
                        checked={cfgAutoDevelopText}
                        onChange={(e) => setCfgAutoDevelopText(e.target.checked)}
                      />
                      <span className="small">
                        同时匹配标题/正文关键字（<b>公开仓库慎用</b>：正文可由任意人编写，可能触发提示注入与无效开发）
                      </span>
                    </label>
                  </div>
                )}
              </div>

              <div
                style={{
                  marginTop: 14,
                  padding: '12px 14px',
                  borderRadius: 8,
                  border: '1px solid var(--border)',
                  background: 'var(--bg-2)',
                }}
              >
                <div className="row" style={{ alignItems: 'center', gap: 10 }}>
                  <div className="grow">
                    <div style={{ fontWeight: 600, fontSize: 13 }}>📥 拉取本地代码</div>
                    <div className="small dim" style={{ marginTop: 2 }}>
                      只允许在「本地工作区干净、当前分支就是目标分支、且分支在白名单内」时执行。
                    </div>
                  </div>
                  <button
                    type="button"
                    className="sm"
                    disabled={pullMut.isPending || pullStatusQ.isLoading || !(pullStatusQ.data ?? []).some((x) => x.canPull)}
                    onClick={() => pullMut.mutate()}
                    title="刷新远端镜像；仅当当前分支就是目标分支且工作区干净时才快进本地"
                  >
                    {pullMut.isPending ? '拉取中…' : '📥 拉取代码'}
                  </button>
                </div>
                <div className="col" style={{ gap: 4, marginTop: 8 }}>
                  {(pullStatusQ.data ?? []).map((x) => (
                    <div key={x.repoId} className="small" style={{ color: 'var(--muted)' }}>
                      <b style={{ color: 'var(--text)' }}>{x.repoName}</b>
                      {' · '}
                      {x.local ? `${x.branch || '未知分支'} → ${x.targetBranch}` : '远程镜像仓库'}
                      {!x.canPull && x.reason ? ` · ${x.reason}` : x.canPull ? ' · 可拉取' : ''}
                    </div>
                  ))}
                  {pullStatusQ.isLoading && <span className="small dim">正在检查本地分支状态…</span>}
                </div>
              </div>

              <div
                style={{
                  marginTop: 14,
                  padding: '12px 14px',
                  borderRadius: 8,
                  border: '1px dashed rgba(239, 68, 68, 0.4)',
                  background: 'rgba(239, 68, 68, 0.05)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 12,
                }}
              >
                <div>
                  <div style={{ color: '#ef4444', fontWeight: 600, fontSize: 13, marginBottom: 2 }}>
                    危险操作：删除此项目
                  </div>
                  <div className="small dim">
                    永久清空此项目及其所有 Issue、Worktree 与关联数据，不可恢复。
                  </div>
                </div>
                <button
                  type="button"
                  className="ghost"
                  style={{ color: '#ef4444', borderColor: '#ef4444' }}
                  onClick={() => {
                    setSettingsOpen(false);
                    setDeleteProjectOpen(true);
                  }}
                >
                  彻底删除…
                </button>
              </div>
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
              {/* 简介面板 */}
              <div
                style={{
                  background: 'var(--bg-2)',
                  border: '1px solid var(--border)',
                  borderRadius: 8,
                  padding: '10px 14px',
                  fontSize: 12,
                  lineHeight: 1.6,
                  color: 'var(--text-2)',
                }}
              >
                <b>💡 ABC 自动化架构联动：</b>
                <span style={{ marginLeft: 6 }}>
                  后台每次轮询 PR 时，将按序执行：<b>Layer 1 (A)</b> 声明式无代码规则 → <b>Layer 2 (B)</b> 项目级 Hooks 脚本 → <b>Layer 3 (C)</b> 规范化生态插件。
                </span>
                <div className="small" style={{ marginTop: 6, color: 'var(--text-3)' }}>
                  当前规则保存在本项目，<b>仅对本项目生效</b>；跨项目的通用能力请使用顶部「🧩 插件」。
                </div>
              </div>

              {/* Layer 1: A 声明式规则 */}
              <div>
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    marginBottom: 8,
                  }}
                >
                  <div>
                    <b style={{ fontSize: 14 }}>🅰️ 声明式自动化规则 (内置免代码)</b>
                    <span className="small dim" style={{ marginLeft: 8 }}>
                      识别指定作者的 Report 或特定评论，自动执行合并 PR、完成 Issue 或拉取代码（规则改动即时保存）
                    </span>
                  </div>
                  <button
                    type="button"
                    className="sm ghost"
                    onClick={() => setNewRuleOpen(!newRuleOpen)}
                  >
                    {newRuleOpen ? '取消添加' : '+ 添加规则'}
                  </button>
                </div>

                {/* 添加新规则折叠表单 */}
                {newRuleOpen && (
                  <div
                    style={{
                      background: 'var(--bg-2)',
                      border: '1px solid var(--accent)',
                      borderRadius: 8,
                      padding: 12,
                      marginBottom: 12,
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 10,
                    }}
                  >
                    <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--accent)' }}>
                      新建自动化规则
                    </div>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
                      <label className="field" style={{ margin: 0 }}>
                        <span className="small">规则名称</span>
                        <input
                          value={newRuleName}
                          onChange={(e) => setNewRuleName(e.target.value)}
                          placeholder="例如: 监控分支新推送自动建 Issue"
                        />
                      </label>
                      <label className="field" style={{ margin: 0 }}>
                        <span className="small">触发事件</span>
                        <select
                          value={newRuleTrigger}
                          onChange={(e) => {
                            const val = e.target.value as 'pr_comment' | 'pr_review' | 'branch_push';
                            setNewRuleTrigger(val);
                            if (val === 'branch_push') {
                              setNewRuleCreateIssue(true);
                              setNewRuleMergePR(false);
                              setNewRuleSetDone(false);
                            } else {
                              setNewRuleMergePR(true);
                              setNewRuleSetDone(true);
                              setNewRuleCreateIssue(false);
                            }
                          }}
                        >
                          <option value="branch_push">🌟 监控分支新推送 (branch_push)</option>
                          <option value="pr_comment">PR 评论 / 检查报告 (pr_comment)</option>
                          <option value="pr_review">PR 审核批准通过 (pr_review)</option>
                        </select>
                      </label>
                    </div>

                    <div style={{ display: 'grid', gridTemplateColumns: newRuleTrigger === 'branch_push' ? '1fr 1fr 1fr' : '1fr 1fr', gap: 10 }}>
                      {newRuleTrigger === 'branch_push' && (
                        <label className="field" style={{ margin: 0 }}>
                          <span className="small">监控分支 (Branch)</span>
                          <input
                            className="mono"
                            value={newRuleBranch}
                            onChange={(e) => setNewRuleBranch(e.target.value)}
                            placeholder="如 main, dev 或正则 (留空匹配所有分支)"
                          />
                        </label>
                      )}
                      <label className="field" style={{ margin: 0 }}>
                        <span className="small">指定人员 / 提交人 (Author)</span>
                        <input
                          value={newRuleAuthor}
                          onChange={(e) => setNewRuleAuthor(e.target.value)}
                          placeholder="例如: qa-team 或 release-bot (留空匹配所有人)"
                        />
                      </label>
                      <label className="field" style={{ margin: 0 }}>
                        <span className="small">正文 / 提交说明正则 (Pattern)</span>
                        <input
                          className="mono"
                          value={newRulePattern}
                          onChange={(e) => setNewRulePattern(e.target.value)}
                          placeholder={newRuleTrigger === 'branch_push' ? '例如: (?i)^feat: 或 (?i)^fix:' : '例如: (?i)status:\\s*passed 或 (?i)LGTM'}
                        />
                      </label>
                    </div>

                    <div>
                      <span className="small" style={{ fontWeight: 600, display: 'block', marginBottom: 6 }}>
                        触发后执行的动作 (Actions):
                      </span>
                      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 14 }}>
                        {newRuleTrigger === 'branch_push' ? (
                          <>
                            <label className="row" style={{ gap: 6, cursor: 'pointer', fontSize: 12 }}>
                              <input
                                type="checkbox"
                                checked={newRuleCreateIssue}
                                onChange={(e) => setNewRuleCreateIssue(e.target.checked)}
                              />
                              <span>自动创建 Issue 跟踪分支推送</span>
                            </label>
                            <label className="row" style={{ gap: 6, cursor: 'pointer', fontSize: 12 }}>
                              <input
                                type="checkbox"
                                checked={newRuleTriggerRun}
                                onChange={(e) => setNewRuleTriggerRun(e.target.checked)}
                              />
                              <span>自动调度 Agent 执行代码分析/审查 (Trigger Run)</span>
                            </label>
                            <label className="row" style={{ gap: 6, cursor: 'pointer', fontSize: 12 }}>
                              <input
                                type="checkbox"
                                checked={newRulePullLocal}
                                onChange={(e) => setNewRulePullLocal(e.target.checked)}
                              />
                              <span>自动刷新拉取本地仓库镜像 (Pull)</span>
                            </label>
                            <label className="row" style={{ gap: 6, cursor: 'pointer', fontSize: 12 }}>
                              <input
                                type="checkbox"
                                checked={newRuleNotify}
                                onChange={(e) => setNewRuleNotify(e.target.checked)}
                              />
                              <span>发送应用内通知</span>
                            </label>
                          </>
                        ) : (
                          <>
                            <label className="row" style={{ gap: 6, cursor: 'pointer', fontSize: 12 }}>
                              <input
                                type="checkbox"
                                checked={newRuleMergePR}
                                onChange={(e) => setNewRuleMergePR(e.target.checked)}
                              />
                              <span>自动合并 PR (Squash Merge)</span>
                            </label>
                            <label className="row" style={{ gap: 6, cursor: 'pointer', fontSize: 12 }}>
                              <input
                                type="checkbox"
                                checked={newRuleSetDone}
                                onChange={(e) => setNewRuleSetDone(e.target.checked)}
                              />
                              <span>自动标记 Issue 为已完成 (Done)</span>
                            </label>
                            <label className="row" style={{ gap: 6, cursor: 'pointer', fontSize: 12 }}>
                              <input
                                type="checkbox"
                                checked={newRulePullLocal}
                                onChange={(e) => setNewRulePullLocal(e.target.checked)}
                              />
                              <span>自动同步拉取本地代码 (Pull)</span>
                            </label>
                            <label className="row" style={{ gap: 6, cursor: 'pointer', fontSize: 12 }}>
                              <input
                                type="checkbox"
                                checked={newRuleNotify}
                                onChange={(e) => setNewRuleNotify(e.target.checked)}
                              />
                              <span>发送应用内通知</span>
                            </label>
                          </>
                        )}
                      </div>

                      {newRuleTrigger === 'branch_push' && newRuleTriggerRun && (
                        <div style={{ marginTop: 10, display: 'grid', gridTemplateColumns: '180px 1fr', gap: 10 }}>
                          <label className="field" style={{ margin: 0 }}>
                            <span className="small">选择执行 Agent</span>
                            <select
                              value={newRuleAgent || defaultAgent}
                              onChange={(e) => setNewRuleAgent(e.target.value)}
                            >
                              {(agentsQ.data ?? []).map((ag) => (
                                <option key={ag.name} value={ag.name} disabled={!ag.available && !remoteProject}>
                                  {ag.name} {!ag.available ? remoteProject ? '(本机未就绪，远程执行)' : '(未就绪)' : ''}
                                </option>
                              ))}
                            </select>
                          </label>
                          <label className="field" style={{ margin: 0 }}>
                            <span className="small">附加指令 / Prompt (可选)</span>
                            <input
                              value={newRulePrompt}
                              onChange={(e) => setNewRulePrompt(e.target.value)}
                              placeholder="例如: 审查最新提交的代码变更，检查是否有破坏性改动或测试遗漏"
                            />
                          </label>
                        </div>
                      )}
                    </div>

                    <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 4, alignItems: 'center' }}>
                      {newRuleTrigger === 'branch_push' &&
                        !newRuleBranch.trim() &&
                        !newRuleAuthor.trim() &&
                        !newRulePattern.trim() && (
                          <span className="small" style={{ color: 'var(--yellow)' }}>
                            为防止全量触发，branch_push 规则至少填写分支/作者/正则之一
                          </span>
                        )}
                      <button
                        type="button"
                        className="sm ghost"
                        onClick={() => setNewRuleOpen(false)}
                      >
                        取消
                      </button>
                      <button
                        type="button"
                        className="sm primary"
                        disabled={
                          !newRuleName.trim() ||
                          (newRuleTrigger === 'branch_push' &&
                            !newRuleBranch.trim() &&
                            !newRuleAuthor.trim() &&
                            !newRulePattern.trim())
                        }
                        onClick={() => {
                          const acts: RuleAction[] = [];
                          if (newRuleTrigger === 'branch_push') {
                            if (newRuleCreateIssue) acts.push({ type: 'create_issue' });
                            if (newRuleTriggerRun) acts.push({ type: 'trigger_run', agent: newRuleAgent || defaultAgent, prompt: newRulePrompt.trim() || undefined });
                            if (newRulePullLocal) acts.push({ type: 'pull_local' });
                            if (newRuleNotify) acts.push({ type: 'notify' });
                          } else {
                            if (newRuleMergePR) acts.push({ type: 'merge_pr', strategy: 'squash' });
                            if (newRuleSetDone) acts.push({ type: 'set_issue_status', status: 'done' });
                            if (newRulePullLocal) acts.push({ type: 'pull_local' });
                            if (newRuleNotify) acts.push({ type: 'notify' });
                          }

                          const newRule: AutomationRule = {
                            id: 'rule_' + Date.now(),
                            name: newRuleName.trim(),
                            enabled: false,
                            trigger: newRuleTrigger,
                            conditions: {
                              branch: newRuleTrigger === 'branch_push' ? (newRuleBranch.trim() || undefined) : undefined,
                              author: newRuleAuthor.trim() || undefined,
                              pattern: newRulePattern.trim() || undefined,
                            },
                            actions: acts,
                          };

                          persistRules([...cfgAutomationRules, newRule]);
                          setNewRuleOpen(false);
                          toast.success(`已添加规则「${newRule.name}」，默认停用，请在列表中确认后启用`);
                        }}
                      >
                        加入规则列表
                      </button>
                    </div>
                  </div>
                )}

                {/* 已有规则列表 */}
                {cfgAutomationRules.length === 0 ? (
                  <div
                    style={{
                      padding: '16px 20px',
                      textAlign: 'center',
                      background: 'var(--bg-1)',
                      border: '1px dashed var(--border)',
                      borderRadius: 8,
                      color: 'var(--text-3)',
                      fontSize: 13,
                    }}
                  >
                    暂无配置的自动化规则。点击右上角「+ 添加规则」快速设置指定人员的 Report 自动合并！
                  </div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                    {cfgAutomationRules.map((rule, idx) => (
                      <div
                        key={rule.id || idx}
                        style={{
                          background: 'var(--bg-2)',
                          border: '1px solid var(--border)',
                          borderRadius: 8,
                          padding: '10px 14px',
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          gap: 12,
                        }}
                      >
                        <div style={{ display: 'flex', alignItems: 'center', gap: 10, flex: 1, minWidth: 0 }}>
                          <input
                            type="checkbox"
                            checked={rule.enabled}
                            onChange={(e) => {
                              const checked = e.target.checked;
                              persistRules(
                                cfgAutomationRules.map((r, i) => (i === idx ? { ...r, enabled: checked } : r))
                              );
                            }}
                            title={rule.enabled ? '已启用 (点击停用)' : '已停用 (点击启用)'}
                          />
                          <div style={{ flex: 1, minWidth: 0 }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                              <b style={{ fontSize: 13, color: rule.enabled ? 'var(--text)' : 'var(--text-3)' }}>
                                {rule.name}
                              </b>
                              <span
                                style={{
                                  fontSize: 11,
                                  background: 'var(--bg-1)',
                                  border: '1px solid var(--border)',
                                  borderRadius: 4,
                                  padding: '1px 6px',
                                  color: 'var(--accent-2)',
                                  fontFamily: 'var(--mono)',
                                }}
                              >
                                {rule.trigger === 'branch_push' ? '🌟 分支推送 (branch_push)' : rule.trigger}
                              </span>
                            </div>
                            <div style={{ fontSize: 12, color: 'var(--text-2)', marginTop: 4 }}>
                              条件:
                              {rule.conditions.branch && (
                                <span style={{ marginLeft: 6 }}>
                                  分支: <code className="mono">{rule.conditions.branch}</code>
                                </span>
                              )}
                              {rule.conditions.author ? (
                                <span style={{ marginLeft: 6 }}>
                                  提交人: <code>@{rule.conditions.author}</code>
                                </span>
                              ) : (
                                !rule.conditions.branch && <span style={{ marginLeft: 6, color: 'var(--text-3)' }}>任意提交人</span>
                              )}
                              {rule.conditions.pattern && (
                                <span style={{ marginLeft: 10 }}>
                                  正则: <code className="mono">{rule.conditions.pattern}</code>
                                </span>
                              )}
                            </div>
                            <div style={{ display: 'flex', gap: 6, marginTop: 4, flexWrap: 'wrap' }}>
                              {rule.actions.map((act, ai) => (
                                <span
                                  key={ai}
                                  style={{
                                    fontSize: 11,
                                    background: 'rgba(76, 183, 130, 0.12)',
                                    color: 'var(--green)',
                                    borderRadius: 4,
                                    padding: '1px 6px',
                                  }}
                                >
                                  {act.type === 'create_issue' && '✓ 自动建 Issue'}
                                  {act.type === 'trigger_run' && `✓ 调度 ${act.agent || 'Agent'}`}
                                  {act.type === 'merge_pr' && '✓ 自动合并 PR'}
                                  {act.type === 'set_issue_status' && `✓ 改为 ${act.status || 'done'}`}
                                  {act.type === 'pull_local' && '✓ 同步本地代码'}
                                  {act.type === 'notify' && '✓ 提醒通知'}
                                </span>
                              ))}
                            </div>
                          </div>
                        </div>

                        <button
                          type="button"
                          className="sm ghost"
                          style={{ color: 'var(--red)', padding: '2px 8px' }}
                          onClick={() => {
                            persistRules(cfgAutomationRules.filter((_, i) => i !== idx));
                          }}
                          title="删除此规则"
                        >
                          ✕
                        </button>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {/* Layer 2: B 项目级 Hooks 脚本 */}
              <div>
                <div style={{ marginBottom: 8 }}>
                  <b style={{ fontSize: 14 }}>🅱️ 项目级生命周期 Hooks 脚本</b>
                  <span className="small dim" style={{ marginLeft: 8 }}>
                    在代码库根目录放置 <code>.xissue/hooks/</code> 脚本自由编写定制逻辑
                  </span>
                </div>

                <div
                  style={{
                    background: 'var(--bg-2)',
                    border: '1px solid var(--border)',
                    borderRadius: 8,
                    padding: 12,
                  }}
                >
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                    {(hooksQ.data ?? []).map((h: HookStatus) => (
                      <div
                        key={h.name}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          padding: '6px 10px',
                          background: 'var(--bg-1)',
                          border: '1px solid var(--border)',
                          borderRadius: 6,
                        }}
                      >
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          <span
                            style={{
                              display: 'inline-block',
                              width: 8,
                              height: 8,
                              borderRadius: '50%',
                              background: h.executable ? 'var(--green)' : h.exists ? 'var(--yellow)' : 'var(--text-3)',
                            }}
                          />
                          <span style={{ fontFamily: 'var(--mono)', fontSize: 13, fontWeight: 600 }}>
                            {h.path}
                          </span>
                          <span style={{ fontSize: 12, color: 'var(--text-3)' }}>({h.description})</span>
                        </div>
                        <span
                          style={{
                            fontSize: 11,
                            padding: '2px 8px',
                            borderRadius: 4,
                            background: h.executable
                              ? 'rgba(76, 183, 130, 0.15)'
                              : h.exists
                                ? 'rgba(242, 201, 76, 0.15)'
                                : 'rgba(255, 255, 255, 0.05)',
                            color: h.executable ? 'var(--green)' : h.exists ? 'var(--yellow)' : 'var(--text-3)',
                          }}
                        >
                          {h.executable ? '✓ 就绪 (可执行)' : h.exists ? '⚠️ 缺少执行权限' : '○ 未创建'}
                        </span>
                      </div>
                    ))}
                  </div>

                  {/* 脚本编写指引与示例代码 */}
                  <div style={{ marginTop: 10, borderTop: '1px solid var(--border)', paddingTop: 10 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
                      <span className="small dim">
                        示例：<code>.xissue/hooks/on-pr-comment.sh</code>（接收 stdin JSON，输出 stdout actions）
                      </span>
                      <button
                        type="button"
                        className="sm ghost"
                        onClick={() => {
                          const script = `#!/usr/bin/env bash\n# xIssue Project Hook: on-pr-comment\npayload=$(cat)\nauthor=$(echo "$payload" | jq -r '.comment.author // empty')\nbody=$(echo "$payload" | jq -r '.comment.body // empty')\n\nif [[ "$author" == "qa-team" ]] && [[ "$body" =~ "PASSED" ]]; then\n  echo '{"actions": [{"type": "merge_pr", "strategy": "squash"}, {"type": "set_issue_status", "status": "done"}, {"type": "pull_local"}]}'\nfi`;
                          void copyText(script).then((ok) => {
                            if (ok) {
                              setCopySuccess(true);
                              setTimeout(() => setCopySuccess(false), 2000);
                            } else {
                              toast.error('复制失败，请手动选择脚本');
                            }
                          });
                        }}
                      >
                        {copySuccess ? '✓ 已复制模板' : '📋 复制脚本模板'}
                      </button>
                    </div>
                  </div>
                </div>
              </div>

              {/* Layer 3: 全局生态插件入口 */}
              <div>
                <div style={{ marginBottom: 8 }}>
                  <b style={{ fontSize: 14 }}>🅲️ 规范化生态插件 (Plugin System)</b>
                </div>
                <div
                  style={{
                    background: 'var(--bg-2)',
                    border: '1px solid var(--border)',
                    borderRadius: 8,
                    padding: 12,
                    fontSize: 12,
                    color: 'var(--text-3)',
                    lineHeight: 1.7,
                  }}
                >
                  插件是全局能力，已统一移至顶部 <b>🧩 插件</b> 管理：启停、README 预览、config.json
                  查看与编辑。插件目录仍为 <code>~/.xissue/plugins/</code>。
                </div>
              </div>
            </div>
          )}
        </Modal>
      )}

      {prModalOpen && <PRModal projectId={id} onClose={() => setPrModalOpen(false)} />}
      {branchCleanupOpen && (
        <BranchCleanupModal
          repos={p?.repos ?? []}
          onClose={() => setBranchCleanupOpen(false)}
        />
      )}
      {commitsModal && (
        <CommitsModal
          projectId={id}
          repoId={commitsModal.repoId}
          branch={commitsModal.branch}
          sha={commitsModal.sha}
          onClose={() => setCommitsModal(null)}
        />
      )}

      {deleteProjectOpen && p && (
        <DeleteProjectModal
          project={{ id: p.id, name: p.name }}
          onClose={() => setDeleteProjectOpen(false)}
          onDeleted={() => nav('/')}
        />
      )}

      {readmeOpen && (
        <ProjectReadmeModal
          projectId={id}
          onClose={() => setReadmeOpen(false)}
          onOpenKnowledge={() => {
            setReadmeOpen(false);
            setKnowledgeOpen(true);
          }}
        />
      )}

      {knowledgeOpen && (
        <KnowledgeBaseModal
          projectId={id}
          projectName={p?.name}
          onClose={() => setKnowledgeOpen(false)}
        />
      )}

      <PluginSlot
        name="project.page"
        context={{ projectId: id, projectName: p?.name, runnerId: p?.runnerId }}
        height={280}
        marginBottom={0}
      />
    </div>
  );
}
