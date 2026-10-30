package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenMigratesLegacyBacklogToTodo(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.CreateProject(ctx, "Legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, proj.ID, "Legacy backlog", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE issues SET status='backlog' WHERE id=?`, issue.ID); err != nil {
		t.Fatal(err)
	}
	st.Close()

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "todo" {
		t.Fatalf("status = %q, want todo", got.Status)
	}
}
