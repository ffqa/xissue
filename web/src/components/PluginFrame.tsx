import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { pluginUIURL, pluginsForSlot, type PluginUI } from '../logic/plugins';
import { toast } from '../toast';
import type { PluginInfo } from '../types';

interface PluginFrameProps {
  plugin: PluginInfo & { ui?: PluginUI };
  slot?: string;
  context?: Record<string, unknown>;
  height?: number | string;
}

interface PluginMessage {
  type?: string;
  message?: string;
  kind?: 'info' | 'success' | 'error';
  path?: string;
  height?: number;
}

// PluginFrame hosts trusted plugin UI in a same-origin iframe. Plugins are
// installed by the operator for their own use, so they intentionally have the
// same privileges as the main app: parent.document, cookies, fetch, storage,
// etc. The postMessage bridge below is a convenience, not a security boundary.
export function PluginFrame({ plugin, slot, context = {}, height = 220 }: PluginFrameProps) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const nav = useNavigate();
  const qc = useQueryClient();
  const [frameHeight, setFrameHeight] = useState<number | string>(height);
  const contextRef = useRef(context);
  contextRef.current = context;

  const sendContext = useCallback(() => {
    frameRef.current?.contentWindow?.postMessage(
      { type: 'xissue:context', slot: slot ?? null, context: contextRef.current },
      '*',
    );
  }, [slot]);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      if (!frameRef.current || event.source !== frameRef.current.contentWindow) return;
      const data = event.data as PluginMessage;
      if (!data || typeof data !== 'object') return;
      switch (data.type) {
        case 'xissue:ready':
          sendContext();
          break;
        case 'xissue:notify': {
          const text = String(data.message ?? '').slice(0, 500);
          if (!text) return;
          if (data.kind === 'error') toast.error(text);
          else if (data.kind === 'success') toast.success(text);
          else toast.info(text);
          break;
        }
        case 'xissue:open': {
          const path = String(data.path ?? '');
          if (path.startsWith('/') && !path.startsWith('//')) nav(path);
          break;
        }
        case 'xissue:reload':
          qc.invalidateQueries({ queryKey: qk.plugins });
          qc.invalidateQueries({ queryKey: qk.projects });
          break;
        case 'xissue:resize': {
          const next = Number(data.height);
          if (Number.isFinite(next)) setFrameHeight(Math.max(80, Math.min(2400, next)));
          break;
        }
        default:
          break;
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [nav, qc, sendContext]);

  return (
    <iframe
      ref={frameRef}
      title={plugin.ui?.title || plugin.name}
      src={pluginUIURL(plugin, slot)}
      referrerPolicy="no-referrer"
      onLoad={sendContext}
      style={{
        width: '100%',
        height: frameHeight,
        border: '1px solid var(--border)',
        borderRadius: 8,
        background: 'transparent',
        display: 'block',
      }}
    />
  );
}

// PluginSlot mounts every enabled plugin that declared the named slot.
export function PluginSlot({
  name,
  context,
  height,
}: {
  name: string;
  context?: Record<string, unknown>;
  height?: number | string;
}) {
  const pluginsQ = useQuery({ queryKey: qk.plugins, queryFn: api.plugins });
  const plugins = pluginsForSlot((pluginsQ.data ?? []) as PluginInfo[], name);
  if (plugins.length === 0) return null;
  return (
    <div className="col" style={{ gap: 10, marginBottom: 12 }}>
      {plugins.map((plugin) => (
        <PluginFrame
          key={`${plugin.id}:${name}`}
          plugin={plugin}
          slot={name}
          context={context}
          height={height}
        />
      ))}
    </div>
  );
}
