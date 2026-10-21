import { useState, useEffect, useMemo } from 'react';
import type { ReactNode } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import type { DocItem } from '../types';
import { Modal, Markdown, fmtTime } from './common';
import { toast } from '../toast';

interface Props {
  projectId: number;
  projectName?: string;
  onClose: () => void;
}

const CATEGORY_META = {
  requirements: { icon: '📋', label: '需求 / 规格', short: '需求' },
  adr: { icon: '🧭', label: 'ADR / 决策', short: 'ADR' },
  reports: { icon: '📊', label: '报告 / 评审', short: '报告' },
  work: { icon: '🗂', label: '工作 / 计划', short: '工作' },
  other: { icon: '📄', label: '其他 / 散落文档', short: '其他' },
} as const;

const CATEGORY_ORDER = ['requirements', 'adr', 'reports', 'work', 'other'] as const;

type CategoryKey = keyof typeof CATEGORY_META;

function normalizeCategory(category?: string): CategoryKey {
  return category && category in CATEGORY_META ? (category as CategoryKey) : 'other';
}

interface DirNode {
  name: string;
  path: string;
  dirs: Map<string, DirNode>;
  docs: DocItem[];
  total: number;
}

function buildDirTree(docs: DocItem[]): DirNode {
  const root: DirNode = { name: '', path: '', dirs: new Map(), docs: [], total: 0 };
  const sorted = [...docs].sort((a, b) => a.path.localeCompare(b.path));
  for (const doc of sorted) {
    const segments = doc.path.replace(/\\/g, '/').split('/').filter(Boolean);
    segments.pop(); // file name
    let node = root;
    for (const seg of segments) {
      let child = node.dirs.get(seg);
      if (!child) {
        child = { name: seg, path: node.path ? `${node.path}/${seg}` : seg, dirs: new Map(), docs: [], total: 0 };
        node.dirs.set(seg, child);
      }
      node = child;
    }
    node.docs.push(doc);
  }
  const finalize = (node: DirNode): number => {
    let total = node.docs.length;
    for (const child of node.dirs.values()) total += finalize(child);
    node.total = total;
    return total;
  };
  finalize(root);
  return root;
}

