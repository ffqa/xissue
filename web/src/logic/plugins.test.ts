import { describe, expect, it } from 'vitest';
import { pluginUIURL, pluginsForSlot } from './plugins';

describe('pluginUIURL', () => {
  it('strips the ui prefix and encodes each path segment', () => {
    expect(pluginUIURL({ id: 'demo', ui: { entry: 'ui/pages/a b.html', title: '', slots: [] } })).toBe(
      '/api/plugins/demo/ui/pages/a%20b.html',
    );
  });

  it('adds the slot query', () => {
    expect(pluginUIURL({ id: 'demo', ui: { entry: 'ui/index.html', title: '', slots: [] } }, 'project.page')).toBe(
      '/api/plugins/demo/ui/index.html?slot=project.page',
    );
  });

  it('falls back to index.html without ui metadata', () => {
    expect(pluginUIURL({ id: 'demo' })).toBe('/api/plugins/demo/ui/index.html');
  });
});

describe('pluginsForSlot', () => {
  const plugins = [
    { id: 'a', enabled: true, ui: { entry: 'ui/index.html', title: '', slots: ['project.page'] } },
    { id: 'b', enabled: false, ui: { entry: 'ui/index.html', title: '', slots: ['project.page'] } },
    { id: 'c', enabled: true, ui: { entry: 'ui/index.html', title: '', slots: ['issue.page'] } },
    { id: 'd', enabled: true },
  ];

  it('returns enabled plugins declaring the slot', () => {
    expect(pluginsForSlot(plugins, 'project.page').map((p) => p.id)).toEqual(['a']);
  });

  it('returns nothing for an unused slot', () => {
    expect(pluginsForSlot(plugins, 'unknown')).toEqual([]);
  });
});
