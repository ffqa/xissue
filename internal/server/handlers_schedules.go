package server

import (
	"fmt"
	"net/http"
	"time"
)

// ScheduledTask describes one background loop owned by the server/runner.
type ScheduledTask struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Interval string     `json:"interval"`
	Detail   string     `json:"detail"`
	Enabled  bool       `json:"enabled"`
	NextRun  *time.Time `json:"nextRun,omitempty"`
}

func (s *Server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	mon := s.cfg.MonitorSnapshot()
	runnerCfg := s.cfg.RunnerSnapshot()
	monitorStatus, _ := s.runner.GetMonitorStatus(r.Context())
	tasks := []ScheduledTask{
		{
			ID: "watcher", Name: "分支 / PR 监控", Interval: "90 秒", Enabled: mon.Enabled,
			Detail: "检查监控分支的新推送，以及关联 Run 的 PR 状态",
		},
		{
			ID: "issue-sync", Name: "远程 Issue 同步",
			Interval: fmt.Sprintf("%d 分钟", mon.SyncIntervalMin),
			Enabled:  mon.Enabled && mon.SyncIssues,
			Detail:   "从 GitHub/Gitea/GitLab 拉取新 Issue 并按规则预开发",
		},
		{
			ID: "auto-update", Name: "版本自动检查", Interval: "1 小时", Enabled: s.cfg.AutoUpdateEnabled(),
			Detail: "启动后 45 秒首次检查，之后每小时检查 GitHub Release",
		},
		{
			ID: "retention", Name: "运行数据清理", Interval: "6 小时", Enabled: true,
			Detail: fmt.Sprintf("Event 保留 %d 天，工作区保留 %d 天", runnerCfg.EventRetentionDays, runnerCfg.WorkspaceRetentionDays),
		},
		{
			ID: "log-prune", Name: "监控日志裁剪", Interval: "1 小时", Enabled: true,
			Detail: "裁剪超过 8 小时的监控日志，最多保留 2000 条",
		},
	}
	if monitorStatus != nil {
		tasks[0].NextRun = monitorStatus.NextCheckTime
	}
	s.syncMu.Lock()
	lastSync := s.lastIssueSync
	s.syncMu.Unlock()
	if !lastSync.IsZero() && mon.SyncIntervalMin > 0 {
		next := lastSync.Add(time.Duration(mon.SyncIntervalMin) * time.Minute)
		tasks[1].NextRun = &next
	}
	writeJSON(w, http.StatusOK, tasks)
}
