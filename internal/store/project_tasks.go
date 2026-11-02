package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/xpier/xissue/internal/domain"
)

// Project-scoped TODO items and test plans outlive the issue/spec that produced
// them: they are deleted only with the project.

type ProjectTodoPatch struct {
	Title  *string `json:"title"`
	Detail *string `json:"detail"`
	Status *string `json:"status"`
}

type ProjectTestPlanPatch struct {
	Detail *string `json:"detail"`
	Status *string `json:"status"`
}

const projectTodoCols = `id, project_id, title, detail, status, source_issue_id, source_key, created_at, updated_at, completed_at`

func scanProjectTodo(sc interface{ Scan(...any) error }) (domain.ProjectTodo, error) {
	var t domain.ProjectTodo
	var src sql.NullInt64
	var c, u string
	var done sql.NullString
	if err := sc.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Detail, &t.Status, &src, &t.SourceKey, &c, &u, &done); err != nil {
		return t, err
	}
	t.CreatedAt, t.UpdatedAt = parseTime(c), parseTime(u)
	t.CompletedAt = parseTimePtr(done)
	if src.Valid {
		id := src.Int64
		t.SourceIssueID = &id
	}
	return t, nil
}

const projectTestPlanCols = `id, project_id, base_title, title, repeat_index, detail, status, source_issue_id, source_key, created_at, updated_at, completed_at`

func scanProjectTestPlan(sc interface{ Scan(...any) error }) (domain.ProjectTestPlan, error) {
	var p domain.ProjectTestPlan
	var src sql.NullInt64
	var c, u string
	var done sql.NullString
	if err := sc.Scan(&p.ID, &p.ProjectID, &p.BaseTitle, &p.Title, &p.RepeatIndex, &p.Detail, &p.Status, &src, &p.SourceKey, &c, &u, &done); err != nil {
		return p, err
	}
	p.CreatedAt, p.UpdatedAt = parseTime(c), parseTime(u)
	p.CompletedAt = parseTimePtr(done)
	if src.Valid {
		id := src.Int64
		p.SourceIssueID = &id
	}
	return p, nil
}

func normalizeTaskStatus(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "open":
		return "open", nil
	case "done":
		return "done", nil
	}
	return "", fmt.Errorf("invalid status %q (want open|done)", status)
}

func cleanTaskTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("title is required")
	}
	if len([]rune(title)) > 500 {
		return "", errors.New("title is too long (max 500 characters)")
	}
	return title, nil
}

var testPlanSuffix = regexp.MustCompile(`(?i)\s+x\d+$`)

// normalizeTestPlanBase strips a trailing "xN" marker so repeated plans of the
// same kind share one base title.
func normalizeTestPlanBase(title string) string {
	return strings.TrimSpace(testPlanSuffix.ReplaceAllString(strings.TrimSpace(title), ""))
}

// ---------- TODO items ----------

