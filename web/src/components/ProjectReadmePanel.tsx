import { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Markdown } from './common';

interface Props {
  projectId: number;
  onOpenKnowledge: () => void;
}

export function ProjectReadmePanel({ projectId, onOpenKnowledge }: Props) {
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

  if (readmesQ.isLoading) {
    return (
      <section className="panel" style={{ marginBottom: 14 }}>
        <div className="panel-h">
          <h3 className="grow">📖 项目 README</h3>
          <span className="dim small">加载中…</span>
        </div>
      </section>
    );
  }
  if (readmesQ.error || readmes.length === 0) return null;

  const current = readmes.find((r) => r.repoId === selectedId) ?? readmes[0];
  const foundCount = readmes.filter((r) => r.found).length;

  return (
    <section className="panel" style={{ marginBottom: 14 }}>
      <div className="panel-h">
        <h3 className="grow">📖 项目 README</h3>
        <span className="dim small">
          {foundCount}/{readmes.length} 个仓库
        </span>
        <button className="sm ghost" onClick={onOpenKnowledge} title="打开项目知识库，搜索全部文档">
          📚 知识库
        </button>
      </div>
      <div className="panel-b" style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
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
            <div style={{ maxHeight: 440, overflow: 'auto', paddingRight: 6 }}>
              <Markdown>{current.content}</Markdown>
            </div>
          </>
        ) : (
          <span className="dim small">
            {current.repoName} 根目录未找到 README.md；可在“知识库”中查看 docs/ 下的文档。
          </span>
        )}
      </div>
    </section>
  );
}
