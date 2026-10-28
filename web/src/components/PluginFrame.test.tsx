// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api, qk } from '../api';
import type { AppSettings, PluginInfo } from '../types';
import { PluginFrame } from './PluginFrame';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const settings = (pluginUISandbox: boolean): AppSettings => ({
  autoUpdate: true,
  pluginUISandbox,
  defaultAgent: '',
  agentRoles: { issueReview: '', issueDevelop: '', prReview: '', autoMerge: '' },
  globalAutomationRules: [],
});

const plugin: PluginInfo = {
  id: 'demo',
  name: 'demo',
  version: '1.0.0',
  apiVersion: 1,
  description: '',
  author: '',
  events: [],
  permissions: [],
  path: '',
  enabled: true,
  hasReadme: false,
  hasConfig: false,
  ui: { entry: 'ui/index.html', title: 'Demo', slots: ['project.page'] },
};

describe('PluginFrame sandbox mode', () => {
  let container: HTMLDivElement;
  let root: Root;
  let qc: QueryClient;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.append(container);
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    qc.setQueryData(qk.settings, settings(false));
    root = createRoot(container);
    vi.spyOn(api, 'settings').mockRejectedValue(new Error('unexpected settings fetch'));
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    qc.clear();
    vi.restoreAllMocks();
  });

  const render = () => {
    act(() => {
      root.render(
        <MemoryRouter>
          <QueryClientProvider client={qc}>
            <PluginFrame plugin={plugin} />
          </QueryClientProvider>
        </MemoryRouter>,
      );
    });
  };
  const iframe = () => {
    const el = container.querySelector('iframe');
    if (!el) throw new Error('iframe not rendered');
    return el;
  };

  it('recreates the iframe when the global switch changes', async () => {
    render();
    const trusted = iframe();
    expect(trusted.getAttribute('sandbox')).toBeNull();

    await act(async () => {
      qc.setQueryData(qk.settings, settings(true));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    const sandboxed = iframe();
    expect(sandboxed).not.toBe(trusted);
    expect(sandboxed.getAttribute('sandbox')).toContain('allow-scripts');

    await act(async () => {
      qc.setQueryData(qk.settings, settings(false));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    const restored = iframe();
    expect(restored).not.toBe(sandboxed);
    expect(restored.getAttribute('sandbox')).toBeNull();
    expect(api.settings).not.toHaveBeenCalled();
  });
});