func (s *Store) ListProjectTodos(ctx context.Context, projectID int64, query, status string) ([]domain.ProjectTodo, error) {
	where := []string{"project_id=?"}
	args := []any{projectID}
	if q := strings.TrimSpace(query); q != "" {
		like := "%" + q + "%"
		where = append(where, "(title LIKE ? OR detail LIKE ? OR source_key LIKE ?)")
		args = append(args, like, like, like)
	}
	if strings.TrimSpace(status) != "" {
		st, err := normalizeTaskStatus(status)
		if err != nil {
			return nil, err
		}
		where = append(where, "status=?")
		args = append(args, st)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+projectTodoCols+` FROM project_todos WHERE `+
		strings.Join(where, " AND ")+` ORDER BY CASE status WHEN 'open' THEN 0 ELSE 1 END, created_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProjectTodo{}
	for rows.Next() {
		t, err := scanProjectTodo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CreateProjectTodo(ctx context.Context, projectID int64, title, detail string, sourceIssueID *int64, sourceKey string) (*domain.ProjectTodo, error) {
	title, err := cleanTaskTitle(title)
	if err != nil {
		return nil, err
	}
	t := now()
	var src any
	if sourceIssueID != nil {
		src = *sourceIssueID
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO project_todos(project_id,title,detail,status,source_issue_id,source_key,created_at,updated_at) VALUES(?,?,?,'open',?,?,?,?)`,
		projectID, title, strings.TrimSpace(detail), src, strings.TrimSpace(sourceKey), t, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetProjectTodo(ctx, projectID, id)
}

func (s *Store) GetProjectTodo(ctx context.Context, projectID, id int64) (*domain.ProjectTodo, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+projectTodoCols+` FROM project_todos WHERE id=? AND project_id=?`, id, projectID)
	t, err := scanProjectTodo(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) UpdateProjectTodo(ctx context.Context, projectID, id int64, patch ProjectTodoPatch) (*domain.ProjectTodo, error) {
	old, err := s.GetProjectTodo(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	sets := []string{}
	args := []any{}
	if patch.Title != nil {
		title, err := cleanTaskTitle(*patch.Title)
		if err != nil {
			return nil, err
		}
		sets, args = append(sets, "title=?"), append(args, title)
	}
	if patch.Detail != nil {
		sets, args = append(sets, "detail=?"), append(args, strings.TrimSpace(*patch.Detail))
	}
	if patch.Status != nil {
		st, err := normalizeTaskStatus(*patch.Status)
		if err != nil {
			return nil, err
		}
		sets, args = append(sets, "status=?"), append(args, st)
		if st == "done" {
			if old.Status != "done" {
				sets, args = append(sets, "completed_at=?"), append(args, now())
			}
		} else {
			sets = append(sets, "completed_at=NULL")
		}
	}
	if len(sets) == 0 {
		return old, nil
	}
	sets, args = append(sets, "updated_at=?"), append(args, now())
	args = append(args, id, projectID)
	if _, err := s.db.ExecContext(ctx, `UPDATE project_todos SET `+strings.Join(sets, ",")+` WHERE id=? AND project_id=?`, args...); err != nil {
		return nil, err
	}
	return s.GetProjectTodo(ctx, projectID, id)
}

func (s *Store) DeleteProjectTodo(ctx context.Context, projectID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM project_todos WHERE id=? AND project_id=?`, id, projectID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Test plans ----------

func (s *Store) ListProjectTestPlans(ctx context.Context, projectID int64, query, status string) ([]domain.ProjectTestPlan, error) {
	where := []string{"project_id=?"}
	args := []any{projectID}
	if q := strings.TrimSpace(query); q != "" {
		like := "%" + q + "%"
		where = append(where, "(title LIKE ? OR base_title LIKE ? OR detail LIKE ? OR source_key LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if strings.TrimSpace(status) != "" {
		st, err := normalizeTaskStatus(status)
		if err != nil {
			return nil, err
		}
		where = append(where, "status=?")
		args = append(args, st)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+projectTestPlanCols+` FROM project_test_plans WHERE `+
		strings.Join(where, " AND ")+` ORDER BY CASE status WHEN 'open' THEN 0 ELSE 1 END, created_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProjectTestPlan{}
	for rows.Next() {
		p, err := scanProjectTestPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateProjectTestPlan numbers repeated plans of the same kind: the first is
// stored as-is, and adding a second one renames the first to "<base> x1" and
// stores the new one as "<base> x2" (then x3, x4, …).
func (s *Store) CreateProjectTestPlan(ctx context.Context, projectID int64, title, detail string, sourceIssueID *int64, sourceKey string) (*domain.ProjectTestPlan, error) {
	title, err := cleanTaskTitle(title)
	if err != nil {
		return nil, err
	}
	base := normalizeTestPlanBase(title)
	if base == "" {
		return nil, errors.New("title is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var count, maxIndex int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(repeat_index),0) FROM project_test_plans WHERE project_id=? AND base_title=?`, projectID, base).
		Scan(&count, &maxIndex); err != nil {
		return nil, err
	}
	finalTitle := base
	index := 0
	if count > 0 {
		var plainID int64
		_ = tx.QueryRowContext(ctx, `SELECT id FROM project_test_plans WHERE project_id=? AND base_title=? AND repeat_index=0 LIMIT 1`, projectID, base).Scan(&plainID)
		t := now()
		if plainID != 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE project_test_plans SET title=?, repeat_index=1, updated_at=? WHERE id=?`,
				base+" x1", t, plainID); err != nil {
				return nil, err
			}
			maxIndex = 1
		}
		index = maxIndex + 1
		finalTitle = fmt.Sprintf("%s x%d", base, index)
	}

	t := now()
	var src any
	if sourceIssueID != nil {
		src = *sourceIssueID
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO project_test_plans(project_id,base_title,title,repeat_index,detail,status,source_issue_id,source_key,created_at,updated_at) VALUES(?,?,?,?,?,'open',?,?,?,?)`,
		projectID, base, finalTitle, index, strings.TrimSpace(detail), src, strings.TrimSpace(sourceKey), t, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetProjectTestPlan(ctx, projectID, id)
}

func (s *Store) GetProjectTestPlan(ctx context.Context, projectID, id int64) (*domain.ProjectTestPlan, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+projectTestPlanCols+` FROM project_test_plans WHERE id=? AND project_id=?`, id, projectID)
	p, err := scanProjectTestPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) UpdateProjectTestPlan(ctx context.Context, projectID, id int64, patch ProjectTestPlanPatch) (*domain.ProjectTestPlan, error) {
	old, err := s.GetProjectTestPlan(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	sets := []string{}
	args := []any{}
	if patch.Detail != nil {
		sets, args = append(sets, "detail=?"), append(args, strings.TrimSpace(*patch.Detail))
	}
	if patch.Status != nil {
		st, err := normalizeTaskStatus(*patch.Status)
		if err != nil {
			return nil, err
		}
		sets, args = append(sets, "status=?"), append(args, st)
		if st == "done" {
			if old.Status != "done" {
				sets, args = append(sets, "completed_at=?"), append(args, now())
			}
		} else {
			sets = append(sets, "completed_at=NULL")
		}
	}
	if len(sets) == 0 {
		return old, nil
	}
	sets, args = append(sets, "updated_at=?"), append(args, now())
	args = append(args, id, projectID)
	if _, err := s.db.ExecContext(ctx, `UPDATE project_test_plans SET `+strings.Join(sets, ",")+` WHERE id=? AND project_id=?`, args...); err != nil {
		return nil, err
	}
	return s.GetProjectTestPlan(ctx, projectID, id)
}

func (s *Store) DeleteProjectTestPlan(ctx context.Context, projectID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM project_test_plans WHERE id=? AND project_id=?`, id, projectID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- Lookups by id (MCP / automation, no project in scope) ----------

func (s *Store) GetProjectTodoByID(ctx context.Context, id int64) (*domain.ProjectTodo, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+projectTodoCols+` FROM project_todos WHERE id=?`, id)
	t, err := scanProjectTodo(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) UpdateProjectTodoByID(ctx context.Context, id int64, patch ProjectTodoPatch) (*domain.ProjectTodo, error) {
	item, err := s.GetProjectTodoByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.UpdateProjectTodo(ctx, item.ProjectID, id, patch)
}

func (s *Store) DeleteProjectTodoByID(ctx context.Context, id int64) error {
	item, err := s.GetProjectTodoByID(ctx, id)
	if err != nil {
		return err
	}
	return s.DeleteProjectTodo(ctx, item.ProjectID, id)
}

func (s *Store) GetProjectTestPlanByID(ctx context.Context, id int64) (*domain.ProjectTestPlan, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+projectTestPlanCols+` FROM project_test_plans WHERE id=?`, id)
	p, err := scanProjectTestPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) UpdateProjectTestPlanByID(ctx context.Context, id int64, patch ProjectTestPlanPatch) (*domain.ProjectTestPlan, error) {
	item, err := s.GetProjectTestPlanByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.UpdateProjectTestPlan(ctx, item.ProjectID, id, patch)
}

func (s *Store) DeleteProjectTestPlanByID(ctx context.Context, id int64) error {
	item, err := s.GetProjectTestPlanByID(ctx, id)
	if err != nil {
		return err
	}
	return s.DeleteProjectTestPlan(ctx, item.ProjectID, id)
}

// ensureTestPlanForIssue records a regression plan for a completed issue, unless
// the project already has a plan for that issue or one with the same base title.
func (s *Store) ensureTestPlanForIssue(ctx context.Context, issue *domain.Issue) error {
	if issue == nil {
		return nil
	}
	base := normalizeTestPlanBase(issue.Title)
	if base == "" {
		base = issue.Key
	}
	var existing int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM project_test_plans WHERE project_id=? AND (source_issue_id=? OR base_title=?) LIMIT 1`,
		issue.ProjectID, issue.ID, base).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	srcID := issue.ID
	_, err = s.CreateProjectTestPlan(ctx, issue.ProjectID, base, "", &srcID, issue.Key)
	return err
}