export function KnowledgeBaseModal({ projectId, projectName, onClose }: Props) {
  const qc = useQueryClient();
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedDocId, setSelectedDocId] = useState<string | null>(null);
  const [isEditing, setIsEditing] = useState(false);
  const [editContent, setEditContent] = useState('');

  // 1. 文档列表查询
  const docsQ = useQuery({
    queryKey: qk.projectDocs(projectId),
    queryFn: () => api.projectDocs(projectId),
  });

  // 2. 搜索查询
  const searchQ = useQuery({
    queryKey: ['docsSearch', projectId, searchQuery],
    queryFn: () => api.searchDocs(projectId, searchQuery, 20),
    enabled: searchQuery.trim().length > 0,
  });

  const docs = docsQ.data ?? [];
  const searchHits = searchQ.data ?? [];
  const [viewMode, setViewMode] = useState<'tree' | 'category'>('tree');
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const dirTree = useMemo(() => buildDirTree(docs), [docs]);
  const toggleGroup = (key: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  // 默认选中第一个文档
  useEffect(() => {
    if (!selectedDocId && docs.length > 0) {
      setSelectedDocId(docs[0].id);
    }
  }, [docs, selectedDocId]);

  // 3. 当前文档详情查询
  const docDetailQ = useQuery({
    queryKey: selectedDocId ? qk.docDetail(projectId, selectedDocId) : ['docDetail', projectId, 'none'],
    queryFn: () => (selectedDocId ? api.docDetail(projectId, selectedDocId) : Promise.reject('no doc')),
    enabled: !!selectedDocId,
  });

  const curDoc = docDetailQ.data;
  const curCategory = curDoc ? CATEGORY_META[normalizeCategory(curDoc.category)] : null;

  // 切换文档时同步编辑框内容并退出编辑态
  useEffect(() => {
    if (curDoc) {
      setEditContent(curDoc.content);
      setIsEditing(false);
    }
  }, [curDoc?.id, curDoc?.content]);

  // 4. 同步 GitHub Wiki
  const syncWikiMut = useMutation({
    mutationFn: () => api.syncWiki(projectId),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: qk.projectDocs(projectId) });
      toast.success(res.message || 'Wiki 同步完成');
    },
    onError: (err: Error) => toast.error(`同步 Wiki 失败: ${err.message}`),
  });

  // 5. 保存文档
  const saveDocMut = useMutation({
    mutationFn: () => {
      if (!selectedDocId) throw new Error('未选择文档');
      return api.saveDoc(projectId, selectedDocId, editContent);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.docDetail(projectId, selectedDocId!) });
      qc.invalidateQueries({ queryKey: qk.projectDocs(projectId) });
      setIsEditing(false);
      toast.success('文档保存成功');
    },
    onError: (err: Error) => toast.error(`保存失败: ${err.message}`),
  });

  // 快捷键 Ctrl+S / Cmd+S 保存，以及 Esc 键优先退出编辑或关闭
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 's') {
        e.preventDefault();
        if (isEditing && !saveDocMut.isPending) {
          saveDocMut.mutate();
        }
        return;
      }
      if (e.key === 'Escape') {
        e.stopPropagation();
        if (isEditing) {
          setIsEditing(false);
        } else {
          onClose();
        }
      }
    };
    window.addEventListener('keydown', handleKeyDown, true);
    return () => window.removeEventListener('keydown', handleKeyDown, true);
  }, [isEditing, saveDocMut, onClose]);

  return (
    <Modal
      title={
        <div className="row" style={{ alignItems: 'center', gap: 10 }}>
          <span>📚 项目知识库与 Wiki</span>
          {projectName && <span className="muted small font-normal">({projectName})</span>}
        </div>
      }
      size="xl"
      onClose={onClose}
      bodyStyle={{
        padding: 0,
        overflow: 'hidden',
        height: '78vh',
        minHeight: 520,
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: '320px 1fr',
          height: '100%',
          minHeight: 0,
          flex: 1,
          overflow: 'hidden',
          background: 'var(--bg-1)',
        }}
      >
        {/* 左侧：搜索 + 列表 */}
        <div
          style={{
            borderRight: '1px solid var(--border)',
            display: 'flex',
            flexDirection: 'column',
            height: '100%',
            minHeight: 0,
            overflow: 'hidden',
            background: 'var(--bg-0)',
          }}
        >
          {/* 搜索框与同步按钮 */}
          <div style={{ padding: '10px 12px', borderBottom: '1px solid var(--border)', display: 'flex', gap: 8, flexShrink: 0 }}>
            <div style={{ position: 'relative', flex: 1 }}>
              <input
                type="text"
                className="sm"
                placeholder="🔍 全文搜索文档..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                style={{ width: '100%', paddingRight: searchQuery ? 24 : 8 }}
              />
              {searchQuery && (
                <button
                  className="ghost sm"
                  style={{ position: 'absolute', right: 4, top: 4, padding: '0 4px', height: 22, lineHeight: '20px' }}
                  onClick={() => setSearchQuery('')}
                >
                  ✕
                </button>
              )}
            </div>
            <button
              className="ghost sm"
              title="拉取并同步 GitHub Wiki 仓库"
              disabled={syncWikiMut.isPending}
              onClick={() => syncWikiMut.mutate()}
              style={{ whiteSpace: 'nowrap' }}
            >
              {syncWikiMut.isPending ? '同步中…' : '🔄 同步 Wiki'}
            </button>
          </div>

          {/* 列表区：设置 minHeight: 0 与独立滚动 */}
          <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '6px 0', WebkitOverflowScrolling: 'touch' }}>
            {searchQuery.trim().length > 0 ? (
              <div>
                <div className="dim small" style={{ padding: '4px 12px 6px' }}>
                  搜索结果 ({searchHits.length})
                </div>
                {searchHits.length === 0 ? (
                  <div className="dim small" style={{ padding: 16, textAlign: 'center' }}>
                    未找到包含 &ldquo;{searchQuery}&rdquo; 的文档
                  </div>
                ) : (
                  searchHits.map((hit) => {
                    const normPath = hit.path.replace(/\\/g, '/');
                    const lastSlash = normPath.lastIndexOf('/');
                    const dirPath = lastSlash !== -1 ? normPath.substring(0, lastSlash) : '';
                    const cat = CATEGORY_META[normalizeCategory(hit.category)];
                    return (
                      <div
                        key={hit.id}
                        onClick={() => setSelectedDocId(hit.id)}
                        style={{
                          padding: '8px 12px',
                          cursor: 'pointer',
                          borderLeft: selectedDocId === hit.id ? '3px solid var(--accent)' : '3px solid transparent',
                          background: selectedDocId === hit.id ? 'var(--bg-2)' : 'transparent',
                          transition: 'background 0.15s ease',
                        }}
                      >
                        {dirPath && (
                          <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginBottom: 2 }}>
                            <span style={{ fontSize: 10, color: 'var(--text-3)', background: 'var(--bg-2)', padding: '1px 5px', borderRadius: 3 }}>
                              📁 {dirPath}
                            </span>
                          </div>
                        )}
                        <div className="row" style={{ justifyContent: 'space-between', alignItems: 'center' }}>
                          <b className="small ellipsis" style={{ maxWidth: 200 }} title={hit.title}>
                            {hit.title}
                          </b>
                          <span className="row" style={{ gap: 3, flexShrink: 0 }}>
                            <span className="badge sm" style={{ fontSize: 9 }} title={cat.label}>
                              {cat.icon} {cat.short}
                            </span>
                            <span className="badge sm" style={{ fontSize: 9 }}>
                              {hit.source === 'github_wiki' ? 'Wiki' : 'Docs'}
                            </span>
                          </span>
                        </div>
                        <div className="muted mono" style={{ fontSize: 11, marginTop: 2, wordBreak: 'break-all' }}>
                          {hit.path}
                        </div>
                        {hit.snippet && (
                          <div className="dim small" style={{ fontSize: 11, marginTop: 4, lineHeight: 1.4 }}>
                            {hit.snippet}
                          </div>
                        )}
                      </div>
                    );
                  })
                )}
              </div>
            ) : docs.length === 0 ? (
              <div className="dim small" style={{ padding: 24, textAlign: 'center', lineHeight: 1.6 }}>
                尚未发现知识库文档。
                <br />
                可在仓库根目录或 <code>docs/</code> 下添加 Markdown 文件，或点击上方「🔄 同步 Wiki」。
              </div>
            ) : (
              <>
                <div
                  style={{
                    display: 'flex',
                    gap: 6,
                    padding: '6px 12px',
                    borderBottom: '1px solid var(--border)',
                    alignItems: 'center',
                  }}
                >
                  <button className={viewMode === 'tree' ? 'primary sm' : 'ghost sm'} onClick={() => setViewMode('tree')}>
                    🗂 目录
                  </button>
                  <button className={viewMode === 'category' ? 'primary sm' : 'ghost sm'} onClick={() => setViewMode('category')}>
                    🏷 分类
                  </button>
                  <span className="dim small" style={{ marginLeft: 'auto' }}>
                    {docs.length} 篇
                  </span>
                </div>
                {viewMode === 'tree' ? renderTree() : renderCategories()}
              </>
            )}
          </div>
        </div>

        {/* 右侧：文档预览与编辑 */}
        <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0, overflow: 'hidden' }}>
          {docDetailQ.isLoading ? (
            <div className="dim" style={{ padding: 40, textAlign: 'center' }}>
              正在加载文档内容…
            </div>
          ) : !curDoc ? (
            <div className="dim" style={{ padding: 40, textAlign: 'center' }}>
              请在左侧选择一篇文档查看
            </div>
          ) : (
            <>
              {/* 顶部标题与操作栏 */}
              <div
                style={{
                  padding: '12px 18px',
                  borderBottom: '1px solid var(--border)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  background: 'var(--bg-0)',
                  flexShrink: 0,
                }}
              >
                <div style={{ minWidth: 0, flex: 1 }}>
                  <div className="row" style={{ alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                    <h3 style={{ margin: 0, fontSize: 16 }} className="ellipsis">
                      {curDoc.title}
                    </h3>
                    <span
                      style={{
                        fontSize: 11,
                        padding: '1px 6px',
                        borderRadius: 4,
                        background: curDoc.source === 'github_wiki' ? 'rgba(59, 130, 246, 0.15)' : 'rgba(34, 197, 94, 0.15)',
                        color: curDoc.source === 'github_wiki' ? '#60a5fa' : '#4ade80',
                        fontWeight: 600,
                      }}
                    >
                      {curDoc.source === 'github_wiki' ? 'GitHub Wiki' : '本地 Docs'}
                    </span>
                    {curCategory && (
                      <span
                        style={{
                          fontSize: 11,
                          padding: '1px 6px',
                          borderRadius: 4,
                          background: 'var(--bg-2)',
                          color: 'var(--text-2)',
                          fontWeight: 600,
                        }}
                        title={curCategory.label}
                      >
                        {curCategory.icon} {curCategory.label}
                      </span>
                    )}
                  </div>
                  <div className="muted mono small" style={{ marginTop: 3 }}>
                    {curDoc.repoName} &middot; <span style={{ color: 'var(--text-1)' }}>{curDoc.path}</span> &middot; 更新于 {fmtTime(curDoc.updatedAt)}
                  </div>
                </div>

                <div className="row" style={{ gap: 8, flexShrink: 0 }}>
                  {isEditing ? (
                    <>
                      <button className="ghost sm" onClick={() => setIsEditing(false)}>
                        取消
                      </button>
                      <button
                        className="primary sm"
                        disabled={saveDocMut.isPending}
                        onClick={() => saveDocMut.mutate()}
                      >
                        {saveDocMut.isPending ? '保存中…' : '💾 保存 (Ctrl+S)'}
                      </button>
                    </>
                  ) : (
                    <button className="ghost sm" onClick={() => setIsEditing(true)}>
                      ✏️ 编辑
                    </button>
                  )}
                </div>
              </div>

              {/* 内容区域：确保 minHeight: 0 与独立滚动条 */}
              <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', WebkitOverflowScrolling: 'touch', padding: isEditing ? 0 : '20px 24px' }}>
                {isEditing ? (
                  <textarea
                    value={editContent}
                    onChange={(e) => setEditContent(e.target.value)}
                    placeholder="在此编辑 Markdown 内容..."
                    style={{
                      width: '100%',
                      height: '100%',
                      padding: '16px 20px',
                      background: 'transparent',
                      color: 'var(--text)',
                      border: 'none',
                      outline: 'none',
                      resize: 'none',
                      fontFamily: 'monospace',
                      fontSize: 13,
                      lineHeight: 1.6,
                      boxSizing: 'border-box',
                    }}
                  />
                ) : (
                  <div style={{ maxWidth: 860, margin: '0 auto', wordBreak: 'break-word' }}>
                    <Markdown>{curDoc.content || '*(此文档为空)*'}</Markdown>
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </Modal>
  );

  function renderGroupHeader(key: string, depth: number, icon: string, label: string, count: number) {
    const isCollapsed = collapsed.has(key);
    return (
      <div
        key={key}
        onClick={() => toggleGroup(key)}
        className="small"
        style={{
          padding: '5px 8px',
          paddingLeft: 8 + depth * 14,
          display: 'flex',
          alignItems: 'center',
          gap: 4,
          cursor: 'pointer',
          userSelect: 'none',
          fontWeight: 600,
          background: 'rgba(255, 255, 255, 0.02)',
        }}
      >
        <span style={{ width: 12, flexShrink: 0 }}>{isCollapsed ? '▶' : '▼'}</span>
        <span className="ellipsis" style={{ flex: 1 }} title={label}>
          {icon} {label}
        </span>
        <span className="dim" style={{ fontSize: 10, flexShrink: 0 }}>
          {count}
        </span>
      </div>
    );
  }

  function renderTree(): ReactNode[] {
    const rows: ReactNode[] = [];
    const walk = (node: DirNode, depth: number) => {
      if (node.docs.length > 0) {
        if (depth === 0) {
          const key = 'docs:root';
          rows.push(renderGroupHeader(key, 0, '📄', '根目录散落文档', node.docs.length));
          if (!collapsed.has(key)) {
            node.docs.forEach((doc) => rows.push(renderDocItem(doc, 1)));
          }
        } else {
          node.docs.forEach((doc) => rows.push(renderDocItem(doc, depth)));
        }
      }
      const children = [...node.dirs.values()].sort((a, b) => a.name.localeCompare(b.name));
      for (const child of children) {
        const key = `dir:${child.path}`;
        rows.push(renderGroupHeader(key, depth, '📁', child.name, child.total));
        if (!collapsed.has(key)) walk(child, depth + 1);
      }
    };
    walk(dirTree, 0);
    return rows;
  }

  function renderCategories(): ReactNode[] {
    const groups: Record<CategoryKey, DocItem[]> = { requirements: [], adr: [], reports: [], work: [], other: [] };
    for (const doc of docs) groups[normalizeCategory(doc.category)].push(doc);

    const rows: ReactNode[] = [];
    for (const key of CATEGORY_ORDER) {
      const items = groups[key];
      if (items.length === 0) continue;
      const groupKey = `cat:${key}`;
      rows.push(renderGroupHeader(groupKey, 0, CATEGORY_META[key].icon, CATEGORY_META[key].label, items.length));
      if (!collapsed.has(groupKey)) {
        items
          .sort((a, b) => a.path.localeCompare(b.path))
          .forEach((doc) => rows.push(renderDocItem(doc, 1, true)));
      }
    }
    return rows;
  }

  function renderDocItem(item: DocItem, depth = 0, showDirTag = false) {
    const isSelected = selectedDocId === item.id;
    const normPath = item.path.replace(/\\/g, '/');
    const lastSlash = normPath.lastIndexOf('/');
    const dirPath = lastSlash !== -1 ? normPath.substring(0, lastSlash) : '';
    const category = normalizeCategory(item.category);
    const meta = CATEGORY_META[category];

    return (
      <div
        key={item.id}
        onClick={() => setSelectedDocId(item.id)}
        style={{
          padding: '8px 12px',
          paddingLeft: 12 + depth * 14,
          cursor: 'pointer',
          borderLeft: isSelected ? '3px solid var(--accent)' : '3px solid transparent',
          background: isSelected ? 'var(--bg-2)' : 'transparent',
          transition: 'background 0.12s ease',
        }}
      >
        {showDirTag && dirPath && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginBottom: 2 }}>
            <span style={{ fontSize: 10, color: 'var(--text-3)', background: 'var(--bg-2)', padding: '1px 5px', borderRadius: 3 }}>
              📁 {dirPath}
            </span>
          </div>
        )}
        <div className="row" style={{ justifyContent: 'space-between', alignItems: 'center', gap: 4 }}>
          <b className="small ellipsis" style={{ maxWidth: 185 }} title={item.title}>
            {item.title}
          </b>
          <span className="row" style={{ gap: 3, flexShrink: 0 }}>
            <span className="badge sm" style={{ fontSize: 9 }} title={meta.label}>
              {meta.icon} {meta.short}
            </span>
            <span className="badge sm" style={{ fontSize: 9 }}>
              {item.source === 'github_wiki' ? 'Wiki' : 'Docs'}
            </span>
          </span>
        </div>
        <div className="muted mono" style={{ fontSize: 11, marginTop: 2, wordBreak: 'break-all' }}>
          {item.path}
        </div>
      </div>
    );
  }
}
