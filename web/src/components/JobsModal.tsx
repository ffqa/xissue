import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { fmtTime, Modal } from './common';
import { toast } from '../toast';
import type { Job } from '../types';

function jobLabel(job: Job): string {
  return job.payload?.branch || job.kind;
}

export function JobsModal({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  // StatusBar owns the polling timer for this shared query.
  const jobsQ = useQuery({ queryKey: qk.jobs, queryFn: () => api.jobs(50) });
  const jobs = jobsQ.data ?? [];
  const active = jobs.filter((job) => job.status === 'queued' || job.status === 'running');
  const retry = useMutation({
    mutationFn: (id: number) => api.retryJob(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.jobs }),
    onError: (e) => toast.error((e as Error).message),
  });
  const cancel = useMutation({
    mutationFn: (id: number) => api.cancelJob(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.jobs }),
    onError: (e) => toast.error((e as Error).message),
  });

  return (
    <Modal
      title={`📋 任务队列（${active.length} 个进行中/排队）`}
      size="lg"
      onClose={onClose}
      footer={<button onClick={onClose}>关闭</button>}
    >
      <div className="col" style={{ gap: 6 }}>
        {jobsQ.isLoading && <div className="empty">加载中…</div>}
        {!jobsQ.isLoading && jobs.length === 0 && <div className="empty">暂无任务</div>}
        {jobs.map((job) => (
          <div key={job.id} className="panel" style={{ padding: 9 }}>
            <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
              <span className="mono small">#{job.id}</span>
              <span className={`badge ${job.status === 'failed' ? 'err' : job.status === 'completed' ? 'ok' : job.status === 'running' ? 'warn' : ''}`}>
                {job.status}
              </span>
              <span className="small">{job.kind}</span>
              <code className="mono small grow ellipsis">{jobLabel(job)}</code>
              <span className="small dim">{fmtTime(job.createdAt)}</span>
              {job.status === 'queued' && (
                <button className="sm ghost" disabled={cancel.isPending} onClick={() => cancel.mutate(job.id)}>取消</button>
              )}
              {(job.status === 'failed' || job.status === 'cancelled') && (
                <button className="sm ghost" disabled={retry.isPending} onClick={() => retry.mutate(job.id)}>重试</button>
              )}
            </div>
            {job.error && <div className="small" style={{ color: 'var(--red)', marginTop: 4 }}>{job.error}</div>}
          </div>
        ))}
      </div>
    </Modal>
  );
}
