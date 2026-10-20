import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Modal } from './common';
import { toast } from '../toast';
import type { AgentInfo } from '../types';

export function AgentManagerModal({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const agentsQ = useQuery({ queryKey: qk.agents, queryFn: api.agents });
  const agents = agentsQ.data ?? [];

  const [selectedName, setSelectedName] = useState<string>('');
  const [isNew, setIsNew] = useState(false);

  // Form fields
  const [nameInput, setNameInput] = useState('');
  const [command, setCommand] = useState('');
  const [argsText, setArgsText] = useState('');
  const [memoryMB, setMemoryMB] = useState(500);
  const [defaultModel, setDefaultModel] = useState('');
  const [customModelsText, setCustomModelsText] = useState('');
  const [envText, setEnvText] = useState('');
  const [probeFilterProv, setProbeFilterProv] = useState('');

  useEffect(() => {
    if (!selectedName && !isNew && agents.length > 0) {
      selectAgent(agents[0]);
    }
  }, [agents, selectedName, isNew]);

  const selectAgent = (a: AgentInfo) => {
    setIsNew(false);
    setSelectedName(a.name);
    setNameInput(a.name);
    setCommand(a.command);
    setArgsText((a.args ?? []).join(' '));
    setMemoryMB(a.memoryMB || 500);
    setDefaultModel(a.defaultModel || '');
    setCustomModelsText((a.models ?? []).join('\n'));
    const envLines = Object.entries(a.env ?? {}).map(([k, v]) => `${k}=${v}`);
    setEnvText(envLines.join('\n'));
    setProbeFilterProv('');
  };

  const startNew = () => {
    setIsNew(true);
    setSelectedName('');
    setNameInput('');
    setCommand('opencode');
    setArgsText('acp');
    setMemoryMB(500);
    setDefaultModel('');
    setCustomModelsText('');
    setEnvText('');
    setProbeFilterProv('');
  };

  const modelsQ = useQuery({
    queryKey: qk.agentModels(selectedName),
    queryFn: () => api.agentModels(selectedName),
    enabled: !!selectedName && !isNew,
    staleTime: 5 * 60 * 1000,
  });

  const reloadMut = useMutation({
    mutationFn: api.reloadAgents,
    onSuccess: (list) => {
      qc.setQueryData(qk.agents, list);
      qc.invalidateQueries({ queryKey: ['agentModels'] });
      toast.success('已从配置文件重新加载 Agent 配置并清空模型缓存');
      const cur = list.find((x) => x.name === selectedName);
      if (cur) selectAgent(cur);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const saveMut = useMutation({
    mutationFn: async () => {
      const targetName = (isNew ? nameInput : selectedName).trim();
      if (!targetName) throw new Error('请输入 Agent 名称');
      const args = argsText
        .split(/\s+/)
        .map((s) => s.trim())
        .filter(Boolean);
      const models = customModelsText
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean);
      const env: Record<string, string> = {};
      for (const line of envText.split('\n')) {
        const trimmed = line.trim();
        if (!trimmed || trimmed.startsWith('#')) continue;
        const idx = trimmed.indexOf('=');
        if (idx > 0) {
          env[trimmed.slice(0, idx).trim()] = trimmed.slice(idx + 1).trim();
        }
      }
      return api.upsertAgent(targetName, {
        command: command.trim(),
        args,
        env: Object.keys(env).length > 0 ? env : undefined,
        memoryMB: Number(memoryMB) || 500,
        defaultModel: defaultModel.trim(),
        models,
      });
    },
    onSuccess: (saved) => {
      qc.invalidateQueries({ queryKey: qk.agents });
      qc.invalidateQueries({ queryKey: qk.agentModels(saved.name) });
      setIsNew(false);
      setSelectedName(saved.name);
      toast.success(`Agent "${saved.name}" 配置已保存`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const delMut = useMutation({
    mutationFn: (name: string) => api.deleteAgent(name),
    onSuccess: (_, name) => {
      qc.invalidateQueries({ queryKey: qk.agents });
      setSelectedName('');
      toast.info(`已删除 Agent "${name}"`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const addCustomModelLine = (val: string) => {
    const lines = customModelsText
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean);
    if (!lines.includes(val)) {
      setCustomModelsText([...lines, val].join('\n'));
    }
  };

  const mcpQ = useQuery({ queryKey: qk.mcpStatus, queryFn: api.mcpStatus });
  const mcpInstallMut = useMutation({
    mutationFn: (targets?: string[]) => api.mcpInstall(targets),
    onSuccess: (res) => {
      qc.setQueryData(qk.mcpStatus, res);
      toast.success(`已将 xIssue MCP 安装到：${res.installed.join(', ') || '目标配置'}`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  const discovered = modelsQ.data?.models ?? [];
  const discoveredProvs = modelsQ.data?.providers ?? [];
  const filteredDiscovered = probeFilterProv
    ? discovered.filter((m) => m.provider === probeFilterProv)
    : discovered;

  return (
    <Modal
      title="Agent 管理 & MCP 跨 IDE 插件接入"
      size="lg"
      onClose={onClose}
      footer={
        <>
          <button
            type="button"
            onClick={() => reloadMut.mutate()}
            disabled={reloadMut.isPending}
            title="重新读取 ~/.xissue/config.yaml 并清空缓存"
          >
            {reloadMut.isPending ? '刷新中…' : '↻ 刷新全局配置'}
          </button>
          <span className="grow" />
          {!isNew && selectedName && (
            <button
              type="button"
              className="danger"
              disabled={delMut.isPending}
              onClick={() =>
                confirm(`确认从配置中删除 Agent "${selectedName}"？`) && delMut.mutate(selectedName)
              }
            >
              删除此 Agent
            </button>
          )}
          <button type="button" onClick={onClose}>
            关闭
          </button>
          <button
            type="button"
            className="primary"
            disabled={saveMut.isPending || !(isNew ? nameInput.trim() : selectedName) || !command.trim()}
            onClick={() => saveMut.mutate()}
          >
            {saveMut.isPending ? '保存中…' : '保存 Agent 配置'}
          </button>
        </>
      }
    >
      {/* MCP 一键安装面板 */}
      <div className="panel" style={{ padding: 14, marginBottom: 12 }}>
        <div className="row" style={{ marginBottom: 8, flexWrap: 'wrap', gap: 8 }}>
          <b>🔌 xIssue MCP 插件（让所有 IDE / Agent 直接发送任务到 xIssue）</b>
          <span className="small muted">
            提供 <code>xissue_create_issue</code>（支持 <code>autoRun: true</code> 自动启动）、
            <code>xissue_run_issue</code>、<code>xissue_list_issues</code> 等工具
          </span>
          <span className="grow" />
          <button
            type="button"
            className="primary sm"
            disabled={mcpInstallMut.isPending}
            onClick={() => mcpInstallMut.mutate(undefined)}
          >
            {mcpInstallMut.isPending ? '正在写入配置…' : '⚡ 一键安装到本机所有 Agent / IDE'}
          </button>
        </div>

        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))',
            gap: 8,
            marginBottom: 8,
          }}
        >
          {(mcpQ.data?.targets ?? []).map((t) => (
            <div
              key={t.id}
              className="row"
              style={{
                padding: '7px 10px',
                border: '1px solid var(--border)',
                borderRadius: 6,
                background: 'var(--bg)',
              }}
            >
              <div className="col grow" style={{ gap: 2, minWidth: 0 }}>
                <div className="row" style={{ gap: 6 }}>
                  <b className="small">{t.name.split(' (')[0]}</b>
                  {t.installed ? (
                    <span className="badge ok">已安装</span>
                  ) : t.detected ? (
                    <span className="badge">已检测到</span>
                  ) : (
                    <span className="badge">可安装</span>
                  )}
                </div>
                <span className="small dim mono truncate" title={t.configPath}>
                  {t.configPath}
                </span>
              </div>
              <button
                type="button"
                className="sm"
                disabled={mcpInstallMut.isPending}
                onClick={() => mcpInstallMut.mutate([t.id])}
              >
                {t.installed ? '重装' : '安装'}
              </button>
            </div>
          ))}
        </div>

        <div className="row small muted" style={{ gap: 10, flexWrap: 'wrap' }}>
          <span>
            Stdio 命令：<code className="mono">{mcpQ.data?.binaryPath || 'xissue'} mcp</code>
          </span>
          <span>·</span>
          <span>
            HTTP JSON-RPC：<code className="mono">{mcpQ.data?.httpUrl || 'http://127.0.0.1:7789/api/mcp'}</code>
          </span>
          <button
            type="button"
            className="sm ghost"
            onClick={() => {
              const snippet = JSON.stringify(
                {
                  mcpServers: {
                    xissue: {
                      command: mcpQ.data?.binaryPath || 'xissue',
                      args: ['mcp'],
                    },
                  },
                },
                null,
                2,
              );
              navigator.clipboard.writeText(snippet);
              toast.success('已复制通用 MCP JSON 配置到剪贴板');
            }}
          >
            📋 复制通用 MCP JSON
          </button>
        </div>
      </div>

      <div className="row" style={{ gap: 8, flexWrap: 'wrap', marginBottom: 4 }}>
        {agents.map((a) => (
          <button
            key={a.name}
            type="button"
            className={`sm ${!isNew && selectedName === a.name ? 'primary' : ''}`}
            onClick={() => selectAgent(a)}
          >
            {a.name}{' '}
            <span className="small" style={{ opacity: 0.75 }}>
              {a.available ? '●' : '○'}
            </span>
          </button>
        ))}
        <button type="button" className={`sm ${isNew ? 'primary' : ''}`} onClick={startNew}>
          + 新增自定义 Agent
        </button>
      </div>

      <div className="col" style={{ gap: 12 }}>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'minmax(140px, 1fr) minmax(140px, 1fr) minmax(240px, 2fr) 130px',
            gap: 12,
          }}
        >
          <label className="field">
            Agent 标识名
            <input
              className="mono"
              value={nameInput}
              disabled={!isNew}
              placeholder="例如：opencode-fast"
              onChange={(e) => setNameInput(e.target.value)}
            />
          </label>
          <label className="field">
            启动命令 (Command)
            <input
              className="mono"
              value={command}
              placeholder="例如：opencode / omp / npx"
              onChange={(e) => setCommand(e.target.value)}
            />
          </label>
          <label className="field">
            参数 (Args，空格分隔)
            <input
              className="mono"
              value={argsText}
              placeholder="例如：acp 或 -y @zed-industries/codex-acp"
              onChange={(e) => setArgsText(e.target.value)}
            />
          </label>
          <label className="field">
            内存配额 (MB)
            <input
              type="number"
              value={memoryMB}
              onChange={(e) => setMemoryMB(Number(e.target.value))}
            />
          </label>
        </div>

        {/* 默认绑定模型 + 从探测结果快速选择 */}
        <div className="panel" style={{ padding: 14 }}>
          <div className="row" style={{ marginBottom: 10, flexWrap: 'wrap', gap: 8 }}>
            <b>Provider / 模型绑定与探测</b>
            <span className="small muted">
              （可绑定该 Agent 默认使用的 provider/model，也可自定义常用模型列表）
            </span>
            <span className="grow" />
            {!isNew && selectedName && (
              <button
                type="button"
                className="sm"
                disabled={modelsQ.isFetching}
                onClick={async () => {
                  try {
                    const fresh = await api.agentModels(selectedName, true);
                    qc.setQueryData(qk.agentModels(selectedName), fresh);
                    if (fresh.models.length > 0) {
                      toast.success(`已探测到 ${fresh.models.length} 个模型（${fresh.providers.length} 个 Provider）`);
                    } else {
                      toast.info(`未探测到模型。该 Agent 可能尚未登录或未通过 ACP 暴露模型列表，您可在下方手动绑定。`);
                    }
                  } catch (e) {
                    toast.error(`探测失败: ${(e as Error).message}`);
                  }
                }}
              >
                {modelsQ.isFetching ? '探测中…' : '↻ 重新探测 Agent 模型'}
              </button>
            )}
          </div>

          <div className="row" style={{ gap: 10, alignItems: 'flex-end', marginBottom: 12 }}>
            <label className="field" style={{ flex: 1 }}>
              绑定默认模型 (Default Provider/Model，留空则使用 Agent 自身默认)
              <input
                className="mono"
                placeholder={
                  modelsQ.data?.currentModel
                    ? `留空默认：${modelsQ.data.currentModel}`
                    : '例如：opencode/cat-jam-free 或 google/gemini-2.5-flash'
                }
                value={defaultModel}
                onChange={(e) => setDefaultModel(e.target.value)}
              />
            </label>
            {defaultModel && (
              <button type="button" onClick={() => setDefaultModel('')}>
                恢复官方默认
              </button>
            )}
          </div>

          {!isNew && (
            <div className="col" style={{ gap: 6, marginBottom: 12 }}>
              <div className="small muted">
                从已探测到的模型库中快速选择（Agent 原生默认:{' '}
                <code className="mono">{modelsQ.data?.currentModel || '未知'}</code>）：
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '220px 1fr', gap: 10 }}>
                <select
                  value={probeFilterProv}
                  onChange={(e) => setProbeFilterProv(e.target.value)}
                >
                  <option value="">全部 Provider ({discoveredProvs.length})</option>
                  {discoveredProvs.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </select>
                <select
                  value=""
                  onChange={(e) => {
                    const v = e.target.value;
                    if (!v) return;
                    setDefaultModel(v);
                    addCustomModelLine(v);
                  }}
                >
                  <option value="">
                    {modelsQ.isLoading
                      ? '正在探测模型列表…'
                      : `点击选择模型设为默认并加入常用列表 (${filteredDiscovered.length} 个)…`}
                  </option>
                  {filteredDiscovered.map((m) => (
                    <option key={m.value} value={m.value}>
                      [{m.provider}] {m.value}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          )}

          <label className="field">
            自定义 / 绑定常用 Provider/Models（每行一个 `provider/model`，会在启动 Run 的下拉框中显示）
            <textarea
              rows={3}
              className="mono"
              placeholder={'opencode/cat-jam-free\ngoogle/gemini-2.5-flash\nopenai/gpt-5.4'}
              value={customModelsText}
              onChange={(e) => setCustomModelsText(e.target.value)}
            />
          </label>
        </div>

        <label className="field">
          环境变量 (Env，可选，每行一个 KEY=VALUE)
          <textarea
            rows={2}
            className="mono"
            placeholder={'OPENAI_API_KEY=sk-...\nGEMINI_API_KEY=...'}
            value={envText}
            onChange={(e) => setEnvText(e.target.value)}
          />
        </label>
      </div>
    </Modal>
  );
}

