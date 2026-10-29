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
