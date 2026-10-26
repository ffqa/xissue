import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Markdown, Modal } from './common';

interface Props {
  projectId: number;
  onClose: () => void;
  onOpenKnowledge: () => void;
}

export function ProjectReadmeModal({ projectId, onClose, onOpenKnowledge }: Props) {
  const readmesQ = useQuery({
    queryKey: qk.projectReadmes(projectId),
    queryFn: () => api.projectReadmes(projectId),
  });
  const readmes = useMemo(() => readmesQ.data ?? [], [readmesQ.data]);
  const [selectedId, setSelectedId] = useState<number | null>(null);

  useEffect(() => {
    if (readmes.length === 0) {
      setSelectedId(null);
      return;
    }
    if (selectedId != null && readmes.some((r) => r.repoId === selectedId)) return;
    setSelectedId((readmes.find((r) => r.found) ?? readmes[0]).repoId);
  }, [readmes, selectedId]);

  const current = readmes.find((r) => r.repoId === selectedId) ?? readmes[0];
  const foundCount = readmes.filter((r) => r.found).length;

  return (
    <Modal
      title="📖 项目 README"
      size="xl"
      onClose={onClose}
      headerActions={
        <>
          {!readmesQ.isLoading && !readmesQ.error && readmes.length > 0 && (
            <span className="dim small">{foundCount}/{readmes.length} 个仓库</span>
          )}
          <button
            className="ghost sm"
            onClick={onOpenKnowledge}
            title="打开项目知识库，搜索全部文档"
          >
            📚 知识库
          </button>
        </>
      }
    >
      {readmesQ.isLoading ? (
        <span className="dim small">加载 README…</span>
      ) : readmesQ.error ? (
        <span className="dim small">加载 README 失败：{(readmesQ.error as Error).message}</span>
      ) : readmes.length === 0 ? (
        <span className="dim small">项目尚未添加仓库，暂无 README。</span>
      ) : (
        <>
          {readmes.length > 1 && (
            <div className="row" style={{ gap: 6, flexWrap: 'wrap' }}>
              {readmes.map((r) => (
                <button
                  key={r.repoId}
                  className={`sm ${r.repoId === current.repoId ? 'primary' : 'ghost'}`}
                  onClick={() => setSelectedId(r.repoId)}
                  title={r.found ? r.path : '根目录未找到 README'}
                >
                  {r.found ? '📄' : '📭'} {r.repoName}
                </button>
              ))}
            </div>
          )}
          {current.found ? (
            <>
              <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
                <span className="dim mono small">{current.path}</span>
                {current.branch && <span className="dim small">@ {current.branch}</span>}
                {current.truncated && <span className="dim small">（内容过长，仅显示前 512 KB）</span>}
              </div>
              <Markdown>{current.content}</Markdown>
            </>
          ) : (
            <span className="dim small">
              {current.repoName} 根目录未找到 README.md；可在“知识库”中查看 docs/ 下的文档。
            </span>
          )}
        </>
      )}
    </Modal>
  );
}
