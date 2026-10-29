package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ScheduledTask describes one background loop owned by the server/runner.
type ScheduledTask struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Interval        string     `json:"interval"`
	IntervalSeconds int        `json:"intervalSeconds"`
	Detail          string     `json:"detail"`
	Enabled         bool       `json:"enabled"`
	Settings        string     `json:"settings,omitempty"` // monitor | settings
	Running         bool       `json:"running"`
	NextRun         *time.Time `json:"nextRun,omitempty"`
}

func (s *Server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	mon := s.cfg.MonitorSnapshot()
	runnerCfg := s.cfg.RunnerSnapshot()
	monitorStatus, _ := s.runner.GetMonitorStatus(r.Context())
	tasks := []ScheduledTask{
		{
			ID: "watcher", Name: "分支 / PR 监控", Interval: "90 秒", IntervalSeconds: 90, Enabled: mon.Enabled,
			Detail: "检查监控分支的新推送，以及关联 Run 的 PR 状态", Settings: "monitor",
		},
		{
			ID: "issue-sync", Name: "远程 Issue 同步",
			Interval: fmt.Sprintf("%d 分钟", mon.SyncIntervalMin), IntervalSeconds: mon.SyncIntervalMin * 60,
			Enabled: mon.Enabled && mon.SyncIssues,
			Detail:  "从 GitHub/Gitea/GitLab 拉取新 Issue 并按规则预开发", Settings: "monitor",
		},
		{
			ID: "auto-update", Name: "版本自动检查", Interval: "1 小时", IntervalSeconds: 3600, Enabled: s.cfg.AutoUpdateEnabled(),
			Detail: "启动后 45 秒首次检查，之后每小时检查 GitHub Release", Settings: "settings",
		},
		{
			ID: "retention", Name: "运行数据清理", Interval: "6 小时", IntervalSeconds: 21600, Enabled: true,
			Detail: fmt.Sprintf("Event 保留 %d 天，工作区保留 %d 天", runnerCfg.EventRetentionDays, runnerCfg.WorkspaceRetentionDays),
		},
		{
			ID: "log-prune", Name: "监控日志裁剪", Interval: "1 小时", IntervalSeconds: 3600, Enabled: true,
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
	s.scheduleMu.Lock()
	for i := range tasks {
		tasks[i].Running = s.scheduleRunning[tasks[i].ID]
	}
	s.scheduleMu.Unlock()
	writeJSON(w, http.StatusOK, tasks)
}

// handleRunSchedule runs one periodic task immediately. The request returns
// after it is accepted; the task runs in the background and its flag is
// visible on the next schedules poll.
func (s *Server) handleRunSchedule(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	valid := map[string]bool{"watcher": true, "issue-sync": true, "auto-update": true, "retention": true, "log-prune": true}
	if !valid[id] {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown schedule %q", id))
		return
	}
	s.scheduleMu.Lock()
	if s.scheduleRunning[id] {
		s.scheduleMu.Unlock()
		writeErr(w, http.StatusConflict, errors.New("该定时任务正在执行"))
		return
	}
	s.scheduleRunning[id] = true
	s.scheduleMu.Unlock()
	go func() {
		defer func() {
			s.scheduleMu.Lock()
			delete(s.scheduleRunning, id)
			s.scheduleMu.Unlock()
		}()
		switch id {
		case "watcher":
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_, _, err := s.runner.CheckWatchesAndPRs(ctx, 0)
			if err != nil {
				s.runner.LogMonitor("WARN", "schedule", "手动执行分支/PR 监控失败: "+err.Error())
			}
		case "issue-sync":
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			s.runIssueSync(ctx)
		case "auto-update":
			s.checkAutoUpdate()
		case "retention":
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			s.runner.PruneRunStorageNow(ctx)
		case "log-prune":
			_, _ = s.runner.PruneMonitorLogs(8 * time.Hour)
		}
	}()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "已开始执行"})
}
