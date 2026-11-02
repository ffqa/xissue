package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestProjectTodoLifecycleAndSearch(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proj, err := st.CreateProject(ctx, "Todo Project", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateProject(ctx, "Other Project", "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := st.CreateProjectTodo(ctx, proj.ID, "登录流程", "支持手机号登录", nil, "SPEC-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProjectTodo(ctx, proj.ID, "导出报表", "", nil, "SPEC-2"); err != nil {
		t.Fatal(err)
	}

	list, err := st.ListProjectTodos(ctx, proj.ID, "", "")
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if found, err := st.ListProjectTodos(ctx, proj.ID, "手机号", ""); err != nil || len(found) != 1 || found[0].ID != first.ID {
		t.Fatalf("search by detail = %+v, %v", found, err)
	}
	if found, err := st.ListProjectTodos(ctx, proj.ID, "SPEC-2", ""); err != nil || len(found) != 1 {
		t.Fatalf("search by source key = %+v, %v", found, err)
	}

	done := "done"
	updated, err := st.UpdateProjectTodo(ctx, proj.ID, first.ID, ProjectTodoPatch{Status: &done})
	if err != nil || updated.Status != "done" || updated.CompletedAt == nil {
		t.Fatalf("complete todo = %+v, %v", updated, err)
	}
	openOnly, err := st.ListProjectTodos(ctx, proj.ID, "", "open")
	if err != nil || len(openOnly) != 1 {
		t.Fatalf("open list = %+v, %v", openOnly, err)
	}
	reopened, err := st.UpdateProjectTodo(ctx, proj.ID, first.ID, ProjectTodoPatch{Status: ptrString("open")})
	if err != nil || reopened.CompletedAt != nil {
		t.Fatalf("reopen todo = %+v, %v", reopened, err)
	}

	if _, err := st.UpdateProjectTodo(ctx, other.ID, first.ID, ProjectTodoPatch{Status: &done}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project update err = %v, want ErrNotFound", err)
	}
	if err := st.DeleteProjectTodo(ctx, proj.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetProjectTodo(ctx, proj.ID, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted todo err = %v, want ErrNotFound", err)
	}
}

func TestProjectTestPlanRepeatSuffix(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	proj, err := st.CreateProject(ctx, "Plan Project", "")
	if err != nil {
		t.Fatal(err)
	}

	first, err := st.CreateProjectTestPlan(ctx, proj.ID, "回归登录", "", nil, "SPEC-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Title != "回归登录" || first.RepeatIndex != 0 {
		t.Fatalf("first plan = %+v", first)
	}
	second, err := st.CreateProjectTestPlan(ctx, proj.ID, "回归登录", "", nil, "SPEC-2")
	if err != nil {
		t.Fatal(err)
	}
	if second.Title != "回归登录 x2" || second.RepeatIndex != 2 {
		t.Fatalf("second plan = %+v", second)
	}
	renamed, err := st.GetProjectTestPlan(ctx, proj.ID, first.ID)
	if err != nil || renamed.Title != "回归登录 x1" || renamed.RepeatIndex != 1 {
		t.Fatalf("first plan after second = %+v, %v", renamed, err)
	}
	third, err := st.CreateProjectTestPlan(ctx, proj.ID, "回归登录", "", nil, "")
	if err != nil || third.Title != "回归登录 x3" {
		t.Fatalf("third plan = %+v, %v", third, err)
	}
	other, err := st.CreateProjectTestPlan(ctx, proj.ID, "导出报表", "", nil, "")
	if err != nil || other.Title != "导出报表" || other.BaseTitle != "导出报表" {
		t.Fatalf("other plan = %+v, %v", other, err)
	}
	// A title typed with a suffix is normalized to the same base.
	fourth, err := st.CreateProjectTestPlan(ctx, proj.ID, "回归登录 x3", "", nil, "")
	if err != nil || fourth.Title != "回归登录 x4" {
		t.Fatalf("fourth plan = %+v, %v", fourth, err)
	}

	found, err := st.ListProjectTestPlans(ctx, proj.ID, "x2", "")
	if err != nil || len(found) != 1 || found[0].ID != second.ID {
		t.Fatalf("search = %+v, %v", found, err)
	}
	done := "done"
	updated, err := st.UpdateProjectTestPlan(ctx, proj.ID, second.ID, ProjectTestPlanPatch{Status: &done})
	if err != nil || updated.Status != "done" || updated.CompletedAt == nil {
		t.Fatalf("complete plan = %+v, %v", updated, err)
	}
}

func ptrString(s string) *string { return &s }
