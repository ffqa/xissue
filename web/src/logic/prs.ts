export type PRFilter = 'open' | 'merged' | 'closed' | 'all';

// filterPRs keeps one cached list for every tab; completed PRs stay available
// without another provider request when the user switches tabs.
export function filterPRs<T extends { state: string }>(prs: T[], state: PRFilter): T[] {
  return state === 'all' ? prs : prs.filter((pr) => pr.state === state);
}
