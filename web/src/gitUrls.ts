import type { Repo } from './types';

function remoteOrigin(remote: string): string | null {
  const https = remote.match(/^https?:\/\/([^/]+)/i);
  if (https) return https[0].replace(/\/$/, '');
  const ssh = remote.match(/^(?:ssh:\/\/)?[^@/]+@([^:/]+)[:/]/i);
  if (ssh) return `https://${ssh[1]}`;
  return null;
}

function repoPath(ownerRepo: string): string {
  return ownerRepo
    .split('/')
    .filter(Boolean)
    .map((seg) => encodeURIComponent(seg))
    .join('/');
}

export function getCommitWebUrl(repo?: Repo, sha?: string): string | null {
  if (!repo || !sha) return null;
  const ownerRepo = repo.ownerRepo?.trim();
  const remote = repo.remoteUrl?.trim() || '';

  const base = remoteOrigin(remote);
  if (repo.provider === 'github' && ownerRepo) {
    return `${base || 'https://github.com'}/${repoPath(ownerRepo)}/commit/${encodeURIComponent(sha)}`;
  }
  if (repo.provider === 'gitlab' && ownerRepo) {
    return `${base || 'https://gitlab.com'}/${repoPath(ownerRepo)}/-/commit/${encodeURIComponent(sha)}`;
  }
  if (repo.provider === 'gitea' && ownerRepo) {
    return `${base || 'https://gitea.com'}/${repoPath(ownerRepo)}/commit/${encodeURIComponent(sha)}`;
  }

  const match = remote.match(/(?:https?:\/\/|git@)([^/:]+)[:/]([^/]+)\/([^/]+?)(?:\.git)?$/);
  if (match) {
    const host = match[1];
    const owner = match[2];
    const name = match[3];
    return `https://${encodeURIComponent(host)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/commit/${encodeURIComponent(sha)}`;
  }
  return null;
}

export function getBranchWebUrl(repo?: Repo, branch?: string): string | null {
  if (!repo || !branch) return null;
  const ownerRepo = repo.ownerRepo?.trim();
  const remote = repo.remoteUrl?.trim() || '';

  const base = remoteOrigin(remote);
  if (repo.provider === 'github' && ownerRepo) {
    return `${base || 'https://github.com'}/${repoPath(ownerRepo)}/tree/${encodeURIComponent(branch)}`;
  }
  if (repo.provider === 'gitlab' && ownerRepo) {
    return `${base || 'https://gitlab.com'}/${repoPath(ownerRepo)}/-/tree/${encodeURIComponent(branch)}`;
  }
  if (repo.provider === 'gitea' && ownerRepo) {
    return `${base || 'https://gitea.com'}/${repoPath(ownerRepo)}/src/branch/${encodeURIComponent(branch)}`;
  }

  const match = remote.match(/(?:https?:\/\/|git@)([^/:]+)[:/]([^/]+)\/([^/]+?)(?:\.git)?$/);
  if (match) {
    const host = match[1];
    const owner = match[2];
    const name = match[3];
    return `https://${encodeURIComponent(host)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/tree/${encodeURIComponent(branch)}`;
  }
  return null;
}

export function commitPagePath(projectId: number, repoId: number, branch: string, sha?: string): string {
  const params = new URLSearchParams({ repo: String(repoId), branch });
  if (sha) params.set('sha', sha);
  return `/projects/${projectId}/commits?${params.toString()}`;
}
