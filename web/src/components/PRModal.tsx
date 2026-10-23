import { useState } from 'react';
import { Modal } from './common';
import { PRPanel } from './PRPanel';

export function PRModal({ projectId, onClose }: { projectId: number; onClose: () => void }) {
  const [fullscreen, setFullscreen] = useState(false);

  return (
    <Modal
      title="🔀 Pull Requests"
      size="xl"
      fullscreen={fullscreen}
      style={fullscreen ? undefined : { width: 'min(1400px, 96vw)' }}
      headerActions={
        <button className="ghost sm" onClick={() => setFullscreen((v) => !v)} title={fullscreen ? '退出全屏 (Esc 关闭)' : '全屏查看 (Esc 关闭)'}>
          {fullscreen ? '🗗' : '⛶'}
        </button>
      }
      onClose={onClose}
      bodyStyle={{ height: fullscreen ? undefined : '78vh', minHeight: fullscreen ? 0 : 480, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}
    >
      <PRPanel projectId={projectId} />
    </Modal>
  );
}
