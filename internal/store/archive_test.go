package store

import (
	"context"
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
	if _, err := st.db.ExecContext(ctx, `UPDATE issues SET updated_at=? WHERE id=?`, old, issue.ID); err != nil {
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
