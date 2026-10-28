import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Modal } from './common';
import { toast } from '../toast';
import { copyText } from '../clipboard';
import type { CherryPickResult, Repo } from '../types';

const PROTECTED_SOURCES = new Set(['main', 'master', 'dev', 'develop', 'production', 'prod', 'staging']);
const isProtectedSource = (branch: string) => PROTECTED_SOURCES.has(branch.trim().toLowerCase());

export function formatChangeRecord(
  items: string[],
  sourceBranch: string,
  targetBranch: string,
  prUrl: string,
  remark: string,
): string {
  const cleaned = items
    .flatMap((it) => it.split('\n'))
    .map((line) =>
      line
        .trim()
        .replace(/^[a-z][.)]\s+/i, '')
        .replace(/^[-*]\s+/, '')
        .trim(),
    )
    .filter(Boolean);

  const list = cleaned.length > 0 ? cleaned : ['代码改动与优化'];
  const lines = ['本次改动：'];
  list.forEach((item, idx) => {
    const letter = idx < 26 ? String.fromCharCode(97 + idx) : `a${idx - 25}`;
    lines.push(` ${letter}. ${item}`);
  });
  lines.push(`分支：${sourceBranch || '(待生成)'} : ${targetBranch || 'main'}`);
  lines.push(`PR：${prUrl || ''}`);
  lines.push(`备注：${remark.trim()}`);
  return lines.join('\n');
}

