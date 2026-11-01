package runner

import (
	"context"
	"fmt"
	"time"
)

// ArchiveDoneIssuesNow moves done issues older than the configured retention
// window into the archive. It does not delete anything.
func (r *Runner) ArchiveDoneIssuesNow(ctx context.Context) (int, error) {
	return r.ArchiveDoneIssues(ctx, time.Now())
}

// ArchiveDoneIssues is the injectable-time variant used by tests.
func (r *Runner) ArchiveDoneIssues(ctx context.Context, now time.Time) (int, error) {
	cfg := r.cfg.RunnerSnapshot()
	total := 0
	if days := cfg.DoneArchiveDays; days > 0 {
		cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
		n, err := r.st.ArchiveDoneIssuesBefore(ctx, cutoff)
		if err != nil {
			return total, err
		}
		if n > 0 {
			r.LogMonitor("INFO", "retention", fmt.Sprintf("归档 %d 个超过 %d 天的已完成 Issue", n, days))
		}
		total += n
	}
	if keep := cfg.DoneArchiveMaxCount; keep > 0 {
		n, err := r.st.ArchiveDoneIssuesOverLimit(ctx, keep)
		if err != nil {
			return total, err
		}
		if n > 0 {
			r.LogMonitor("INFO", "retention", fmt.Sprintf("归档 %d 个超出每项目 %d 条上限的已完成 Issue", n, keep))
		}
		total += n
	}
	return total, nil
}
