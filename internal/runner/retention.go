package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/store"
)

// PruneStats summarizes one retention pass.
type PruneStats struct {
	RunsScanned  int      `json:"runsScanned"`
	RunsArchived int      `json:"runsArchived"`
	EventRows    int64    `json:"eventRows"`
	Workdirs     int      `json:"workdirs"`
	Errors       []string `json:"errors,omitempty"`
}

// PruneRunStorage archives old terminal runs, deletes their run_events rows and
// removes stale workspaces. now is injectable so retention can be tested without
// waiting days. A zero retention setting disables that part independently.
func (r *Runner) PruneRunStorage(ctx context.Context, now time.Time) (PruneStats, error) {
	r.retentionMu.Lock()
	defer r.retentionMu.Unlock()

	var stats PruneStats
	cfg := r.cfg.RunnerSnapshot()
	eventDays := cfg.EventRetentionDays
	workspaceDays := cfg.WorkspaceRetentionDays
	if eventDays <= 0 && workspaceDays <= 0 {
		return stats, nil
	}
	days := 0
	if eventDays > 0 {
		days = eventDays
	}
	if workspaceDays > 0 && (days == 0 || workspaceDays < days) {
		days = workspaceDays
	}
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	runs, err := r.st.ListEndedRunsBefore(ctx, cutoff)
	if err != nil {
		return stats, err
	}
	stats.RunsScanned = len(runs)
	eventCutoff := now.Add(-time.Duration(eventDays) * 24 * time.Hour)
	workspaceCutoff := now.Add(-time.Duration(workspaceDays) * 24 * time.Hour)
	for i := range runs {
		run := &runs[i]
		if run.EndedAt == nil {
			continue
		}
		needEvents := eventDays > 0 && run.EndedAt.Before(eventCutoff)
		if needEvents {
			hasEvents, herr := r.st.RunHasEvents(ctx, run.ID)
			if herr != nil {
				stats.addError(fmt.Sprintf("run %d: check events: %v", run.ID, herr))
				continue
			}
			needEvents = hasEvents
		}
		// completed means the agent finished but nobody reviewed/accepted the
		// changes yet. A submitted run normally had its workdir cleared on
		// success; a non-empty one means some repos failed to push, so keep it
		// too. Only failed/cancelled/discarded worktrees are safe to delete.
		keepWorkspace := run.Status == domain.RunCompleted || run.Status == domain.RunSubmitted
		needWorkspace := workspaceDays > 0 && !keepWorkspace &&
			run.Workdir != "" && run.EndedAt.Before(workspaceCutoff)
		if !needEvents && !needWorkspace {
			continue
		}
		issue, ierr := r.st.GetIssue(ctx, run.IssueID)
		if ierr != nil {
			stats.addError(fmt.Sprintf("run %d: load issue: %v", run.ID, ierr))
			continue
		}
		proj, perr := r.st.GetProject(ctx, issue.ProjectID)
		if perr != nil {
			stats.addError(fmt.Sprintf("run %d: load project: %v", run.ID, perr))
			continue
		}
		// Archive before deleting either the event rows or the worktree.
		if _, aerr := r.ArchiveRun(ctx, run.ID); aerr != nil {
			stats.addError(fmt.Sprintf("run %d: archive: %v", run.ID, aerr))
			continue
		}
		stats.RunsArchived++
		if needEvents {
			n, derr := r.st.DeleteRunEvents(ctx, run.ID)
			if derr != nil {
				stats.addError(fmt.Sprintf("run %d: delete events: %v", run.ID, derr))
			} else {
				stats.EventRows += n
			}
		}
		if needWorkspace {
			if strings.TrimSpace(run.RunnerID) != "" {
				if r.remote == nil || !r.remote.Online(run.RunnerID) {
					stats.addError(fmt.Sprintf("run %d: remote runner %s is offline; workspace retained", run.ID, run.RunnerID))
					continue
				}
				if derr := r.remote.Discard(ctx, run, proj); derr != nil {
					stats.addError(fmt.Sprintf("run %d: remove remote workdir: %v", run.ID, derr))
					continue
				}
			} else if rerr := r.removeRunWorkdir(ctx, run, proj); rerr != nil {
				stats.addError(fmt.Sprintf("run %d: remove workdir: %v", run.ID, rerr))
				continue
			}
			empty := ""
			if u, uerr := r.st.UpdateRun(ctx, run.ID, store.RunUpdate{Workdir: &empty}); uerr == nil {
				if r.notify != nil {
					r.notify.BroadcastRunUpdated(u)
				}
				stats.Workdirs++
			} else {
				stats.addError(fmt.Sprintf("run %d: clear workdir: %v", run.ID, uerr))
			}
		}
	}
	if stats.EventRows > 0 || stats.Workdirs > 0 {
		r.LogMonitor("INFO", "retention", fmt.Sprintf("清理完成：归档 %d 个 Run，删除 %d 条执行日志，移除 %d 个工作区", stats.RunsArchived, stats.EventRows, stats.Workdirs))
	}
	return stats, nil
}

func (s *PruneStats) addError(msg string) {
	if len(s.Errors) >= 20 {
		return
	}
	s.Errors = append(s.Errors, msg)
}

// PruneRunStorageNow is the background/manual entry point.
func (r *Runner) PruneRunStorageNow(ctx context.Context) (PruneStats, error) {
	return r.PruneRunStorage(ctx, time.Now())
}
