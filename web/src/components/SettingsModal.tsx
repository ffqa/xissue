import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, qk } from '../api';
import { Modal } from './common';
import { toast } from '../toast';
import { useTheme } from '../theme';
import type { AgentRoles, AutomationRule } from '../types';
import {
  NOTIFICATION_CHANNELS,
  NOTIFICATION_EVENTS,
  defaultNotificationRoutes,
  normalizeNotificationRoutes,
  readBrowserNotify,
  readBrowserOnlyHidden,
  readBrowserRoutes,
  writeBrowserOnlyHidden,
  writeBrowserNotify,
  writeBrowserRoutes,
  type NotificationChannel,
  type NotificationRoutes,
} from '../notificationSettings';

type SettingsTab = 'general' | 'channels' | 'routes' | 'agents' | 'automation';

export function SettingsModal({
  onClose,
  initialTab = 'general',
  onOpenAgents,
  onOpenMCP,
}: {
  onClose: () => void;
  initialTab?: SettingsTab;
  onOpenAgents?: () => void;
  onOpenMCP?: () => void;
}) {
  const qc = useQueryClient();
  const [theme, toggleTheme] = useTheme();
  const [tab, setTab] = useState<SettingsTab>(initialTab);
  const [expanded, setExpanded] = useState(false);

  const configQ = useQuery({ queryKey: qk.monitorConfig, queryFn: api.monitorConfig });
  const settingsQ = useQuery({ queryKey: qk.settings, queryFn: api.settings });
  const projectsQ = useQuery({ queryKey: qk.projects, queryFn: api.projects });
  const agentsQ = useQuery({ queryKey: qk.agents, queryFn: api.agents });

  const [cfgTgToken, setCfgTgToken] = useState('');
  const [cfgTgChatId, setCfgTgChatId] = useState('');
  const [cfgWebhookUrl, setCfgWebhookUrl] = useState('');
  const [autoUpdate, setAutoUpdate] = useState(true);
  const [defaultAgent, setDefaultAgent] = useState('');
  const [agentRoles, setAgentRoles] = useState<AgentRoles>({
    issueReview: '',
    issueDevelop: '',
    prReview: '',
    autoMerge: '',
  });
  const [publicUrl, setPublicUrl] = useState('');
  const [browserNotify, setBrowserNotify] = useState(readBrowserNotify());
  const [browserOnlyHidden, setBrowserOnlyHidden] = useState(readBrowserOnlyHidden());
  const [browserPermission, setBrowserPermission] = useState<string>(
    typeof Notification !== 'undefined' ? Notification.permission : 'unsupported',
  );
  const [routes, setRoutes] = useState<NotificationRoutes>(readBrowserRoutes());
  const [globalRules, setGlobalRules] = useState<AutomationRule[]>([]);
  const [newGlobalRuleOpen, setNewGlobalRuleOpen] = useState(false);
  const [newRuleName, setNewRuleName] = useState('全局分支推送检查');
  const [newRuleTrigger, setNewRuleTrigger] = useState<'branch_push' | 'pr_comment' | 'pr_review'>('branch_push');
  const [newRuleBranch, setNewRuleBranch] = useState('');
  const [newRuleAuthor, setNewRuleAuthor] = useState('');
  const [newRulePattern, setNewRulePattern] = useState('');
  const [newRuleProjects, setNewRuleProjects] = useState<number[]>([]);
  const [newRuleMerge, setNewRuleMerge] = useState(false);
  const [newRuleDone, setNewRuleDone] = useState(false);
  const [newRulePull, setNewRulePull] = useState(false);
  const [newRuleNotify, setNewRuleNotify] = useState(true);

  useEffect(() => {
    if (!configQ.data) return;
    setCfgTgToken(configQ.data.telegramBotToken || '');
    setCfgTgChatId(configQ.data.telegramChatId || '');
    setCfgWebhookUrl(configQ.data.webhookUrl || '');
    setPublicUrl(configQ.data.publicUrl || '');
    const normalized = normalizeNotificationRoutes(configQ.data.notificationRoutes);
    setRoutes(normalized);
    writeBrowserRoutes(normalized);
  }, [configQ.data]);

  useEffect(() => {
    if (settingsQ.data) {
      setAutoUpdate(settingsQ.data.autoUpdate);
      setDefaultAgent(settingsQ.data.defaultAgent || '');
      setAgentRoles({
        issueReview: settingsQ.data.agentRoles?.issueReview || '',
        issueDevelop: settingsQ.data.agentRoles?.issueDevelop || '',
        prReview: settingsQ.data.agentRoles?.prReview || '',
        autoMerge: settingsQ.data.agentRoles?.autoMerge || '',
      });
      setGlobalRules(settingsQ.data.globalAutomationRules ?? []);
    }
  }, [settingsQ.data]);

  const saveChannelsMut = useMutation({
    mutationFn: () =>
      api.patchMonitorConfig({
        telegramBotToken: cfgTgToken,
        telegramChatId: cfgTgChatId,
        webhookUrl: cfgWebhookUrl,
        notificationRoutes: routes,
      }),
    onSuccess: (updated) => {
      qc.setQueryData(qk.monitorConfig, updated);
      writeBrowserRoutes(routes);
      toast.success('通知渠道与路由已保存');
    },
    onError: (e) => toast.error(`保存失败: ${(e as Error).message}`),
  });

  const saveGeneralMut = useMutation({
    mutationFn: async () => {
      await api.patchSettings({ autoUpdate, defaultAgent, agentRoles });
      return api.patchMonitorConfig({ publicUrl });
    },
    onSuccess: (updated) => {
      qc.setQueryData(qk.monitorConfig, updated);
      qc.invalidateQueries({ queryKey: qk.settings });
      toast.success('通用设置已保存');
    },
    onError: (e) => toast.error(`保存失败: ${(e as Error).message}`),
  });

  const saveAgentRole = (key: keyof AgentRoles, value: string) => {
    const next = { ...agentRoles, [key]: value };
    setAgentRoles(next);
    void api
      .patchSettings({ agentRoles: next })
      .then(() => qc.invalidateQueries({ queryKey: qk.settings }))
      .catch((err) => toast.error(`保存 Agent 角色失败: ${(err as Error).message}`));
  };

  const testNotifyMut = useMutation({
    mutationFn: () => api.testNotify(),
    onSuccess: (res) => toast.success(res.message || '测试通知已派发'),
    onError: (e) => toast.error(`测试通知失败: ${(e as Error).message}`),
  });

  const saveGlobalRulesMut = useMutation({
    mutationFn: (rules: AutomationRule[]) => api.patchSettings({ globalAutomationRules: rules }),
    onSuccess: (updated) => {
      setGlobalRules(updated.globalAutomationRules ?? []);
      qc.setQueryData(qk.settings, updated);
      toast.success('全局自动化规则已保存');
    },
    onError: (e) => toast.error(`保存全局规则失败: ${(e as Error).message}`),
  });

  const persistGlobalRules = (rules: AutomationRule[]) => {
    setGlobalRules(rules);
    saveGlobalRulesMut.mutate(rules);
  };

  const requestPermission = async () => {
    if (typeof Notification === 'undefined') {
      toast.error('当前浏览器不支持桌面通知');
      return;
    }
    const result = await Notification.requestPermission();
    setBrowserPermission(result);
    if (result === 'granted') {
      setBrowserNotify(true);
      toast.success('浏览器通知已开启');
    } else {
      toast.error('浏览器通知权限未授予，可在浏览器站点设置中修改');
    }
  };

  const toggleRoute = (kind: string, channel: NotificationChannel) => {
    setRoutes((prev) => {
      const current = prev[kind] ?? [];
      const next = current.includes(channel)
        ? current.filter((c) => c !== channel)
        : [...current, channel];
      return { ...prev, [kind]: next };
    });
  };

  const saveCurrent = () => {
    if (tab === 'general' || tab === 'agents') saveGeneralMut.mutate();
    else if (tab === 'channels' || tab === 'routes') saveChannelsMut.mutate();
    else if (tab === 'automation') saveGlobalRulesMut.mutate(globalRules);
  };

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') {
        e.preventDefault();
        saveCurrent();
      }
      if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === 'f') {
        e.preventDefault();
        setExpanded((v) => !v);
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  });

  const activeRules = useMemo(
    () =>
      (projectsQ.data ?? []).map((p) => ({
        ...p,
        active: (p.automationRules ?? []).filter((r) => r.enabled).length,
        total: (p.automationRules ?? []).length,
      })),
    [projectsQ.data],
  );

  return (
    <Modal
      title="⚙️ 全局设置"
      size="xl"
      fullscreen={expanded}
      style={expanded ? undefined : { height: '84vh' }}
      headerActions={
        <button className="ghost sm" onClick={() => setExpanded((v) => !v)} title="切换全屏">
          {expanded ? '⤡ 还原' : '⛶ 全屏'}
        </button>
      }
      onClose={onClose}
      footer={
        <div className="row" style={{ width: '100%', justifyContent: 'space-between' }}>
          <span className="small dim">
            {tab === 'routes'
              ? 'In-app 始终记录；浏览器通知按本机设置生效。'
              : tab === 'automation'
                ? '全局规则修改会立即保存；Ctrl/Cmd+S 保存，Ctrl/Cmd+Shift+F 切换全屏。'
                : '全局设置会立即保存到本机 config.yaml。'}
          </span>
          <div className="row" style={{ gap: 8 }}>
            <button onClick={onClose}>关闭</button>
            {(tab === 'general' || tab === 'agents') && (
              <button
                className="primary"
                disabled={saveGeneralMut.isPending || settingsQ.isLoading}
                onClick={() => {
                  writeBrowserNotify(browserNotify);
                  saveGeneralMut.mutate();
                }}
              >
                {saveGeneralMut.isPending ? '保存中…' : '保存通用设置'}
              </button>
            )}
            {tab === 'automation' && (
              <button className="primary" disabled={saveGlobalRulesMut.isPending} onClick={saveCurrent}>
                {saveGlobalRulesMut.isPending ? '保存中…' : '保存全局规则'}
              </button>
            )}
            {(tab === 'channels' || tab === 'routes') && (
              <button
                className="primary"
                disabled={saveChannelsMut.isPending || configQ.isLoading}
                onClick={() => {
                  writeBrowserNotify(browserNotify);
                  saveChannelsMut.mutate();
                }}
              >
                {saveChannelsMut.isPending ? '保存中…' : '保存通知设置'}
              </button>
            )}
          </div>
        </div>
      }
    >
      <div className="settings-layout">
        <nav className="settings-nav">
          {[
            { id: 'general', label: '🧭 通用' },
            { id: 'channels', label: '📨 通知渠道' },
            { id: 'routes', label: '🔀 事件路由' },
            { id: 'agents', label: '🤖 Agent' },
            { id: 'automation', label: '⚡ 自动化' },
          ].map((item) => (
            <button
              key={item.id}
              className={tab === item.id ? 'primary sm' : 'ghost sm'}
              style={{ justifyContent: 'flex-start' }}
              onClick={() => setTab(item.id as SettingsTab)}
            >
              {item.label}
            </button>
          ))}
        </nav>

        <div className="settings-content">
          {tab === 'general' && (
            <div className="col" style={{ gap: 18 }}>
              <section className="settings-card">
                <h3>🎨 外观与更新</h3>
                <div className="row" style={{ gap: 10, flexWrap: 'wrap' }}>
                  <button className="ghost" onClick={toggleTheme}>
                    {theme === 'dark' ? '☀️ 切换亮色' : '🌙 切换暗色'}
                  </button>
                  <label className="row" style={{ gap: 8, cursor: 'pointer' }}>
                    <input
                      type="checkbox"
                      checked={autoUpdate}
                      onChange={(e) => setAutoUpdate(e.target.checked)}
                    />
                    <span>自动检查更新</span>
                  </label>
                </div>
                <label className="field" style={{ marginTop: 12 }}>
                  外部访问地址（通知链接）
                  <input
                    className="mono"
                    value={publicUrl}
                    placeholder="如 http://100.64.0.2:7789；留空自动推导"
                    onChange={(e) => setPublicUrl(e.target.value)}
                  />
                  <span className="small dim">外部通知里的链接需要它才能点击；0.0.0.0 监听时自动取本机非回环 IP。</span>
                </label>
                <p className="small dim" style={{ marginBottom: 0 }}>
                  自动检查更新只检查版本，不会未经确认安装；浏览器通知是当前浏览器的本地偏好。
                </p>
              </section>
            </div>
          )}

          {tab === 'channels' && (
            <div className="col" style={{ gap: 18 }}>
              <section className="settings-card">
                <h3>🔔 浏览器通知</h3>
                <p className="small muted">
                  当页面收到新的 Issue、PR、分支推送、任务完成等通知时，使用系统桌面通知提醒。
                </p>
                <label className="row" style={{ gap: 8, cursor: 'pointer' }}>
                  <input
                    type="checkbox"
                    checked={browserNotify}
                    onChange={(e) => {
                      const next = e.target.checked;
                      setBrowserNotify(next);
                      writeBrowserNotify(next);
                      if (next && typeof Notification !== 'undefined' && Notification.permission === 'default') {
                        void requestPermission();
                      }
                    }}
                  />
                  <span>启用浏览器通知</span>
                  <span className={`badge ${browserPermission === 'granted' ? 'ok' : ''}`}>
                    {browserPermission === 'granted'
                      ? '已授权'
                      : browserPermission === 'denied'
                        ? '已被拒绝'
                        : browserPermission === 'unsupported'
                          ? '不支持'
                          : '未授权'}
                  </span>
                </label>
                <label className="row" style={{ gap: 8, cursor: 'pointer', marginTop: 8 }}>
                  <input
                    type="checkbox"
                    checked={browserOnlyHidden}
                    onChange={(e) => {
                      setBrowserOnlyHidden(e.target.checked);
                      writeBrowserOnlyHidden(e.target.checked);
                    }}
                  />
                  <span>仅当页面在后台时发送桌面通知</span>
                </label>
                <div className="row" style={{ gap: 8, marginTop: 10 }}>
                  <button className="sm ghost" onClick={() => void requestPermission()}>
                    🔐 请求通知权限
                  </button>
                  <button
                    className="sm ghost"
                    onClick={() => {
                      if (typeof Notification === 'undefined') {
                        toast.error('当前浏览器不支持桌面通知');
                        return;
                      }
                      if (Notification.permission !== 'granted') {
                        toast.error('请先请求并授予通知权限');
                        return;
                      }
                      try {
                        new Notification('xIssue 通知测试', {
                          body: '如果你看到这条消息，浏览器通知链路正常。',
                          icon: '/favicon.svg',
                        });
                      } catch (e) {
                        toast.error(`本地通知发送失败: ${(e as Error).message}`);
                      }
                    }}
                  >
                    🧪 发送本地测试
                  </button>
                </div>
              </section>

              <section className="settings-card">
                <h3>📨 通知渠道</h3>
                <p className="small muted">
                  In-app 通知始终保留。Telegram / Webhook 用于把消息推送到外部，凭据只保存在本机。
                </p>
                <label className="field">
                  Telegram Bot Token
                  <input
                    type="password"
                    value={cfgTgToken}
                    placeholder="123456:ABC-DEF..."
                    onChange={(e) => setCfgTgToken(e.target.value)}
                  />
                </label>
                <label className="field">
                  Telegram Chat ID
                  <input
                    value={cfgTgChatId}
                    placeholder="例如 123456789"
                    onChange={(e) => setCfgTgChatId(e.target.value)}
                  />
                </label>
                <label className="field">
                  通用 Webhook URL（飞书 / 企微 / 钉钉 / Slack / 自定义）
                  <input
                    value={cfgWebhookUrl}
                    placeholder="https://..."
                    onChange={(e) => setCfgWebhookUrl(e.target.value)}
                  />
                </label>
                <div className="row" style={{ gap: 8 }}>
                  <button className="sm ghost" disabled={testNotifyMut.isPending} onClick={() => testNotifyMut.mutate()}>
                    {testNotifyMut.isPending ? '发送中…' : '🧪 测试已启用渠道'}
                  </button>
                  <span className="small dim">测试会遵循「事件路由」中 test 的配置。</span>
                </div>
              </section>
            </div>
          )}

          {tab === 'routes' && (
            <div className="col" style={{ gap: 12 }}>
              <section className="settings-card">
                <h3>🔀 事件路由</h3>
                <div className="row" style={{ justifyContent: 'space-between', gap: 10 }}>
                  <p className="small muted" style={{ margin: 0 }}>
                    每一类事件可以独立选择走哪些通道。取消勾选表示该事件不发送到该通道；In-app 不受影响。
                  </p>
                  <button className="sm ghost" onClick={() => setRoutes(defaultNotificationRoutes())}>
                    恢复全开
                  </button>
                </div>
                <div className="route-table">
                  <div className="route-row route-head">
                    <span>事件</span>
                    {NOTIFICATION_CHANNELS.map((c) => (
                      <span key={c.id}>{c.label}</span>
                    ))}
                    <span>In-app</span>
                  </div>
                  {NOTIFICATION_EVENTS.map((event) => (
                    <div className="route-row" key={event.kind}>
                      <span>
                        <b>{event.label}</b>
                        <span className="small dim" style={{ display: 'block' }}>{event.description}</span>
                      </span>
                      {NOTIFICATION_CHANNELS.map((channel) => (
                        <label key={channel.id} className="row" style={{ gap: 4, justifyContent: 'center' }}>
                          <input
                            type="checkbox"
                            checked={(routes[event.kind] ?? []).includes(channel.id)}
                            onChange={() => toggleRoute(event.kind, channel.id)}
                          />
                        </label>
                      ))}
                      <span className="small dim" style={{ textAlign: 'center' }}>始终开</span>
                    </div>
                  ))}
                </div>
              </section>
            </div>
          )}

          {tab === 'agents' && (
            <div className="col" style={{ gap: 12 }}>
              <section className="settings-card">
                <h3>🤖 默认 Agent</h3>
                <p className="small muted">
                  收到 Issue 后，项目未单独指定 Agent 时使用这里的默认值；项目设置里仍可覆盖。
                </p>
                <label className="field">
                  默认 Agent
                  <select
                    value={defaultAgent}
                    onChange={(e) => {
                      const next = e.target.value;
                      setDefaultAgent(next);
                      void api
                        .patchSettings({ defaultAgent: next })
                        .then((updated) => qc.setQueryData(qk.settings, updated))
                        .catch((err) => toast.error(`保存默认 Agent 失败: ${(err as Error).message}`));
                    }}
                  >
                    <option value="">自动选择（优先 opencode）</option>
                    {(agentsQ.data ?? []).map((a) => (
                      <option key={a.name} value={a.name}>
                        {a.name}{a.available ? '' : '（本机未就绪，可用于远程节点）'}
                      </option>
                    ))}
                  </select>
                </label>
                <div
                  style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(auto-fit, minmax(190px, 1fr))',
                    gap: 10,
                    marginTop: 14,
                  }}
                >
                  {([
                    ['issueReview', 'Issue 审查'],
                    ['issueDevelop', 'Issue 开发'],
                    ['prReview', 'PR 审查'],
                    ['autoMerge', '审核通过后自动合并'],
                  ] as const).map(([key, label]) => (
                    <label key={key} className="field">
                      {label} Agent
                      <select
                        value={agentRoles[key]}
                        onChange={(e) => saveAgentRole(key, e.target.value)}
                      >
                        <option value="">自动（{defaultAgent || '默认 Agent'}）</option>
                        {(agentsQ.data ?? []).map((a) => (
                          <option key={a.name} value={a.name}>
                            {a.name}{a.available ? '' : '（本机未就绪，可用于远程节点）'}
                          </option>
                        ))}
                      </select>
                    </label>
                  ))}
                </div>
                <p className="small dim" style={{ marginTop: 10 }}>
                  Issue 开发身份会用于自动预开发；其他角色供 Issue 审查、PR 审查和审核通过后的自动合并流程使用。留空时回退到默认 Agent。
                </p>

                <div className="row" style={{ gap: 8, marginTop: 10, flexWrap: 'wrap' }}>
                  {onOpenAgents && (
                    <button className="sm ghost" onClick={onOpenAgents}>
                      ⚙️ 打开 Agent 管理
                    </button>
                  )}
                  {onOpenMCP && (
                    <button className="sm ghost" onClick={onOpenMCP}>
                      🔌 MCP 接入
                    </button>
                  )}
                </div>
              </section>
              <section className="settings-card">
                <h3>已配置 Agent</h3>
                {(agentsQ.data ?? []).map((a) => (
                  <div key={a.name} className="row" style={{ gap: 8, padding: '8px 0', borderTop: '1px solid var(--border)' }}>
                    <b>{a.name}</b>
                    <span className={`badge ${a.available ? 'ok' : ''}`}>{a.available ? '就绪' : '未就绪'}</span>
                    {(a.defaultModel || a.models?.[0]) && <span className="small dim">{a.defaultModel || a.models?.[0]}</span>}
                    <span className="grow" />
                    {defaultAgent === a.name && <span className="badge ok">默认</span>}
                  </div>
                ))}
              </section>
            </div>
          )}

          {tab === 'automation' && (
            <div className="col" style={{ gap: 12 }}>
              <section className="settings-card">
                <div className="row" style={{ justifyContent: 'space-between', gap: 10 }}>
                  <div>
                    <h3>⚡ 全局自动化规则</h3>
                    <p className="small muted" style={{ margin: 0 }}>
                      全局规则对所有项目生效，也可勾选限定项目；项目内规则继续叠加执行。
                    </p>
                  </div>
                  <button className="sm ghost" onClick={() => setNewGlobalRuleOpen((v) => !v)}>
                    {newGlobalRuleOpen ? '取消添加' : '+ 添加全局规则'}
                  </button>
                </div>

                {newGlobalRuleOpen && (
                  <div className="settings-subcard">
                    <label className="field">
                      规则名称
                      <input value={newRuleName} onChange={(e) => setNewRuleName(e.target.value)} />
                    </label>
                    <div className="row" style={{ gap: 8, flexWrap: 'wrap', marginBottom: 8 }}>
                      <label className="field" style={{ margin: 0, minWidth: 160 }}>
                        触发事件
                        <select value={newRuleTrigger} onChange={(e) => setNewRuleTrigger(e.target.value as typeof newRuleTrigger)}>
                          <option value="branch_push">分支推送</option>
                          <option value="pr_comment">PR 评论 / Report</option>
                          <option value="pr_review">PR Review</option>
                        </select>
                      </label>
                      {newRuleTrigger === 'branch_push' && (
                        <label className="field" style={{ margin: 0, minWidth: 140 }}>
                          分支（留空=任意）
                          <input value={newRuleBranch} onChange={(e) => setNewRuleBranch(e.target.value)} placeholder="main, dev" />
                        </label>
                      )}
                      <label className="field" style={{ margin: 0, minWidth: 140 }}>
                        作者（留空=任意）
                        <input value={newRuleAuthor} onChange={(e) => setNewRuleAuthor(e.target.value)} placeholder="release-bot" />
                      </label>
                      <label className="field" style={{ margin: 0, minWidth: 180 }}>
                        内容正则 / 关键字
                        <input value={newRulePattern} onChange={(e) => setNewRulePattern(e.target.value)} placeholder="(?i)passed" />
                      </label>
                    </div>

                    <div className="field" style={{ marginBottom: 8 }}>
                      项目范围（不选=全部项目）
                      <div className="scope-list">
                        {(projectsQ.data ?? []).map((p) => (
                          <label key={p.id} className="row" style={{ gap: 6 }}>
                            <input
                              type="checkbox"
                              checked={newRuleProjects.includes(p.id)}
                              onChange={(e) =>
                                setNewRuleProjects((prev) =>
                                  e.target.checked ? [...prev, p.id] : prev.filter((id) => id !== p.id),
                                )
                              }
                            />
                            <span>{p.name}</span>
                          </label>
                        ))}
                      </div>
                    </div>

                    <div className="row" style={{ gap: 12, flexWrap: 'wrap', marginBottom: 10 }}>
                      <label className="row" style={{ gap: 6 }}>
                        <input type="checkbox" checked={newRuleMerge} onChange={(e) => setNewRuleMerge(e.target.checked)} />
                        合并 PR
                      </label>
                      <label className="row" style={{ gap: 6 }}>
                        <input type="checkbox" checked={newRuleDone} onChange={(e) => setNewRuleDone(e.target.checked)} />
                        标记 Issue 完成
                      </label>
                      <label className="row" style={{ gap: 6 }}>
                        <input type="checkbox" checked={newRulePull} onChange={(e) => setNewRulePull(e.target.checked)} />
                        拉取本地代码
                      </label>
                      <label className="row" style={{ gap: 6 }}>
                        <input type="checkbox" checked={newRuleNotify} onChange={(e) => setNewRuleNotify(e.target.checked)} />
                        发送通知
                      </label>
                    </div>
                    <button
                      className="primary sm"
                      disabled={!newRuleName.trim() || saveGlobalRulesMut.isPending}
                      onClick={() => {
                        const actions: AutomationRule['actions'] = [];
                        if (newRuleMerge) actions.push({ type: 'merge_pr', strategy: 'squash' });
                        if (newRuleDone) actions.push({ type: 'set_issue_status', status: 'done' });
                        if (newRulePull) actions.push({ type: 'pull_local' });
                        if (newRuleNotify) actions.push({ type: 'notify', message: '全局自动化规则已触发' });
                        const rule: AutomationRule = {
                          id: (crypto.randomUUID?.() || `rule-${Date.now()}`),
                          name: newRuleName.trim(),
                          enabled: true,
                          trigger: newRuleTrigger,
                          conditions: {
                            branch: newRuleTrigger === 'branch_push' ? newRuleBranch.trim() || undefined : undefined,
                            author: newRuleAuthor.trim() || undefined,
                            pattern: newRulePattern.trim() || undefined,
                          },
                          actions,
                          projectIds: newRuleProjects.length ? newRuleProjects : undefined,
                        };
                        persistGlobalRules([...globalRules, rule]);
                        setNewGlobalRuleOpen(false);
                      }}
                    >
                      添加规则
                    </button>
                  </div>
                )}

                {globalRules.length === 0 ? (
                  <div className="empty">暂无全局规则。全局规则适合跨项目的统一策略。</div>
                ) : (
                  globalRules.map((rule) => (
                    <div key={rule.id} className="row" style={{ gap: 10, padding: '10px 0', borderTop: '1px solid var(--border)' }}>
                      <input
                        type="checkbox"
                        checked={rule.enabled}
                        onChange={(e) =>
                          persistGlobalRules(globalRules.map((r) => (r.id === rule.id ? { ...r, enabled: e.target.checked } : r)))
                        }
                      />
                      <div className="grow">
                        <b>{rule.name}</b>
                        <div className="small dim">
                          {rule.trigger} · {rule.projectIds?.length ? `${rule.projectIds.length} 个项目` : '全部项目'} ·{' '}
                          {rule.actions.map((a) => a.type).join(' / ') || '无动作'}
                        </div>
                      </div>
                      <button className="danger sm" onClick={() => persistGlobalRules(globalRules.filter((r) => r.id !== rule.id))}>
                        删除
                      </button>
                    </div>
                  ))
                )}
              </section>

              <section className="settings-card">
                <h3>项目级规则概览</h3>
                <p className="small muted">项目规则只对该项目生效，并会与命中的全局规则叠加执行。</p>
                {projectsQ.isLoading && <div className="empty">正在加载项目…</div>}
                {activeRules.map((p) => (
                  <div key={p.id} className="row" style={{ gap: 10, padding: '10px 0', borderTop: '1px solid var(--border)' }}>
                    <div className="grow">
                      <b>{p.name}</b>
                      <div className="small dim">
                        {p.active} 条启用 / 共 {p.total} 条规则
                      </div>
                    </div>
                    <a className="sm ghost btn" href={`/projects/${p.id}`} onClick={onClose} style={{ textDecoration: 'none' }}>
                      打开项目设置
                    </a>
                  </div>
                ))}
                {!projectsQ.isLoading && activeRules.length === 0 && <div className="empty">还没有项目</div>}
              </section>
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
}