export function CherryPickModal({
  projectId,
  repos,
  initialRepoId,
  initialSourceBranch,
  initialShas,
  onClose,
}: {
  projectId: number;
  repos: Repo[];
  initialRepoId?: number;
  initialSourceBranch?: string;
  initialShas?: string[];
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const [repoId, setRepoId] = useState<number>(initialRepoId ?? repos[0]?.id ?? 0);
  const currentRepo = repos.find((r) => r.id === repoId) ?? repos[0];

  const branchesQ = useQuery({
    queryKey: qk.repoBranches(repoId),
    queryFn: () => api.repoBranches(repoId),
    enabled: repoId > 0,
  });
  const branches = branchesQ.data ?? [];
  const sourceBranches = branches.filter((branch) => !isProtectedSource(branch));

  const [sourceBranch, setSourceBranch] = useState(
    initialSourceBranch && !isProtectedSource(initialSourceBranch) ? initialSourceBranch : '',
  );
  const [targetBranch, setTargetBranch] = useState(currentRepo?.defaultBranch || 'main');
  const [newBranch, setNewBranch] = useState('');
  const [selectedShas, setSelectedShas] = useState<string[]>(initialShas ?? []);
  const [mode, setMode] = useState<'pr' | 'push'>('pr');
  const [title, setTitle] = useState('');
  const [itemsText, setItemsText] = useState('');
  const [remark, setRemark] = useState('');
  const [result, setResult] = useState<CherryPickResult | null>(null);

  useEffect(() => {
    if (!sourceBranch || isProtectedSource(sourceBranch)) {
      setSourceBranch(sourceBranches[0] ?? '');
      setSelectedShas([]);
    }
  }, [repoId, sourceBranches.join('\u0000'), sourceBranch]);

  const commitsQ = useQuery({
    queryKey: qk.repoCommits(repoId, sourceBranch),
    queryFn: () => api.repoCommits(repoId, sourceBranch, 30),
    enabled: repoId > 0 && !!sourceBranch,
  });
  const commits = commitsQ.data ?? [];

  // When selectedShas changes, auto-populate change items from commit subjects if not manually edited yet
  useEffect(() => {
    if (selectedShas.length === 0 || commits.length === 0) return;
    // Order oldest-to-newest according to commit list
    const chosen = commits
      .filter((c) => selectedShas.includes(c.sha))
      .reverse();
    if (chosen.length > 0) {
      setItemsText(chosen.map((c) => c.subject).join('\n'));
      if (!title) {
        setTitle(`chore(${targetBranch}): ${chosen[0].subject}`);
      }
    }
  }, [selectedShas, commits, targetBranch]);

  const toggleSha = (sha: string) => {
    setSelectedShas((prev) =>
      prev.includes(sha) ? prev.filter((s) => s !== sha) : [...prev, sha],
    );
  };

  const cpMut = useMutation({
    mutationFn: () => {
      // Order selected SHAs oldest-first for clean cherry-pick sequence
      const orderedShas =
        commits.length > 0
          ? commits
              .filter((c) => selectedShas.includes(c.sha))
              .reverse()
              .map((c) => c.sha)
          : selectedShas;
      return api.cherryPick(projectId, {
        repoId,
        sourceBranch,
        targetBranch,
        newBranch: newBranch.trim() || undefined,
        shas: orderedShas,
        mode,
        title: title.trim() || undefined,
        changeItems: itemsText
          .split('\n')
          .map((s) => s.trim())
          .filter(Boolean),
        remark: remark.trim(),
      });
    },
    onSuccess: (res) => {
      setResult(res);
      qc.invalidateQueries({ queryKey: qk.issues(projectId) });
      qc.invalidateQueries({ queryKey: qk.notifications });
      toast.success(
        res.prUrl
          ? `Cherry-Pick PR 创建成功 (${res.issueKey})`
          : `Cherry-Pick 分支 ${res.branch} 推送成功`,
      );
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const previewRecord = result
    ? result.changeRecord
    : formatChangeRecord(
        itemsText.split('\n'),
        newBranch.trim() || `cp/${targetBranch}-<timestamp>`,
        targetBranch,
        mode === 'pr' ? '(创建 PR 后自动填入链接)' : '无 (仅推送分支)',
        remark,
      );

  return (
    <Modal
      title="🍒 Cherry-Pick 改动创建 PR & 生成标准改动记录"
      size="lg"
      onClose={onClose}
      footer={
        result ? (
          <>
            <button
              type="button"
              onClick={() => {
                void copyText(result.changeRecord).then((ok) =>
                  ok ? toast.success('已复制改动记录到剪贴板') : toast.error('复制失败，请手动选择文本')
                );
              }}
            >
              📋 复制改动记录
            </button>
            <button type="button" className="primary" onClick={onClose}>
              完成并关闭
            </button>
          </>
        ) : (
          <>
            <button type="button" onClick={onClose}>
              取消
            </button>
            <button
              type="button"
              className="primary"
              disabled={cpMut.isPending || selectedShas.length === 0 || !repoId || !sourceBranch || isProtectedSource(sourceBranch)}
              onClick={() => cpMut.mutate()}
            >
              {cpMut.isPending
                ? '正在 Cherry-Pick 并推送…'
                : mode === 'pr'
                  ? `🍒 Cherry-Pick (${selectedShas.length}) 并创建 PR`
                  : `🍒 Cherry-Pick (${selectedShas.length}) 并推送分支`}
            </button>
          </>
        )
      }
    >
      {result ? (
        <div className="col" style={{ gap: 12 }}>
          <div className="panel" style={{ padding: 14, borderColor: 'var(--green)' }}>
            <div className="row" style={{ marginBottom: 8 }}>
              <b style={{ color: 'var(--green)' }}>
                ✓ Cherry-Pick 已完成，已关联任务 [{result.issueKey}]
              </b>
              <span className="grow" />
              {result.prUrl && (
                <a href={result.prUrl} target="_blank" rel="noreferrer" className="badge pr-open">
                  打开 PR ↗
                </a>
              )}
            </div>
            <div className="small muted" style={{ marginBottom: 8 }}>
              以下是为您自动生成的标准改动记录（已同步写入 PR 描述与 Issue 备注，可直接复制备查）：
            </div>
            <pre
              className="mono"
              style={{
                background: 'var(--bg)',
                border: '1px solid var(--border)',
                borderRadius: 6,
                padding: 12,
                whiteSpace: 'pre-wrap',
                margin: 0,
              }}
            >
              {result.changeRecord}
            </pre>
          </div>
        </div>
      ) : (
        <div className="col" style={{ gap: 12 }}>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(4, minmax(140px, 1fr))',
              gap: 10,
            }}
          >
            <label className="field">
              目标仓库
              <select
                value={repoId}
                onChange={(e) => {
                  const id = Number(e.target.value);
                  setRepoId(id);
                  setSelectedShas([]);
                  setSourceBranch('');
                  const r = repos.find((x) => x.id === id);
                  if (r) {
                    setTargetBranch(r.defaultBranch || 'main');
                  }
                }}
              >
                {repos.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.name} ({r.provider})
                  </option>
                ))}
              </select>
            </label>

            <label className="field">
              来源分支 (选取提交)
              <input
                className="mono"
                list="cp-source-branches"
                value={sourceBranch}
                onChange={(e) => {
                  setSourceBranch(e.target.value);
                  setSelectedShas([]);
                }}
                placeholder="例如：feature/xxx（不能是 main/dev/develop）"
              />
              <datalist id="cp-source-branches">
                {sourceBranches.map((b) => (
                  <option key={b} value={b} />
                ))}
              </datalist>
              {isProtectedSource(sourceBranch) && (
                <span className="small" style={{ color: 'var(--orange)' }}>不能从主分支/集成分支选取提交</span>
              )}
            </label>

            <label className="field">
              目标基准分支 (Target Base)
              <input
                className="mono"
                list="cp-target-branches"
                value={targetBranch}
                onChange={(e) => setTargetBranch(e.target.value)}
                placeholder="例如：main / release"
              />
              <datalist id="cp-target-branches">
                {branches.map((b) => (
                  <option key={b} value={b} />
                ))}
              </datalist>
            </label>

            <label className="field">
              新建 PR 分支名 (留空自动生成)
              <input
                className="mono"
                value={newBranch}
                onChange={(e) => setNewBranch(e.target.value)}
                placeholder={`cp/${targetBranch}-...`}
              />
            </label>
          </div>

          {/* Commit list */}
          <div className="panel" style={{ padding: 10 }}>
            <div className="row" style={{ marginBottom: 6 }}>
              <b className="small">
                勾选要 Cherry-Pick 的提交（来源：<code className="mono">{sourceBranch}</code>，已选{' '}
                {selectedShas.length} 个）
              </b>
              <span className="grow" />
              <button
                type="button"
                className="sm ghost"
                onClick={() => commitsQ.refetch()}
                disabled={commitsQ.isFetching}
              >
                {commitsQ.isFetching ? '拉取中…' : '↻ 刷新提交'}
              </button>
            </div>
            <div
              style={{
                maxHeight: 170,
                overflowY: 'auto',
                border: '1px solid var(--border)',
                borderRadius: 6,
                background: 'var(--bg)',
              }}
            >
              {commitsQ.isLoading ? (
                <div className="empty small" style={{ padding: 16 }}>
                  正在从仓库拉取最新提交记录…
                </div>
              ) : commits.length === 0 ? (
                <div className="empty small" style={{ padding: 16 }}>
                  未找到分支 {sourceBranch} 的提交记录
                </div>
              ) : (
                commits.map((c) => {
                  const checked = selectedShas.includes(c.sha);
                  return (
                    <label
                      key={c.sha}
                      className="row"
                      style={{
                        padding: '6px 10px',
                        borderBottom: '1px solid var(--border)',
                        cursor: 'pointer',
                        background: checked ? 'var(--accent-bg)' : 'transparent',
                        gap: 8,
                        flexWrap: 'nowrap',
                      }}
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={() => toggleSha(c.sha)}
                        style={{ width: 16, height: 16, flex: 'none', cursor: 'pointer' }}
                      />
                      <code className="mono small" style={{ color: 'var(--accent)', flex: 'none' }}>
                        {c.shortSha}
                      </code>
                      <span className="small grow ellipsis" title={c.subject}>
                        {c.subject}
                      </span>
                      <span className="small dim nowrap" style={{ flex: 'none' }}>
                        {c.author}
                      </span>
                    </label>
                  );
                })
              )}
            </div>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <div className="col" style={{ gap: 8 }}>
              <div className="row" style={{ gap: 8 }}>
                <label className="field grow">
                  PR 标题
                  <input
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                    placeholder="简明扼要的 PR 标题"
                  />
                </label>
                <label className="field" style={{ width: 150 }}>
                  操作方式
                  <select value={mode} onChange={(e) => setMode(e.target.value as 'pr' | 'push')}>
                    <option value="pr">推送并创建 PR</option>
                    <option value="push">仅推送分支</option>
                  </select>
                </label>
              </div>

              <label className="field">
                本次改动要点（每行一条，自动生成 a. / b. / c. 编号，需简明扼要）
                <textarea
                  rows={4}
                  value={itemsText}
                  onChange={(e) => setItemsText(e.target.value)}
                  placeholder={'修复 xxx 问题\n优化 yyy 逻辑'}
                />
              </label>

              <label className="field">
                备注 (可选)
                <input
                  value={remark}
                  onChange={(e) => setRemark(e.target.value)}
                  placeholder="例如：紧急修复线上问题 / 同步自 dev 分支"
                />
              </label>
            </div>

            <div className="col" style={{ gap: 6 }}>
              <div className="row">
                <span className="small muted">标准改动记录实时预览（自动写入 PR 与备查记录）：</span>
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
                  padding: 10,
                  whiteSpace: 'pre-wrap',
                  margin: 0,
                }}
              >
                {previewRecord}
              </pre>
            </div>
          </div>
        </div>
      )}
    </Modal>
  );
}
