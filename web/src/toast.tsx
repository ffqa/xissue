import { useEffect, useState } from 'react';

export type ToastKind = 'error' | 'success' | 'info';
interface ToastItem {
  id: number;
  kind: ToastKind;
  text: string;
}

type Listener = (items: ToastItem[]) => void;
let items: ToastItem[] = [];
let nextId = 1;
const listeners = new Set<Listener>();

function emit() {
  for (const l of listeners) l(items);
}

export function toast(text: string, kind: ToastKind = 'info', ttl = 5000) {
  const id = nextId++;
  items = [...items, { id, kind, text }].slice(-5);
  emit();
  setTimeout(() => dismiss(id), ttl);
}
toast.error = (text: string) => toast(text, 'error', 8000);
toast.success = (text: string) => toast(text, 'success');
toast.info = (text: string) => toast(text, 'info');

function dismiss(id: number) {
  items = items.filter((t) => t.id !== id);
  emit();
}

export function Toaster() {
  const [list, setList] = useState<ToastItem[]>(items);
  useEffect(() => {
    listeners.add(setList);
    return () => {
      listeners.delete(setList);
    };
  }, []);
  return (
    <div className="toaster">
      {list.map((t) => (
        <div key={t.id} className={`toast toast-${t.kind}`} onClick={() => dismiss(t.id)}>
          {t.text}
        </div>
      ))}
    </div>
  );
}
