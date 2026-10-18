import { useEffect, useState } from 'react';
import { api } from '../api';
import { toast } from '../toast';
import type { BrowseResult, InspectResult } from '../types';
import { Modal } from './common';

export function LocalDirPickerModal({
  title = '选择本地目录 / Git 仓库',
  onClose,
  onPick,
}: {
  title?: string;
  onClose: () => void;
  onPick: (inspected: InspectResult) => void;
}) {
  const [curPath, setCurPath] = useState<string>('');
  const [inputPath, setInputPath] = useState<string>('');
  const [browse, setBrowse] = useState<BrowseResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [inspecting, setInspecting] = useState(false);

  const loadDir = async (target?: string) => {
    setLoading(true);
    try {
      const r = await api.browseDir(target);
      setBrowse(r);
      setCurPath(r.path);
      setInputPath(r.path);
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadDir('');
  }, []);

  const pickPath = async (p: string) => {
    setInspecting(true);
    try {
      const res = await api.inspectDir(p);
      onPick(res);
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setInspecting(false);
    }
  };

  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        <>
          <button onClick={onClose}>取消</button>
          <button
            className="primary"
            disabled={!curPath || inspecting}
            onClick={() => pickPath(inputPath.trim() || curPath)}
          >
            {inspecting ? '解析 Git 信息中…' : browse?.isGit ? '选用当前 Git 仓库' : '选用此目录（自动扫描子仓库）'}
          </button>
        </>
      }
    >
      <div className="row">
        {browse?.parent && (
          <button className="sm" title="上级目录" onClick={() => loadDir(browse.parent)}>
            ↑ 上级
          </button>
        )}
        <input
          className="grow mono"
          value={inputPath}
          placeholder="/path/to/repo"
          onChange={(e) => setInputPath(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') loadDir(inputPath.trim());
          }}
        />
        <button className="sm" onClick={() => loadDir(inputPath.trim())}>
          跳转
        </button>
      </div>

      {browse?.isGit && (
        <div className="row small" style={{ padding: '6px 10px', background: '#14261d', border: '1px solid var(--green)', borderRadius: 6 }}>
          <span className="grow">
            当前目录本身就是一个 Git 仓库：<b className="mono">{browse.path}</b>
          </span>
          <button className="primary sm" disabled={inspecting} onClick={() => pickPath(browse.path)}>
            直接选用
          </button>
        </div>
      )}

      <div
        className="panel"
        style={{ maxHeight: 340, overflow: 'auto', divideY: '1px solid var(--border)' } as React.CSSProperties}
      >
        {loading && <div className="empty">正在读取目录…</div>}
        {!loading && (browse?.entries?.length ?? 0) === 0 && <div className="empty">此目录下没有子文件夹</div>}
        {!loading &&
          browse?.entries?.map((e) => (
            <div
              key={e.path}
              className="row"
              style={{
                padding: '7px 10px',
                borderBottom: '1px solid var(--border)',
                cursor: 'pointer',
              }}
              onClick={() => loadDir(e.path)}
            >
              <span>{e.isGit ? '📦' : '📁'}</span>
              <span className="grow mono ellipsis">{e.name}</span>
              {e.isGit && <span className="badge" style={{ color: 'var(--green)' }}>Git 仓库</span>}
              {e.isGit && (
                <button
                  className="primary sm"
                  disabled={inspecting}
                  onClick={(ev) => {
                    ev.stopPropagation();
                    pickPath(e.path);
                  }}
                >
                  选用
                </button>
              )}
            </div>
          ))}
      </div>
      <div className="small dim">
        提示：既可以直接选择一个 Git 仓库目录（单仓），也可以选择包含多个 Git 子仓库的父目录（多仓项目一键导入）。
      </div>
    </Modal>
  );
}
