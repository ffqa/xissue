import { describe, expect, it } from 'vitest';
import { projectViewOf } from './nav';

describe('projectViewOf', () => {
  it('returns the board key for a project root', () => {
    expect(projectViewOf('/projects/12')).toBe('');
  });

  it('returns the sub-route key for project pages', () => {
    expect(projectViewOf('/projects/12/settings')).toBe('settings');
    expect(projectViewOf('/projects/12/test-plans')).toBe('test-plans');
  });

  it('returns null outside project routes so no project menu item is active', () => {
    expect(projectViewOf('/issues/7')).toBeNull();
    expect(projectViewOf('/issues/7/terminal')).toBeNull();
    expect(projectViewOf('/settings/general')).toBeNull();
    expect(projectViewOf('/')).toBeNull();
  });
});
