import { useEffect, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

/** 渲染到顶栏的面包屑 */
export function Crumbs({ children }: { children: ReactNode }) {
  const [el, setEl] = useState<HTMLElement | null>(null);
  useEffect(() => setEl(document.getElementById('crumbs')), []);
  return el ? createPortal(children, el) : null;
}

export function Markdown({ children }: { children: string }) {
  return (
    <div className="markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{ a: ({ node: _n, ...props }) => <a {...props} target="_blank" rel="noreferrer" /> }}
      >
        {children}
      </ReactMarkdown>
    </div>
  );
}

export function Modal({
  title,
  onClose,
  children,
  footer,
  size = 'md',
  bodyStyle,
  style,
}: {
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  size?: 'md' | 'lg' | 'xl';
  bodyStyle?: React.CSSProperties;
  style?: React.CSSProperties;
}) {
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener('keydown', h, true);
    return () => window.removeEventListener('keydown', h, true);
  }, [onClose]);
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={`modal modal-${size}`} role="dialog" style={style}>
        <div className="modal-h">
          <h2 className="grow">{title}</h2>
          <button className="ghost sm" onClick={onClose} title="关闭 (Esc)">
            ✕
          </button>
        </div>
        <div className="modal-b" style={bodyStyle}>{children}</div>
        {footer && <div className="modal-f">{footer}</div>}
      </div>
    </div>
  );
}

export function fmtTime(s: string | null | undefined) {
  if (!s) return '';
  const d = new Date(s);
  if (isNaN(d.getTime())) return s;
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  return sameDay
    ? d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
    : d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

export const ISSUE_STATUS_LABEL: Record<string, string> = {
  backlog: 'Backlog',
  todo: 'Todo',
  running: 'Running',
  review: 'Review',
  pr_open: 'PR Open',
  done: 'Done',
  failed: 'Failed',
};

export const RUN_STATUS_LABEL: Record<string, string> = {
  queued: '排队中',
  starting: '启动中',
  running: '运行中',
  awaiting_permission: '待审批',
  completed: '已完成',
  failed: '失败',
  cancelled: '已取消',
  submitted: '已提交',
  discarded: '已丢弃',
};

export function RunStatusTag({ status }: { status: string }) {
  return (
    <span className={`row small st-${status}`} style={{ gap: 5 }}>
      <span className="dot" />
      {RUN_STATUS_LABEL[status] ?? status}
    </span>
  );
}

export function editorUrl(template: string | undefined, absPath: string): string | null {
  if (!template || !absPath) return null;
  const encoded = absPath
    .split('/')
    .map((seg) => encodeURIComponent(seg))
    .join('/');
  return template.replace('{path}', encoded);
}

