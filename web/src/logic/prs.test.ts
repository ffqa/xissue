import { describe, expect, it } from 'vitest';
import { filterPRs } from './prs';

const prs = [
  { number: 1, state: 'open' },
  { number: 2, state: 'merged' },
  { number: 3, state: 'closed' },
];

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
