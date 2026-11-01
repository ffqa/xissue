import { PageShell } from './common';
import { CommitsPanel } from './CommitsPanel';

export function CommitsModal({
  projectId,
  repoId,
  branch,
  sha,
}: {
  projectId: number;
  repoId?: number;
  branch?: string;
  sha?: string;
}) {
  return (
    <PageShell
      title="📜 提交记录"
      bodyStyle={{ padding: 0, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}
    >
      <CommitsPanel projectId={projectId} initialRepoId={repoId} initialBranch={branch} initialSha={sha} />
    </PageShell>
  );
}
