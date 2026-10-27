import { describe, expect, it } from 'vitest';
import {
  availableRemoteAgentNames,
  filterRemoteCapable,
  pickDefaultAgent,
} from './agents';

describe('pickDefaultAgent', () => {
  const agents = [{ name: 'opencode' }, { name: 'codex' }];

  it('keeps a configured agent that is usable', () => {
    expect(pickDefaultAgent('codex', agents)).toBe('codex');
  });

  it('falls back to opencode when the configured agent is unavailable', () => {
    expect(pickDefaultAgent('missing', agents)).toBe('opencode');
  });

  it('uses the first usable agent when opencode is unavailable', () => {
    expect(pickDefaultAgent('missing', [{ name: 'codex' }])).toBe('codex');
  });

  it('keeps the configured agent when the usable list is empty', () => {
    expect(pickDefaultAgent('  codex  ', [])).toBe('codex');
  });

  it('returns opencode when nothing is configured or usable', () => {
    expect(pickDefaultAgent('', [])).toBe('opencode');
  });
});

describe('remote agent capabilities', () => {
  const capabilities = [
    { name: 'opencode', available: true },
    { name: 'codex', available: false },
  ];

  it('collects available remote names only', () => {
    expect([...availableRemoteAgentNames(capabilities)]).toEqual(['opencode']);
  });

  it('filters configured agents by remote availability', () => {
    const got = filterRemoteCapable(
      [{ name: 'opencode' }, { name: 'codex' }],
      capabilities,
      true,
    );
    expect(got.map((a) => a.name)).toEqual(['opencode']);
  });

  it('does not filter before the probe is known', () => {
    const agents = [{ name: 'opencode' }, { name: 'codex' }];
    expect(filterRemoteCapable(agents, capabilities, false)).toBe(agents);
  });
});
