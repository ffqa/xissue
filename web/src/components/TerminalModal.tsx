import { useEffect, useRef, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { Modal } from './common';

type ConnectionState = 'connecting' | 'open' | 'closed' | 'error';

function tokenQuery(): string {
  try {
    const token = localStorage.getItem('xissue_token');
    return token ? `?token=${encodeURIComponent(token)}` : '';
  } catch {
    return '';
  }
}

function decodeBase64(data: string): Uint8Array {
  const raw = atob(data);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

function cssVar(name: string, fallback: string): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}

export function TerminalModal({
  runId,
  workdir,
  onClose,
}: {
  runId: number;
  workdir: string;
  onClose: () => void;
}) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const [state, setState] = useState<ConnectionState>('connecting');
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: cssVar('--mono', 'ui-monospace, Menlo, monospace'),
      scrollback: 5000,
      theme: {
        background: cssVar('--bg', '#0e0f11'),
        foreground: cssVar('--text', '#e6e7ea'),
        cursor: cssVar('--accent', '#6e7bf2'),
        selectionBackground: cssVar('--bg-3', '#23262b'),
        black: '#1b1d21',
        red: cssVar('--red', '#eb5757'),
        green: cssVar('--green', '#4cb782'),
        yellow: cssVar('--yellow', '#f2c94c'),
        blue: cssVar('--blue', '#4ea7fc'),
        magenta: cssVar('--purple', '#b48ef7'),
        cyan: '#56b6c2',
        white: cssVar('--text', '#e6e7ea'),
        brightBlack: cssVar('--text-3', '#6d717b'),
        brightRed: cssVar('--red', '#eb5757'),
        brightGreen: cssVar('--green', '#4cb782'),
        brightYellow: cssVar('--yellow', '#f2c94c'),
        brightBlue: cssVar('--blue', '#4ea7fc'),
        brightMagenta: cssVar('--purple', '#b48ef7'),
        brightCyan: '#7fd0da',
        brightWhite: '#ffffff',
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();
    term.focus();

    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const ws = new WebSocket(`${proto}://${location.host}/api/runs/${runId}/terminal${tokenQuery()}`);
    let disposed = false;
    const send = (payload: unknown) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(payload));
    };
    const fitAndResize = () => {
      try {
        fit.fit();
      } catch {
        return;
      }
      send({ type: 'resize', cols: term.cols, rows: term.rows });
    };

    ws.onopen = () => {
      if (disposed) return;
      setState('open');
      fitAndResize();
      term.focus();
    };
    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(String(ev.data)) as { type?: string; data?: string };
        if (msg.type === 'output' && msg.data) {
          term.write(decodeBase64(msg.data));
        } else if (msg.type === 'exit') {
          term.writeln('\r\n\x1b[33m[shell exited]\x1b[0m');
          setState('closed');
        }
      } catch {
        // ignore malformed frames
      }
    };
    ws.onerror = () => {
      if (!disposed) setState('error');
    };
    ws.onclose = () => {
      if (!disposed) setState((s) => (s === 'error' ? s : 'closed'));
    };

    const dataDisposable = term.onData((data) => send({ type: 'input', data }));
    const resizeDisposable = term.onResize(({ cols, rows }) => send({ type: 'resize', cols, rows }));
    const observer = new ResizeObserver(() => requestAnimationFrame(fitAndResize));
    observer.observe(host);

    return () => {
      disposed = true;
      observer.disconnect();
      dataDisposable.dispose();
      resizeDisposable.dispose();
      ws.close();
      term.dispose();
    };
  }, [runId, generation]);

  const statusLabel =
    state === 'open'
      ? '已连接'
      : state === 'connecting'
        ? '连接中…'
        : state === 'error'
          ? '连接失败'
          : '已断开';

  return (
    <Modal
      title={
        <span className="row" style={{ gap: 10 }}>
          <span>⌨️ 工作区终端</span>
          <span className={`badge ${state === 'open' ? 'pr-open' : ''}`}>{statusLabel}</span>
          <span className="small muted mono">{workdir}</span>
        </span>
      }
      onClose={onClose}
      size="xl"
      bodyStyle={{ padding: 0, minHeight: 0 }}
      style={{ width: 'min(1280px, 96vw)', height: '82vh' }}
      footer={
        <>
          <span className="small muted">终端运行在 Run 工作区内；关闭窗口会结束该 Shell。</span>
          <span className="grow" />
          <button type="button" onClick={() => setGeneration((n) => n + 1)}>
            重连
          </button>
          <button type="button" className="primary" onClick={onClose}>
            关闭
          </button>
        </>
      }
    >
      <div className="terminal-shell">
        <div ref={hostRef} className="terminal-host" />
      </div>
    </Modal>
  );
}
