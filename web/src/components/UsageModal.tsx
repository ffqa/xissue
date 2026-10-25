import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Modal, fmtTime } from './common';
import type { UsageDay, UsageReport } from '../types';

function fmtTokens(n: number): string {
  if (!n) return '0';
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

function fmtCosts(costs: Record<string, number> | undefined): string {
  const entries = Object.entries(costs ?? {}).filter(([, v]) => v > 0);
  if (entries.length === 0) return '—';
  return entries.map(([cur, v]) => `${cur} ${v.toFixed(4)}`).join(' + ');
}

function UsageRows({ days, report }: { days: UsageDay[]; report: UsageReport }) {
  if (days.length === 0) return <div className="empty">该时间范围内没有 Run 记录。</div>;
  return (
    <table className="table">
      <thead>
        <tr>
          <th>日期</th>
          <th>Runs</th>
          <th>输入</th>
          <th>输出</th>
          <th>总计</th>
          <th>费用</th>
        </tr>
      </thead>
      <tbody>
        {days.map((d) => (
          <tr key={d.date}>
            <td className="mono">{d.date}</td>
            <td>{d.runs}</td>
            <td>{fmtTokens(d.inputTokens)}</td>
            <td>{fmtTokens(d.outputTokens)}</td>
            <td>{fmtTokens(d.totalTokens)}</td>
            <td>{fmtCosts(d.costByCurrency)}</td>
          </tr>
        ))}
        <tr>
          <td><b>合计</b></td>
          <td><b>{report.totals.runs}</b></td>
          <td><b>{fmtTokens(report.totals.inputTokens)}</b></td>
          <td><b>{fmtTokens(report.totals.outputTokens)}</b></td>
          <td><b>{fmtTokens(report.totals.totalTokens)}</b></td>
          <td><b>{fmtCosts(report.totals.costByCurrency)}</b></td>
        </tr>
      </tbody>
    </table>
  );
}

export function UsageModal({ onClose }: { onClose: () => void }) {
  const [days, setDays] = useState(30);
  const usageQ = useQuery({
    queryKey: qk.usage(days),
    queryFn: () => api.usage(days),
    staleTime: 30_000,
  });
  const report = usageQ.data;

  return (
    <Modal
      title="📊 Agent 用量与费用"
      onClose={onClose}
      size="xl"
      footer={
        <>
          <span className="small muted">数据来自 ACP usage_update 与 Agent 返回的结束回合 usage；未上报的 Agent 显示为 0。</span>
          <span className="grow" />
          <button type="button" onClick={onClose}>关闭</button>
        </>
      }
    >
      <div className="row" style={{ gap: 6 }}>
        {[7, 30, 90, 365].map((n) => (
          <button
            key={n}
            type="button"
            className={`sm ${days === n ? 'primary' : 'ghost'}`}
            onClick={() => setDays(n)}
          >
            近 {n} 天
          </button>
        ))}
        <span className="grow" />
        {report && <span className="small muted">统计区间 {report.since.slice(0, 10)} → {report.until.slice(0, 10)}</span>}
      </div>

      {usageQ.isLoading && <div className="empty">加载中…</div>}
      {usageQ.error && <div className="ev-stderr">{(usageQ.error as Error).message}</div>}
      {report && (
        <>
          <div className="row" style={{ gap: 10, flexWrap: 'wrap' }}>
            <div className="card" style={{ padding: 12, minWidth: 150 }}>
              <div className="small muted">Runs</div>
              <div style={{ fontSize: 24 }}>{report.totals.runs}</div>
            </div>
            <div className="card" style={{ padding: 12, minWidth: 170 }}>
              <div className="small muted">Token 总量</div>
              <div style={{ fontSize: 24 }}>{fmtTokens(report.totals.totalTokens)}</div>
              <div className="small muted">输入 {fmtTokens(report.totals.inputTokens)} · 输出 {fmtTokens(report.totals.outputTokens)}</div>
            </div>
            <div className="card" style={{ padding: 12, minWidth: 180 }}>
              <div className="small muted">费用</div>
              <div style={{ fontSize: 24 }}>{fmtCosts(report.totals.costByCurrency)}</div>
              <div className="small muted">按 Agent 上报币种汇总</div>
            </div>
          </div>

          <UsageRows days={report.daily} report={report} />

          <div className="col" style={{ gap: 6 }}>
            <b>费用最高的 Runs</b>
            {report.topRuns.length === 0 ? (
              <div className="small muted">暂无数据</div>
            ) : (
              <table className="table">
                <thead>
                  <tr><th>Run</th><th>Agent</th><th>模型</th><th>Token</th><th>费用</th><th>时间</th></tr>
                </thead>
                <tbody>
                  {report.topRuns.map((r) => (
                    <tr key={r.id}>
                      <td><Link to={`/issues/${r.issueId}`}>#{r.id}</Link></td>
                      <td>{r.agent}</td>
                      <td className="mono">{r.model || '—'}</td>
                      <td>{fmtTokens(r.totalTokens)}</td>
                      <td>{fmtCosts({ [r.costCurrency || 'USD']: r.costAmount })}</td>
                      <td className="small muted">{fmtTime(r.createdAt)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </>
      )}
    </Modal>
  );
}
