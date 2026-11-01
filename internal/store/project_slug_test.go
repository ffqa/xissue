package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSlugGenerationAndValidation(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.CreateProject(ctx, "Dup Name", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateProject(ctx, "Dup Name", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Slug != "dup-name" || b.Slug != "dup-name-2" {
		t.Fatalf("slugs = %q, %q", a.Slug, b.Slug)
	}

	custom := "custom-id"
	got, err := st.UpdateProject(ctx, a.ID, ProjectPatch{Slug: &custom})
	if err != nil || got.Slug != custom {
		t.Fatalf("explicit slug = %q, %v", got.Slug, err)
	}
	if _, err := st.UpdateProject(ctx, b.ID, ProjectPatch{Slug: &custom}); err == nil || !strings.Contains(err.Error(), "已被占用") {
		t.Fatalf("duplicate slug error = %v", err)
	}
	bad := "Bad Slug"
	if _, err := st.UpdateProject(ctx, b.ID, ProjectPatch{Slug: &bad}); err == nil {
		t.Fatal("invalid slug accepted")
	}

	cjk, err := st.CreateProject(ctx, "中文项目", "")
	if err != nil {
		t.Fatal(err)
	}
	if cjk.Slug == "" {
		t.Fatal("CJK project got an empty slug")
	}
}

func TestOpenBackfillsUniqueSlugs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE projects (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO projects(name, created_at, updated_at) VALUES ('Dup', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'), ('Dup', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	list, err := st.ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Slug == "" || list[1].Slug == "" || list[0].Slug == list[1].Slug {
		t.Fatalf("backfilled slugs = %+v", list)
	}
}
