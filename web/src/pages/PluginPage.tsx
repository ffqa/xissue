import { Link, useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Crumbs } from '../components/common';
import { PluginFrame } from '../components/PluginFrame';

export default function PluginPage() {
  const { id = '' } = useParams();
  const pluginsQ = useQuery({ queryKey: qk.plugins, queryFn: api.plugins });
  const plugin = (pluginsQ.data ?? []).find((p) => p.id === id);

  if (pluginsQ.isLoading) {
    return <div className="page empty">正在读取插件…</div>;
  }
  if (!plugin) {
    return <div className="page empty">插件不存在</div>;
  }
  if (!plugin.ui) {
    return (
      <div className="page empty">
        插件 {plugin.name} 没有声明 UI。请在 plugin.yaml 中添加 <code>ui.entry</code>。
      </div>
    );
  }

  return (
    <div className="page">
      <Crumbs>
        <Link to="/">项目</Link>
        <span>/</span>
        <span>插件</span>
        <span>/</span>
        <span>{plugin.ui.title || plugin.name}</span>
      </Crumbs>
      <div className="row mb" style={{ alignItems: 'baseline', gap: 10 }}>
        <h1 style={{ margin: 0 }}>🧩 {plugin.ui.title || plugin.name}</h1>
        <span className="dim small">v{plugin.version}</span>
        <span className="grow" />
        <span className="small dim">插件 iframe · 访问权限由全局「插件 UI 沙盒模式」控制</span>
      </div>
      <PluginFrame
        plugin={plugin}
        height="calc(100vh - 190px)"
        context={{ pluginId: plugin.id }}
      />
    </div>
  );
}
