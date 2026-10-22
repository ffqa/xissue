import type { Repo } from './types';

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

  if (repo.provider === 'github' && ownerRepo) {
    return `https://github.com/${repoPath(ownerRepo)}/commit/${encodeURIComponent(sha)}`;
  }
  if (repo.provider === 'gitlab' && ownerRepo) {
    return `https://gitlab.com/${repoPath(ownerRepo)}/-/commit/${encodeURIComponent(sha)}`;
  }
  if (repo.provider === 'gitea' && ownerRepo) {
    return `https://gitea.com/${repoPath(ownerRepo)}/commit/${encodeURIComponent(sha)}`;
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

  if (repo.provider === 'github' && ownerRepo) {
    return `https://github.com/${repoPath(ownerRepo)}/tree/${encodeURIComponent(branch)}`;
  }
  if (repo.provider === 'gitlab' && ownerRepo) {
    return `https://gitlab.com/${repoPath(ownerRepo)}/-/tree/${encodeURIComponent(branch)}`;
  }
  if (repo.provider === 'gitea' && ownerRepo) {
    return `https://gitea.com/${repoPath(ownerRepo)}/src/branch/${encodeURIComponent(branch)}`;
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
