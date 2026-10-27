import { describe, expect, it } from 'vitest';
import { isLocalGitPath } from './repos';

describe('isLocalGitPath', () => {
  it.each([
    '/Users/me/repo',
    './repo',
    '../repo',
    '~/repo',
    'file:///Users/me/repo',
    'C:\\repo',
    'd:/repo',
  ])('accepts %s', (value) => {
    expect(isLocalGitPath(value)).toBe(true);
  });

  it.each([
    'https://github.com/acme/demo.git',
    'git@github.com:acme/demo.git',
    'ssh://git@example.com/acme/demo.git',
    '',
  ])('rejects %s', (value) => {
    expect(isLocalGitPath(value)).toBe(false);
  });
});
