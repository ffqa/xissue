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
	days := r.cfg.RunnerSnapshot().DoneArchiveDays
	if days <= 0 {
		return 0, nil
	}
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	n, err := r.st.ArchiveDoneIssuesBefore(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		r.LogMonitor("INFO", "retention", fmt.Sprintf("归档 %d 个超过 %d 天的已完成 Issue", n, days))
	}
	return n, nil
}
