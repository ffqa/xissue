import { PageShell } from './common';
import { PRPanel } from './PRPanel';

export function PRModal({ projectId }: { projectId: number }) {
  return (
    <PageShell
      title="🔀 Pull Requests"
      bodyStyle={{ padding: '12px 14px', overflow: 'hidden', display: 'flex', flexDirection: 'column' }}
    >
      <PRPanel projectId={projectId} />
    </PageShell>
  );
}
