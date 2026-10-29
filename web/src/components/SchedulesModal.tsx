import { useQuery } from '@tanstack/react-query';
import { api, qk } from '../api';
import { fmtTime, Modal } from './common';
import type { ScheduledTask } from '../types';

export function SchedulesModal({
  onClose,
  onOpenMonitor,
  onOpenSettings,
}: {
  onClose: () => void;
  onOpenMonitor: () => void;
  onOpenSettings: () => void;
}) {
  const tasksQ = useQuery({
    queryKey: qk.schedules,
    queryFn: api.schedules,
    // StatusBar owns the polling timer for this query.
  });
  const tasks = tasksQ.data ?? [];
  return (
    <Modal
      title="⏱ 定时任务"
      size="lg"
      onClose={onClose}
      footer={<button onClick={onClose}>关闭</button>}
    >
      <div className="col" style={{ gap: 8 }}>
        <div className="small dim">
          监控中心显示的是最近一次检查结果；这里显示后台实际运行的定时循环。
        </div>
        {tasksQ.isLoading && <div className="empty">加载中…</div>}
        {tasks.map((task: ScheduledTask) => (
          <div key={task.id} className="panel" style={{ padding: 10 }}>
            <div className="row" style={{ gap: 8 }}>
              <b>{task.name}</b>
              <span className={`badge ${task.enabled ? 'ok' : ''}`}>{task.enabled ? '运行中' : '已停用'}</span>
              <span className="grow" />
              <span className="badge mono">{task.interval}</span>
            </div>
            <div className="small muted" style={{ marginTop: 4 }}>{task.detail}</div>
            {task.nextRun && (
              <div className="small dim" style={{ marginTop: 2 }}>下次：{fmtTime(task.nextRun)}</div>
            )}
            {!task.enabled && task.settings && (
              <div className="row" style={{ marginTop: 6, gap: 6 }}>
                <span className="small" style={{ color: 'var(--orange)' }}>当前已停用</span>
                <button
                  type="button"
                  className="sm"
                  onClick={() => {
                    if (task.settings === 'monitor') onOpenMonitor();
                    else onOpenSettings();
                  }}
                >
                  前往启用
                </button>
              </div>
            )}
          </div>
        ))}
      </div>
    </Modal>
  );
}
