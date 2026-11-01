package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestProjectPinnedPatchAndOrder(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, err := st.CreateProject(ctx, "Alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateProject(ctx, "Beta", "")
	if err != nil {
		t.Fatal(err)
	}
	pin := true
	if _, err := st.UpdateProject(ctx, b.ID, ProjectPatch{Pinned: &pin}); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 || list[0].ID != b.ID || !list[0].Pinned {
		t.Fatalf("pinned project not first: %+v", list)
	}
	unpin := false
	got, err := st.UpdateProject(ctx, b.ID, ProjectPatch{Pinned: &unpin})
	if err != nil || got.Pinned {
		t.Fatalf("unpin: pinned=%v err=%v", got.Pinned, err)
	}
	list, err = st.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ID != a.ID {
		t.Fatalf("order after unpin: %d first, want %d", list[0].ID, a.ID)
	}
}

func TestOpenMigratesPinnedColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE projects (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open on old schema: %v", err)
	}
	defer st.Close()
	p, err := st.CreateProject(context.Background(), "Migrated", "")
	if err != nil {
		t.Fatalf("CreateProject after migration: %v", err)
	}
	if p.Pinned {
		t.Fatal("migrated project unexpectedly pinned")
	}
}
