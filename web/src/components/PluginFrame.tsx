import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Modal } from './common';
import { pluginUIURL, pluginsForSlot, type PluginUI } from '../logic/plugins';
import { toast } from '../toast';
import type { PluginInfo } from '../types';

interface PluginFrameProps {
  plugin: PluginInfo & { ui?: PluginUI };
  slot?: string;
  context?: Record<string, unknown>;
  height?: number | string;
  width?: number | string;
  allowModal?: boolean;
  modal?: boolean;
  onCloseModal?: () => void;
}

interface PluginMessage {
  type?: string;
  message?: string;
  kind?: 'info' | 'success' | 'error';
  path?: string;
  height?: number;
  title?: string;
}

// PluginFrame hosts trusted plugin UI in a same-origin iframe. Plugins are
// installed by the operator for their own use, so they intentionally have the
// same privileges as the main app: parent.document, cookies, fetch, storage,
// etc. The postMessage bridge below is a convenience, not a security boundary.
export function PluginFrame({
  plugin,
  slot,
  context = {},
  height = 220,
  width = '100%',
  allowModal = true,
  modal = false,
  onCloseModal,
}: PluginFrameProps) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const nav = useNavigate();
  const qc = useQueryClient();
  const settingsQ = useQuery({ queryKey: qk.settings, queryFn: api.settings, staleTime: 60_000 });
  // Changing the sandbox attribute does not retroactively apply to an already
  // loaded iframe. Remount when the global switch changes so isolation is
  // effective immediately (and trusted access returns without a page reload).
  // Fail closed until settings arrive: an enabled sandbox must not be
  // bypassed by a slow or failed /api/settings request.
  const sandboxed = settingsQ.data?.pluginUISandbox ?? true;
  const [frameHeight, setFrameHeight] = useState<number | string>(height);
  const [openModal, setOpenModal] = useState<{ title: string; height: number } | null>(null);
  const contextRef = useRef(context);
  contextRef.current = context;

  const sendContext = useCallback(() => {
    frameRef.current?.contentWindow?.postMessage(
      { type: 'xissue:context', slot: slot ?? null, modal, context: contextRef.current },
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
        case 'xissue:open-modal': {
          if (!allowModal) break;
          const next = Number(data.height);
          setOpenModal({
            title: String(data.title || plugin.ui?.title || plugin.name),
            height: Number.isFinite(next) ? Math.max(160, Math.min(1400, next)) : 480,
          });
          break;
        }
        case 'xissue:close-modal':
          if (onCloseModal) onCloseModal();
          else setOpenModal(null);
          break;
        default:
          break;
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [nav, qc, sendContext]);

  return (
    <>
    <iframe
      key={sandboxed ? 'sandboxed' : 'trusted'}
      ref={frameRef}
      title={plugin.ui?.title || plugin.name}
      src={pluginUIURL(plugin, slot, modal)}
      sandbox={sandboxed ? 'allow-scripts allow-forms allow-popups allow-downloads' : undefined}
      referrerPolicy="no-referrer"
      onLoad={sendContext}
      style={{
        width,
        height: frameHeight,
        border: '1px solid var(--border)',
        borderRadius: 8,
        background: 'transparent',
        display: 'block',
      }}
    />
    {openModal && (
      <Modal
        title={`🧩 ${openModal.title}`}
        size="xl"
        onClose={() => setOpenModal(null)}
        footer={<button onClick={() => setOpenModal(null)}>关闭</button>}
        style={{ width: 'min(900px, 94vw)', height: `min(${openModal.height + 120}px, 90vh)` }}
        bodyStyle={{ height: openModal.height, minHeight: 0 }}
      >
        <PluginFrame
          plugin={plugin}
          slot={slot}
          context={{ ...context, modal: true }}
          height={openModal.height}
          allowModal={false}
          modal
          onCloseModal={() => setOpenModal(null)}
        />
      </Modal>
    )}
    </>
  );
}

// PluginSlot mounts every enabled plugin that declared the named slot.
export function PluginSlot({
  name,
  context,
  height,
  marginBottom = 12,
  inline = false,
}: {
  name: string;
  context?: Record<string, unknown>;
  height?: number | string;
  marginBottom?: number;
  inline?: boolean;
}) {
  const pluginsQ = useQuery({ queryKey: qk.plugins, queryFn: api.plugins });
  const plugins = pluginsForSlot((pluginsQ.data ?? []) as PluginInfo[], name);
  if (plugins.length === 0) return null;
  if (inline) {
    return (
      <span className="row" style={{ gap: 6, display: 'inline-flex' }}>
        {plugins.map((plugin) => (
          <PluginFrame
            key={`${plugin.id}:${name}`}
            plugin={plugin}
            slot={name}
            context={context}
            height={height ?? 28}
            width={150}
          />
        ))}
      </span>
    );
  }
  return (
    <div className="col" style={{ gap: 10, marginBottom }}>
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
