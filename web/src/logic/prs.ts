export type PRFilter = 'open' | 'merged' | 'closed' | 'all';

// filterPRs keeps one cached list for every tab; completed PRs stay available
// without another provider request when the user switches tabs.
export function filterPRs<T extends { state: string }>(prs: T[], state: PRFilter): T[] {
  return state === 'all' ? prs : prs.filter((pr) => pr.state === state);
}

// sortPRs puts the newest PR first. The provider order is not stable across
// GitHub GraphQL, Gitea pagination and cached responses.
export function sortPRs<T extends { createdAt?: string; updatedAt?: string; number: number }>(prs: T[]): T[] {
  const at = (pr: T) => {
    const raw = pr.createdAt || pr.updatedAt || '';
    const time = Date.parse(raw);
    return Number.isFinite(time) ? time : 0;
  };
  return [...prs].sort((a, b) => at(b) - at(a) || b.number - a.number);
}

// dedupePRs keeps one row per pull request. The same remote can be registered
// under two repositories (or two pages of an infinite query); the PR URL is the
// stable identity, with repoId#number as a fallback when the provider omits it.
export function dedupePRs<T extends { url?: string; repoId?: number; number: number }>(prs: T[]): T[] {
  if (prs.length < 2) return prs;
  const seen = new Set<string>();
  const out: T[] = [];
  for (const pr of prs) {
    const key = (pr.url?.trim().replace(/\/+$/, '') || `${pr.repoId ?? ''}#${pr.number}`).toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(pr);
  }
  return out;
}
