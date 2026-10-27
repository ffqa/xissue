import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Markdown, Modal } from './common';
import { toast } from '../toast';
import type { PluginInfo } from '../types';

export function PluginManagerModal({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const pluginsQ = useQuery({ queryKey: qk.plugins, queryFn: api.plugins });
  const plugins = pluginsQ.data ?? [];
  const [selectedId, setSelectedId] = useState('');
  const [tab, setTab] = useState<'readme' | 'config'>('readme');
  const [configText, setConfigText] = useState('');

  useEffect(() => {
    if (plugins.length === 0) {
      setSelectedId('');
      return;
    }
    if (!plugins.some((p) => p.id === selectedId)) {
      setSelectedId(plugins[0].id);
    }
  }, [plugins, selectedId]);

  const selected = plugins.find((p) => p.id === selectedId);

  useEffect(() => {
    if (selected && !selected.hasReadme) setTab('config');
    if (selected && !selected.hasConfig && selected.hasReadme) setTab('readme');
  }, [selected]);

  const readmeQ = useQuery({
    queryKey: qk.pluginReadme(selectedId),
    queryFn: () => api.pluginReadme(selectedId),
    enabled: !!selectedId && tab === 'readme' && !!selected?.hasReadme,
    retry: false,
  });

  const configQ = useQuery({
    queryKey: qk.pluginConfig(selectedId),
    queryFn: () => api.pluginConfig(selectedId),
    enabled: !!selectedId && tab === 'config',
  });

  useEffect(() => {
    if (configQ.data) setConfigText(configQ.data.content);
  }, [configQ.data]);

  const toggleMut = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => api.setPluginEnabled(id, enabled),
    onSuccess: (list, vars) => {
      qc.setQueryData(qk.plugins, list);
      qc.invalidateQueries({ queryKey: qk.plugins });
      toast.success(vars.enabled ? '插件已启用' : '插件已停用');
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const saveMut = useMutation({
    mutationFn: () => api.savePluginConfig(selectedId, configText),
    onSuccess: (cfg) => {
      qc.setQueryData(qk.pluginConfig(selectedId), cfg);
      qc.invalidateQueries({ queryKey: qk.plugins });
      toast.success('config.json 已保存');
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const pluginsDir = useMemo(() => {
    const p = plugins[0]?.path;
    if (!p) return '~/.xissue/plugins';
    const idx = Math.max(p.lastIndexOf('/'), p.lastIndexOf('\\'));
    return idx > 0 ? p.slice(0, idx) : p;
  }, [plugins]);

  const validateAndSave = () => {
    try {
      const parsed = JSON.parse(configText);
      if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
        toast.error('config.json 必须是 JSON 对象');
        return;
      }
    } catch (e) {
      toast.error(`JSON 格式错误: ${(e as Error).message}`);
      return;
    }
    saveMut.mutate();
  };

  return (
    <Modal
      title="🧩 全局插件管理"
      size="xl"
      onClose={onClose}
      footer={
        <>
          <span className="small dim grow">
            插件目录：<code className="mono">{pluginsDir}</code>
            {' · '}停用后不会再触发；config.json 保存后立即生效。
          </span>
          <button type="button" onClick={onClose}>
            关闭
          </button>
        </>
      }
      style={{ height: 'min(860px, 92vh)' }}
      bodyStyle={{ display: 'flex', flexDirection: 'column', minHeight: 0 }}
    >
      {pluginsQ.isLoading ? (
        <div className="empty">正在读取插件…</div>
      ) : plugins.length === 0 ? (
        <div className="empty" style={{ lineHeight: 1.8 }}>
          暂未安装插件。
          <br />
          使用 <code>xissue plugin install &lt;name&gt;</code> 安装，或把插件放入{' '}
          <code>{pluginsDir}/&lt;plugin-id&gt;/</code>。
        </div>
      ) : (
        <div className="row" style={{ gap: 14, alignItems: 'stretch', minHeight: 0, flex: 1 }}>
          <div
            className="col"
            style={{
              width: 280,
              flex: 'none',
              gap: 6,
              overflowY: 'auto',
              paddingRight: 4,
              borderRight: '1px solid var(--border)',
            }}
          >
            {plugins.map((p) => (
              <PluginListItem
                key={p.id}
                plugin={p}
                active={p.id === selectedId}
                pending={toggleMut.isPending}
                onSelect={() => setSelectedId(p.id)}
                onToggle={() => toggleMut.mutate({ id: p.id, enabled: !p.enabled })}
              />
            ))}
          </div>

          <div className="col grow" style={{ minWidth: 0, minHeight: 0 }}>
            {selected ? (
              <>
                <div className="row" style={{ gap: 8, flexWrap: 'wrap', alignItems: 'center', flex: 'none' }}>
                  <b style={{ fontSize: 16 }}>{selected.name}</b>
                  <span className="dim small">v{selected.version}</span>
                  <span className="badge mono">api v{selected.apiVersion}</span>
                  <span className={`badge ${selected.enabled ? 'ok' : ''}`}>
                    {selected.enabled ? '● 已启用' : '○ 已停用'}
                  </span>
                  <span className="grow" />
                  {selected.ui && (
                    <Link
                      to={`/plugins/${selected.id}`}
                      target="_blank"
                      rel="noreferrer"
                      className="sm ghost"
                      title="在新标签页打开插件 UI"
                    >
                      🪟 打开插件页面
                    </Link>
                  )}
                  <button
                    type="button"
                    className={`sm ${selected.enabled ? 'danger' : 'primary'}`}
                    disabled={toggleMut.isPending || !!selected.error}
                    onClick={() => toggleMut.mutate({ id: selected.id, enabled: !selected.enabled })}
                    title={selected.error || (selected.enabled ? '停用后不再触发' : '启用插件')}
                  >
                    {selected.enabled ? '停用' : '启用'}
                  </button>
                </div>

                {selected.description && <div className="muted small">{selected.description}</div>}
                <div className="row small dim" style={{ gap: 10, flexWrap: 'wrap' }}>
                  {selected.author && <span>作者：{selected.author}</span>}
                  <span>事件：{selected.events.join(', ')}</span>
                  <span>权限：{selected.permissions.join(', ') || '无'}</span>
                  {selected.ui && (
                    <span>
                      UI：{selected.ui.entry} · 插槽：{selected.ui.slots.join(', ') || '仅独立页面'}
                    </span>
                  )}
                </div>
                {selected.error && (
                  <div className="panel" style={{ color: 'var(--red)', fontSize: 12 }}>
                    清单错误：{selected.error}
                  </div>
                )}

                <div className="row" style={{ gap: 6, flex: 'none', marginTop: 4 }}>
                  <button
                    type="button"
                    className={`sm ${tab === 'readme' ? 'primary' : 'ghost'}`}
                    onClick={() => setTab('readme')}
                  >
                    📖 README
                  </button>
                  <button
                    type="button"
                    className={`sm ${tab === 'config' ? 'primary' : 'ghost'}`}
                    onClick={() => setTab('config')}
                  >
                    ⚙ config.json {selected.hasConfig ? '' : '(未创建)'}
                  </button>
                  <span className="grow" />
                  {tab === 'config' && (
                    <>
                      <button
                        type="button"
                        className="ghost sm"
                        onClick={() => setConfigText(configQ.data?.content || '{\n  \n}\n')}
                        title="恢复为已保存内容/示例模板"
                      >
                        重置
                      </button>
                      <button
                        type="button"
                        className="ghost sm"
                        onClick={() => setConfigText('{\n  \n}\n')}
                        title="生成空配置对象"
                      >
                        生成默认
                      </button>
                      <button
                        type="button"
                        className="primary sm"
                        disabled={saveMut.isPending || !selectedId}
                        onClick={validateAndSave}
                      >
                        {saveMut.isPending ? '保存中…' : '💾 保存 config.json'}
                      </button>
                    </>
                  )}
                </div>

                <div
                  className="panel"
                  style={{
                    minHeight: 0,
                    flex: 1,
                    overflowY: tab === 'readme' ? 'auto' : 'hidden',
                    padding: tab === 'readme' ? '12px 18px' : 8,
                    display: 'flex',
                    flexDirection: 'column',
                  }}
                >
                  {tab === 'readme' ? (
                    readmeQ.isLoading ? (
                      <span className="dim small">加载 README…</span>
                    ) : readmeQ.error ? (
                      <span className="dim small">未找到 README.md</span>
                    ) : readmeQ.data ? (
                      <>
                        <div className="dim mono small" style={{ marginBottom: 8 }}>
                          {readmeQ.data.path}
                        </div>
                        <Markdown>{readmeQ.data.content}</Markdown>
                      </>
                    ) : (
                      <span className="dim small">未找到 README.md</span>
                    )
                  ) : configQ.isLoading ? (
                    <span className="dim small">加载 config.json…</span>
                  ) : configQ.error ? (
                    <span className="dim small">读取失败：{(configQ.error as Error).message}</span>
                  ) : (
                    <>
                      <div className="row small dim" style={{ marginBottom: 6, gap: 8 }}>
                        <span className="mono grow truncate" title={configQ.data?.path}>
                          {configQ.data?.path}
                        </span>
                        {configQ.data?.exists ? (
                          <span className="badge ok">已存在</span>
                        ) : (
                          <span className="badge">保存后创建</span>
                        )}
                      </div>
                      <textarea
                        className="mono"
                        style={{
                          flex: 1,
                          minHeight: 260,
                          resize: 'none',
                          background: 'var(--bg)',
                          color: 'var(--text)',
                          border: '1px solid var(--border)',
                          borderRadius: 6,
                          padding: 10,
                          fontSize: 12,
                          lineHeight: 1.5,
                        }}
                        value={configText}
                        onChange={(e) => setConfigText(e.target.value)}
                        spellCheck={false}
                      />
                    </>
                  )}
                </div>
              </>
            ) : (
              <div className="empty">选择左侧插件查看详情</div>
            )}
          </div>
        </div>
      )}
    </Modal>
  );
}

function PluginListItem({
  plugin,
  active,
  pending,
  onSelect,
  onToggle,
}: {
  plugin: PluginInfo;
  active: boolean;
  pending: boolean;
  onSelect: () => void;
  onToggle: () => void;
}) {
  return (
    <div
      onClick={onSelect}
      style={{
        cursor: 'pointer',
        padding: '8px 10px',
        borderRadius: 6,
        border: `1px solid ${active ? 'var(--accent)' : 'var(--border)'}`,
        background: active ? 'var(--bg-2)' : 'var(--bg-1)',
      }}
    >
      <div className="row" style={{ gap: 6 }}>
        <b className="small grow truncate" title={plugin.name}>
          {plugin.name}
        </b>
        <span className="dim" style={{ fontSize: 10 }}>
          v{plugin.version}
        </span>
      </div>
      <div className="small dim truncate" title={plugin.description}>
        {plugin.error ? '清单无效' : plugin.description || '生态插件'}
      </div>
      <div className="row" style={{ marginTop: 5, gap: 6 }}>
        <span className={`badge ${plugin.enabled ? 'ok' : ''}`} style={{ fontSize: 10 }}>
          {plugin.enabled ? '启用' : '停用'}
        </span>
        <span className="grow" />
        <button
          type="button"
          className="ghost sm"
          disabled={pending || !!plugin.error}
          onClick={(e) => {
            e.stopPropagation();
            onToggle();
          }}
          style={{ fontSize: 10, padding: '2px 6px' }}
        >
          {plugin.enabled ? '停用' : '启用'}
        </button>
      </div>
    </div>
  );
}
