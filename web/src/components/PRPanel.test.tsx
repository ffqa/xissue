// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '../api';
import type { ProjectPRPage } from '../types';
import { PRPanel } from './PRPanel';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

describe('PRPanel layout', () => {
  let container: HTMLDivElement;
  let root: Root;
  let qc: QueryClient;

  beforeEach(() => {
    localStorage.clear();
    container = document.createElement('div');
    document.body.append(container);
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    qc.clear();
    vi.restoreAllMocks();
  });

  it('keeps actions on the left and summary on the right', async () => {
    const page: ProjectPRPage = {
      mineKnown: true,
      prs: [{
        number: 7,
        title: 'Fix login flow',
        body: 'Long summary that should be visible on the right side of the card.',
        state: 'open',
        url: 'https://example.test/pr/7',
        headBranch: 'fix/login',
        baseBranch: 'main',
        author: 'alice',
        mine: true,
        labels: [],
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
        repoId: 1,
        repoName: 'web',
        ciState: 'success',
      }],
    };
    vi.spyOn(api, 'projectPRPage').mockResolvedValue(page);

    act(() => {
      root.render(
        <QueryClientProvider client={qc}>
          <PRPanel projectId={1} />
        </QueryClientProvider>,
      );
    });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 20)); });

    const card = container.querySelector('.pr-card');
    expect(card).not.toBeNull();
    const side = card!.querySelector('.pr-side');
    const main = card!.querySelector('.pr-main');
    const body = card!.querySelector('.pr-body');
    expect(side?.querySelector('.pr-actions')).not.toBeNull();
    expect(main?.querySelector('.pr-title')?.textContent).toBe('Fix login flow');
    expect(body?.textContent).toContain('visible on the right');
    expect(side!.compareDocumentPosition(main!)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    expect(main!.compareDocumentPosition(body!)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  });
});
