import { describe, expect, it } from 'vitest';
import { dedupePRs, filterPRs, sortPRs } from './prs';

const prs = [
  { number: 1, state: 'open' },
  { number: 2, state: 'merged' },
  { number: 3, state: 'closed' },
];

describe('sortPRs', () => {
  it('puts the newest PR first and uses number as a tie breaker', () => {
    const sorted = sortPRs([
      { number: 1, createdAt: '2026-01-01T00:00:00Z' },
      { number: 3, createdAt: '2026-01-03T00:00:00Z' },
      { number: 2, createdAt: '2026-01-03T00:00:00Z' },
      { number: 4, createdAt: '' },
    ]);
    expect(sorted.map((pr) => pr.number)).toEqual([3, 2, 1, 4]);
  });
});

describe('filterPRs', () => {
  it('returns the cached list for all', () => {
    expect(filterPRs(prs, 'all')).toBe(prs);
  });

  it('filters each completed state without refetching', () => {
    expect(filterPRs(prs, 'open').map((p) => p.number)).toEqual([1]);
    expect(filterPRs(prs, 'merged').map((p) => p.number)).toEqual([2]);
    expect(filterPRs(prs, 'closed').map((p) => p.number)).toEqual([3]);
  });
});

describe('dedupePRs', () => {
  it('keeps one row when two repositories point at the same PR', () => {
    const rows = dedupePRs([
      { number: 7, url: 'https://github.com/acme/h5/pull/7', repoId: 1, repoName: 'h5' } as never,
      { number: 7, url: 'https://github.com/acme/h5/pull/7/', repoId: 2, repoName: 'h5-v2' } as never,
      { number: 8, url: 'https://github.com/acme/h5/pull/8', repoId: 1 } as never,
    ]);
    expect(rows.map((p: { number: number }) => p.number)).toEqual([7, 8]);
  });

  it('falls back to repoId#number when the provider has no URL', () => {
    const rows = dedupePRs([
      { number: 3, repoId: 9 } as never,
      { number: 3, repoId: 9 } as never,
      { number: 3, repoId: 10 } as never,
    ]);
    expect(rows).toHaveLength(2);
  });
});
