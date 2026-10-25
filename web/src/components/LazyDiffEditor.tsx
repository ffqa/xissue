import { lazy, Suspense } from 'react';
import type { DiffEditorProps } from '@monaco-editor/react';

// Load Monaco (and its worker) only when a diff is actually opened.
const MonacoDiffEditor = lazy(async () => {
  await import('../monaco');
  const mod = await import('@monaco-editor/react');
  return { default: mod.DiffEditor };
});

export function LazyDiffEditor(props: DiffEditorProps) {
  return (
    <Suspense fallback={<div className="empty">正在加载 Diff 编辑器…</div>}>
      <MonacoDiffEditor {...props} />
    </Suspense>
  );
}
