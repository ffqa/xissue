import { useState } from 'react';
import { Modal } from './common';
import { CommitsPanel } from './CommitsPanel';

export function CommitsModal({
  projectId,
  repoId,
  branch,
  sha,
  onClose,
}: {
  projectId: number;
  repoId?: number;
  branch?: string;
  sha?: string;
  onClose: () => void;
}) {
  const [fullscreen, setFullscreen] = useState(false);

  return (
    <Modal
      title="📜 提交记录"
      size="xl"
      fullscreen={fullscreen}
      style={fullscreen ? undefined : { width: 'min(1400px, 96vw)' }}
      headerActions={
        <button className="ghost sm" onClick={() => setFullscreen((v) => !v)} title={fullscreen ? '退出全屏 (Esc 关闭)' : '全屏查看 (Esc 关闭)'}>
          {fullscreen ? '🗗' : '⛶'}
        </button>
      }
      onClose={onClose}
      bodyStyle={{ height: fullscreen ? undefined : '78vh', minHeight: fullscreen ? 0 : 520, display: 'flex', flexDirection: 'column', overflow: 'hidden', padding: 0 }}
    >
      <CommitsPanel projectId={projectId} initialRepoId={repoId} initialBranch={branch} initialSha={sha} />
    </Modal>
  );
}
