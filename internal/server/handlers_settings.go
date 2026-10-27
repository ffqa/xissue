package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/runner"
)

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}
	rep, err := s.st.UsageReport(r.Context(), time.Now().AddDate(0, 0, -days))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handleMonitorStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.runner.GetMonitorStatus(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	mon := s.cfg.MonitorSnapshot()
	st.IssueSyncEnabled = mon.SyncIssues
	st.IssueSyncIntervalMin = mon.SyncIntervalMin
	s.syncMu.Lock()
	if !s.lastIssueSync.IsZero() {
		t := s.lastIssueSync
		st.LastIssueSyncTime = &t
	}
	st.LastIssueSyncCount = s.lastSyncImported
	st.LastIssueSyncError = s.lastSyncError
	s.syncMu.Unlock()
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"autoUpdate":            s.cfg.AutoUpdateEnabled(),
		"pluginUISandbox":       s.cfg.PluginUISandboxEnabled(),
		"defaultAgent":          s.cfg.DefaultAgentName(),
		"agentRoles":            s.cfg.AgentRolesSnapshot(),
		"globalAutomationRules": s.cfg.GlobalAutomationRulesSnapshot(),
	})
}

func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AutoUpdate            *bool                    `json:"autoUpdate"`
		PluginUISandbox       *bool                    `json:"pluginUISandbox"`
		DefaultAgent          *string                  `json:"defaultAgent"`
		AgentRoles            *config.AgentRolesConfig `json:"agentRoles"`
		GlobalAutomationRules *[]domain.AutomationRule `json:"globalAutomationRules"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	changed := false
	if body.AutoUpdate != nil {
		s.cfg.SetAutoUpdate(*body.AutoUpdate)
		changed = true
	}
	if body.PluginUISandbox != nil {
		s.cfg.SetPluginUISandbox(*body.PluginUISandbox)
		changed = true
	}
	if body.DefaultAgent != nil {
		s.cfg.SetDefaultAgent(*body.DefaultAgent)
		changed = true
	}
	if body.AgentRoles != nil {
		s.cfg.SetAgentRoles(*body.AgentRoles)
		changed = true
	}
	if body.GlobalAutomationRules != nil {
		s.cfg.SetGlobalAutomationRules(*body.GlobalAutomationRules)
		changed = true
	}
	if changed {
		if err := s.cfg.Save(); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"autoUpdate":            s.cfg.AutoUpdateEnabled(),
		"pluginUISandbox":       s.cfg.PluginUISandboxEnabled(),
		"defaultAgent":          s.cfg.DefaultAgentName(),
		"agentRoles":            s.cfg.AgentRolesSnapshot(),
		"globalAutomationRules": s.cfg.GlobalAutomationRulesSnapshot(),
	})
}

func (s *Server) handleGetMonitorConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.MaskedMonitorSnapshot())
}

func (s *Server) handleUpdateMonitorConfig(w http.ResponseWriter, r *http.Request) {
	var body config.MonitorConfig
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.cfg.UpdateMonitor(body); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	status := "运行中"
	if !body.Enabled {
		status = "已暂停"
	}
	s.runner.LogMonitor("EVENT", "watcher", fmt.Sprintf("监控配置已更新: 状态=%s, 活跃时段=%s", status, body.ActiveHours))
	s.triggerIssueSyncNow()
	writeJSON(w, http.StatusOK, s.cfg.MaskedMonitorSnapshot())
}

func (s *Server) handlePatchMonitorConfig(w http.ResponseWriter, r *http.Request) {
	var body config.MonitorPatch
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.cfg.PatchMonitor(body); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if body.Enabled != nil {
		status := "运行中"
		if !*body.Enabled {
			status = "已暂停"
		}
		s.runner.LogMonitor("EVENT", "watcher", fmt.Sprintf("监控状态切换: %s", status))
	}
	s.triggerIssueSyncNow()
	writeJSON(w, http.StatusOK, s.cfg.MaskedMonitorSnapshot())
}

func (s *Server) handlePruneRunStorage(w http.ResponseWriter, r *http.Request) {
	stats, err := s.runner.PruneRunStorageNow(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.usageMu.Lock()
	s.dataSizeAt = time.Time{} // force the next status sample to rescan
	s.usageMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": len(stats.Errors) == 0, "stats": stats})
}

func (s *Server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	res, err := s.runner.SendTestNotification(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleMonitorLogs(w http.ResponseWriter, r *http.Request) {
	hoursStr := r.URL.Query().Get("hours")
	hours := 8
	if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
		hours = h
	}
	logs := s.runner.GetMonitorLogs(r.Context(), hours)
	if logs == nil {
		logs = []runner.MonitorLogEntry{}
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) handleMonitorTrigger(w http.ResponseWriter, r *http.Request) {
	watchesTriggered, prsUpdated, err := s.runner.CheckWatchesAndPRs(r.Context(), 0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"watchesTriggered": watchesTriggered,
		"prsUpdated":       prsUpdated,
		"message":          fmt.Sprintf("轮询检查完成: 触发 %d 个分支推送, %d 个 PR 状态流转", watchesTriggered, prsUpdated),
	})
}

func (s *Server) handleMonitorPrune(w http.ResponseWriter, r *http.Request) {
	hoursStr := r.URL.Query().Get("hours")
	hours := 8
	if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
		hours = h
	}
	pruned, err := s.runner.PruneMonitorLogs(time.Duration(hours) * time.Hour)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "prunedCount": pruned, "message": fmt.Sprintf("已成功清理并保留最近 %d 小时日志", hours)})
}
