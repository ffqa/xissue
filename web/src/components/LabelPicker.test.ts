import { describe, expect, it } from 'vitest';
import { splitLabels, toggleLabel } from './LabelPicker';

describe('issue label helpers', () => {
  it('splits comma and Chinese comma separated labels', () => {
    expect(splitLabels('bug, feature，frontend')).toEqual(['bug', 'feature', 'frontend']);
  });

  it('toggles common labels case-insensitively without duplicates', () => {
    expect(toggleLabel('', 'bug')).toBe('bug');
    expect(toggleLabel('Bug, feature', 'bug')).toBe('feature');
    expect(toggleLabel('bug', 'feature')).toBe('bug, feature');
  });
});
