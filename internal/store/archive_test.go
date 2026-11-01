package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/domain"
)

func TestArchiveDoneIssuesBefore(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proj, err := st.CreateProject(ctx, "Archive", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "Old done", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := domain.IssueDone
	if _, err := st.UpdateIssue(ctx, issue.ID, IssuePatch{Status: &done}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := st.db.ExecContext(ctx, `UPDATE issues SET completed_at=? WHERE id=?`, old, issue.ID); err != nil {
		t.Fatal(err)
	}

	n, err := st.ArchiveDoneIssuesBefore(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("archive = %d, %v", n, err)
	}
	got, err := st.GetIssue(ctx, issue.ID)
	if err != nil || got.ArchivedAt == nil {
		t.Fatalf("archivedAt = %v, %v", got.ArchivedAt, err)
	}
	n, err = st.ArchiveDoneIssuesBefore(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil || n != 0 {
		t.Fatalf("second archive = %d, %v", n, err)
	}
	todo := domain.IssueTodo
	if _, err := st.UpdateIssue(ctx, issue.ID, IssuePatch{Status: &todo}); err != nil {
		t.Fatal(err)
	}
	reopened, err := st.GetIssue(ctx, issue.ID)
	if err != nil || reopened.ArchivedAt != nil {
		t.Fatalf("archivedAt after reopen = %v, %v", reopened.ArchivedAt, err)
	}
}

func TestUpdateIssueArchivedFlag(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proj, err := st.CreateProject(ctx, "Archive flag", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "done issue", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := domain.IssueDone
	if _, err := st.UpdateIssue(ctx, issue.ID, IssuePatch{Status: &done}); err != nil {
		t.Fatal(err)
	}
	archived := true
	got, err := st.UpdateIssue(ctx, issue.ID, IssuePatch{Archived: &archived})
	if err != nil || got.ArchivedAt == nil {
		t.Fatalf("archive: archivedAt=%v err=%v", got.ArchivedAt, err)
	}
	list, err := st.ListActiveIssues(ctx, proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("archived issue still listed: %+v", list)
	}
	restored := false
	got, err = st.UpdateIssue(ctx, issue.ID, IssuePatch{Archived: &restored})
	if err != nil || got.ArchivedAt != nil {
		t.Fatalf("unarchive: archivedAt=%v err=%v", got.ArchivedAt, err)
	}
	if list, _ = st.ListActiveIssues(ctx, proj.ID); len(list) != 1 {
		t.Fatalf("unarchived issue missing from list: %+v", list)
	}
}

func TestArchiveDoneIssuesOverLimit(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proj, err := st.CreateProject(ctx, "Over limit", "")
	if err != nil {
		t.Fatal(err)
	}
	done := domain.IssueDone
	var ids []int64
	for i := 0; i < 3; i++ {
		issue, err := st.CreateLocalIssue(ctx, proj.ID, fmt.Sprintf("done %d", i), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpdateIssue(ctx, issue.ID, IssuePatch{Status: &done}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, issue.ID)
	}
	n, err := st.ArchiveDoneIssuesOverLimit(ctx, 2)
	if err != nil || n != 1 {
		t.Fatalf("archive = %d, %v", n, err)
	}
	oldest, err := st.GetIssue(ctx, ids[0])
	if err != nil || oldest.ArchivedAt == nil {
		t.Fatalf("oldest not archived: %v, %v", oldest.ArchivedAt, err)
	}
	for _, id := range ids[1:] {
		issue, _ := st.GetIssue(ctx, id)
		if issue.ArchivedAt != nil {
			t.Fatalf("issue %d archived but within limit", id)
		}
	}
	if n, err := st.ArchiveDoneIssuesOverLimit(ctx, 2); err != nil || n != 0 {
		t.Fatalf("second archive = %d, %v", n, err)
	}
}

func TestArchiveDoneIssuesOverLimitUsesCompletionTime(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proj, err := st.CreateProject(ctx, "Completion order", "")
	if err != nil {
		t.Fatal(err)
	}
	done := domain.IssueDone
	var ids []int64
	for i := 0; i < 3; i++ {
		issue, err := st.CreateLocalIssue(ctx, proj.ID, fmt.Sprintf("done %d", i), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpdateIssue(ctx, issue.ID, IssuePatch{Status: &done}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, issue.ID)
	}
	// A comment bumps updated_at but must not make the oldest issue look newest.
	if _, err := st.AddComment(ctx, ids[0], "user", "late comment"); err != nil {
		t.Fatal(err)
	}

	n, err := st.ArchiveDoneIssuesOverLimit(ctx, 2)
	if err != nil || n != 1 {
		t.Fatalf("archive = %d, %v", n, err)
	}
	for i, id := range ids {
		issue, err := st.GetIssue(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && issue.ArchivedAt == nil {
			t.Fatalf("oldest completed issue %d was not archived", id)
		}
		if i > 0 && issue.ArchivedAt != nil {
			t.Fatalf("issue %d archived but within limit", id)
		}
	}
}

func TestArchiveDoneIssuesBeforeUsesCompletionTime(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proj, err := st.CreateProject(ctx, "Completion cutoff", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "done long ago", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := domain.IssueDone
	if _, err := st.UpdateIssue(ctx, issue.ID, IssuePatch{Status: &done}); err != nil {
		t.Fatal(err)
	}
	// Completed 40 days ago, but commented on today (updated_at is fresh).
	old := time.Now().Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := st.db.ExecContext(ctx, `UPDATE issues SET completed_at=? WHERE id=?`, old, issue.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddComment(ctx, issue.ID, "user", "recent comment"); err != nil {
		t.Fatal(err)
	}

	n, err := st.ArchiveDoneIssuesBefore(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("archive = %d, %v", n, err)
	}
}
