import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Modal } from './common';
import { toast } from '../toast';
import { copyText } from '../clipboard';

export function MCPModal({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const mcpQ = useQuery({ queryKey: qk.mcpStatus, queryFn: api.mcpStatus });
  const mcpInstallMut = useMutation({
    mutationFn: (v: { targets?: string[]; transport?: 'stdio' | 'http' }) =>
      api.mcpInstall(v.targets, v.transport),
    onSuccess: (res) => {
      qc.setQueryData(qk.mcpStatus, res);
      const mode = res.transport === 'http' ? 'HTTP 复用' : 'Stdio';
      toast.success(`${mode} 模式已写入：${res.installed.join(', ') || '目标配置'}`);
    },
    onError: (e) => toast.error((e as Error).message),
  });

  return (
    <Modal
      title="🔌 MCP 接入管理"
      size="lg"
      onClose={onClose}
      footer={
        <>
          <span className="small dim">
            每个 IDE / Agent 可以选择 Stdio 独立进程，或共享一个 HTTP MCP 服务以节省内存。
          </span>
          <span className="grow" />
          <button onClick={onClose}>关闭</button>
        </>
      }
    >
      <div className="panel" style={{ padding: 14 }}>
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
            title="每个客户端会话会启动一个 xissue mcp stdio 桥接进程"
            disabled={mcpInstallMut.isPending}
            onClick={() => mcpInstallMut.mutate({ transport: 'stdio' })}
          >
            {mcpInstallMut.isPending ? '正在写入配置…' : '⚡ Stdio 安装到所有 Agent / IDE'}
          </button>
          <button
            type="button"
            className="sm"
            title="所有支持 Streamable HTTP 的客户端共用同一个 xIssue 服务，不再为每个会话启动子进程"
            disabled={mcpInstallMut.isPending}
            onClick={() => mcpInstallMut.mutate({ transport: 'http' })}
          >
            🔗 HTTP 复用安装（省内存）
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
                    <span className="badge ok">
                      已安装{t.transport ? ` · ${t.transport === 'http' ? 'HTTP' : 'Stdio'}` : ''}
                    </span>
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
                onClick={() => mcpInstallMut.mutate({ targets: [t.id], transport: 'stdio' })}
              >
                {t.installed ? '重装 Stdio' : '安装 Stdio'}
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
              void copyText(snippet).then((ok) =>
                ok ? toast.success('已复制通用 MCP JSON 配置到剪贴板') : toast.error('复制失败，请手动选择文本')
              );
            }}
          >
            📋 复制 Stdio JSON
          </button>
          <button
            type="button"
            className="sm ghost"
            onClick={() => {
              const snippet = JSON.stringify(
                {
                  mcpServers: {
                    xissue: {
                      type: 'http',
                      url: mcpQ.data?.httpUrl || 'http://127.0.0.1:7789/api/mcp',
                      headers: { 'X-XIssue': '1' },
                    },
                  },
                },
                null,
                2,
              );
              void copyText(snippet).then((ok) =>
                ok ? toast.success('已复制 HTTP 复用配置（Claude/Cursor）') : toast.error('复制失败，请手动选择文本')
              );
            }}
          >
            🔗 复制 HTTP JSON
          </button>
        </div>
        <div className="small dim" style={{ marginTop: 6, lineHeight: 1.5 }}>
          HTTP 模式下所有 IDE/Agent 共用 <code>{mcpQ.data?.httpUrl || 'http://127.0.0.1:7789/api/mcp'}</code>{' '}
          这一个服务，不再为每个客户端会话启动 <code>xissue mcp</code> 子进程；OpenCode / Claude Code / Cursor /
          Windsurf 支持一键写入。若开启了访问 token，请把请求头改为 <code>Authorization: Bearer &lt;token&gt;</code>。
        </div>
      </div>
    </Modal>
  );
}
