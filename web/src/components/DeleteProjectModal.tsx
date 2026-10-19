import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Modal } from './common';
import { toast } from '../toast';

interface Props {
  project: {
    id: number;
    name: string;
  };
  onClose: () => void;
  onDeleted?: () => void;
}

export function DeleteProjectModal({ project, onClose, onDeleted }: Props) {
  const qc = useQueryClient();
  const [confirmName, setConfirmName] = useState('');

  const isMatched = confirmName.trim() === project.name.trim();

  const deleteMut = useMutation({
    mutationFn: () => api.deleteProject(project.id, { name: confirmName.trim() }),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: qk.projects });
      qc.invalidateQueries({ queryKey: qk.issues(project.id) });
      qc.invalidateQueries({ queryKey: qk.watches(project.id) });
      qc.invalidateQueries({ queryKey: qk.notifications });
      toast.success(`项目「${res.deleted || project.name}」及其所有关联数据已彻底删除`);
      onClose();
      onDeleted?.();
    },
    onError: (e) => toast.error((e as Error).message),
  });

  return (
    <Modal
      title={`🗑️ 删除项目：${project.name}`}
      onClose={onClose}
      footer={
        <>
          <button onClick={onClose} disabled={deleteMut.isPending}>
            取消
          </button>
          <span className="grow" />
          <button
            className="danger"
            disabled={!isMatched || deleteMut.isPending}
            onClick={() => deleteMut.mutate()}
            style={{
              backgroundColor: isMatched ? '#ef4444' : undefined,
              borderColor: isMatched ? '#dc2626' : undefined,
              color: isMatched ? '#ffffff' : undefined,
              opacity: isMatched ? 1 : 0.45,
              cursor: isMatched ? 'pointer' : 'not-allowed',
            }}
          >
            {deleteMut.isPending ? '正在彻底删除…' : '我已知晓，彻底删除项目'}
          </button>
        </>
      }
    >
      <div className="col" style={{ gap: 14 }}>
        <div
          style={{
            background: 'rgba(239, 68, 68, 0.12)',
            border: '1px solid rgba(239, 68, 68, 0.35)',
            borderRadius: 6,
            padding: '12px 14px',
            color: '#f87171',
            fontSize: 13,
            lineHeight: 1.6,
          }}
        >
          <div style={{ fontWeight: 700, marginBottom: 4, display: 'flex', alignItems: 'center', gap: 6 }}>
            <span>⚠️</span>
            <span>此操作不可逆，将永久清空以下所有关联内容：</span>
          </div>
          <ul style={{ margin: 0, paddingLeft: 18, fontSize: 12 }}>
            <li>项目下的所有 Issue、评论日志与任务运行记录 (Runs)</li>
            <li>关联的代码改动、事件流以及本地 Git Worktree 隔离工作区</li>
            <li>仓库镜像缓存 (Mirrors) 与分支推送监控 (Watches)</li>
            <li>本地导出的 Markdown 归档与对话记录</li>
          </ul>
          <div style={{ marginTop: 6, fontSize: 12, opacity: 0.9 }}>
            💡 彻底删除后，<b>下次重新绑定或导入该代码库时将作为一个全新项目，不再残留任何旧数据</b>。
          </div>
        </div>

        <label className="field" style={{ marginTop: 2 }}>
          <span style={{ fontSize: 13 }}>
            请输入项目名称 <b className="mono" style={{ color: '#ef4444' }}>{project.name}</b> 以确认删除：
          </span>
          <input
            autoFocus
            type="text"
            value={confirmName}
            onChange={(e) => setConfirmName(e.target.value)}
            placeholder={project.name}
            style={{
              borderColor: confirmName && !isMatched ? '#ef4444' : undefined,
            }}
          />
        </label>
      </div>
    </Modal>
  );
}
