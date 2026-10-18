// Package store persists xIssue data in SQLite.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/xpier/xissue/internal/domain"
)

//go:embed schema.sql
var schemaSQL string

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single writer keeps SQLite simple and safe
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func parseTimePtr(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t := parseTime(ns.String)
	return &t
}

func nullInt(ns sql.NullInt64) *int64 {
	if !ns.Valid {
		return nil
	}
	v := ns.Int64
	return &v
}

// ---------- Projects ----------

func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, created_at, updated_at FROM projects ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		var c, u string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &c, &u); err != nil {
			return nil, err
		}
		p.CreatedAt, p.UpdatedAt = parseTime(c), parseTime(u)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		repos, err := s.ListRepos(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Repos = repos
	}
	return out, nil
}

func (s *Store) GetProject(ctx context.Context, id int64) (*domain.Project, error) {
	var p domain.Project
	var c, u string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, description, created_at, updated_at FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Description, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = parseTime(c), parseTime(u)
	p.Repos, err = s.ListRepos(ctx, id)
	return &p, err
}

func (s *Store) CreateProject(ctx context.Context, name, desc string) (*domain.Project, error) {
	t := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO projects(name, description, created_at, updated_at) VALUES(?,?,?,?)`, name, desc, t, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetProject(ctx, id)
}

func (s *Store) UpdateProject(ctx context.Context, id int64, name, desc *string) (*domain.Project, error) {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	if name != nil {
		p.Name = *name
	}
	if desc != nil {
		p.Description = *desc
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE projects SET name=?, description=?, updated_at=? WHERE id=?`, p.Name, p.Description, now(), id); err != nil {
		return nil, err
	}
	return s.GetProject(ctx, id)
}

func (s *Store) DeleteProject(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id)
	return err
}

// ---------- Repos ----------

const repoCols = `id, project_id, name, provider, remote_url, owner_repo, default_branch, test_command`

func scanRepo(sc interface{ Scan(...any) error }) (domain.Repo, error) {
	var r domain.Repo
	err := sc.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Provider, &r.RemoteURL, &r.OwnerRepo, &r.DefaultBranch, &r.TestCommand)
	return r, err
}

func (s *Store) ListRepos(ctx context.Context, projectID int64) ([]domain.Repo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+repoCols+` FROM repos WHERE project_id=? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Repo{}
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRepo(ctx context.Context, id int64) (*domain.Repo, error) {
	r, err := scanRepo(s.db.QueryRowContext(ctx, `SELECT `+repoCols+` FROM repos WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

func (s *Store) CreateRepo(ctx context.Context, r domain.Repo) (*domain.Repo, error) {
	if r.DefaultBranch == "" {
		r.DefaultBranch = "main"
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO repos(project_id, name, provider, remote_url, owner_repo, default_branch, test_command) VALUES(?,?,?,?,?,?,?)`,
		r.ProjectID, r.Name, r.Provider, r.RemoteURL, r.OwnerRepo, r.DefaultBranch, r.TestCommand)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetRepo(ctx, id)
}

func (s *Store) DeleteRepo(ctx context.Context, projectID, repoID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM repos WHERE id=? AND project_id=?`, repoID, projectID)
	return err
}

// ---------- Issues ----------

const issueCols = `i.id, i.project_id, i.key, i.source, i.repo_id, i.remote_number, i.remote_url, i.title, i.body, i.labels, i.status, i.created_at, i.updated_at,
  (SELECT r.id FROM runs r WHERE r.issue_id=i.id ORDER BY r.id DESC LIMIT 1),
  (SELECT r.status FROM runs r WHERE r.issue_id=i.id ORDER BY r.id DESC LIMIT 1)`

func scanIssue(sc interface{ Scan(...any) error }) (domain.Issue, error) {
	var i domain.Issue
	var repoID, num, runID sql.NullInt64
	var labels, c, u string
	var runStatus sql.NullString
	err := sc.Scan(&i.ID, &i.ProjectID, &i.Key, &i.Source, &repoID, &num, &i.RemoteURL, &i.Title, &i.Body, &labels, &i.Status, &c, &u, &runID, &runStatus)
	if err != nil {
		return i, err
	}
	i.RepoID, i.RemoteNumber, i.ActiveRunID = nullInt(repoID), nullInt(num), nullInt(runID)
	i.Awaiting = runStatus.String == string(domain.RunAwaiting)
	i.CreatedAt, i.UpdatedAt = parseTime(c), parseTime(u)
	_ = json.Unmarshal([]byte(labels), &i.Labels)
	if i.Labels == nil {
		i.Labels = []string{}
	}
	return i, nil
}

func (s *Store) ListIssues(ctx context.Context, projectID int64) ([]domain.Issue, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+issueCols+` FROM issues i WHERE i.project_id=? ORDER BY i.updated_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) GetIssue(ctx context.Context, id int64) (*domain.Issue, error) {
	i, err := scanIssue(s.db.QueryRowContext(ctx, `SELECT `+issueCols+` FROM issues i WHERE i.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &i, err
}

func (s *Store) FindIssueByKey(ctx context.Context, projectID int64, key string) (*domain.Issue, error) {
	i, err := scanIssue(s.db.QueryRowContext(ctx, `SELECT `+issueCols+` FROM issues i WHERE i.project_id=? AND i.key=?`, projectID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &i, err
}

// CreateLocalIssue allocates the next LOCAL-n key for the project.
func (s *Store) CreateLocalIssue(ctx context.Context, projectID int64, title, body string, labels []string) (*domain.Issue, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var seq int64
	if err := tx.QueryRowContext(ctx, `UPDATE projects SET local_seq=local_seq+1 WHERE id=? RETURNING local_seq`, projectID).Scan(&seq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if labels == nil {
		labels = []string{}
	}
	lb, _ := json.Marshal(labels)
	t := now()
	res, err := tx.ExecContext(ctx, `INSERT INTO issues(project_id, key, source, title, body, labels, status, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		projectID, fmt.Sprintf("LOCAL-%d", seq), "local", title, body, string(lb), domain.IssueTodo, t, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetIssue(ctx, id)
}

// UpsertRemoteIssue inserts or refreshes an issue imported from a git platform.
// Local status is preserved for existing issues.
func (s *Store) UpsertRemoteIssue(ctx context.Context, i domain.Issue) (created bool, err error) {
	lb, _ := json.Marshal(i.Labels)
	t := now()
	existing, err := s.FindIssueByKey(ctx, i.ProjectID, i.Key)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE issues SET title=?, body=?, labels=?, remote_url=?, updated_at=? WHERE id=?`,
			i.Title, i.Body, string(lb), i.RemoteURL, t, existing.ID)
		return false, err
	}
	if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO issues(project_id, key, source, repo_id, remote_number, remote_url, title, body, labels, status, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		i.ProjectID, i.Key, "remote", i.RepoID, i.RemoteNumber, i.RemoteURL, i.Title, i.Body, string(lb), domain.IssueTodo, t, t)
	return err == nil, err
}

type IssuePatch struct {
	Title  *string             `json:"title"`
	Body   *string             `json:"body"`
	Status *domain.IssueStatus `json:"status"`
}

func (s *Store) UpdateIssue(ctx context.Context, id int64, p IssuePatch) (*domain.Issue, error) {
	sets, args := []string{}, []any{}
	if p.Title != nil {
		sets, args = append(sets, "title=?"), append(args, *p.Title)
	}
	if p.Body != nil {
		sets, args = append(sets, "body=?"), append(args, *p.Body)
	}
	if p.Status != nil {
		if !domain.ValidIssueStatus(*p.Status) {
			return nil, fmt.Errorf("invalid status %q", *p.Status)
		}
		sets, args = append(sets, "status=?"), append(args, *p.Status)
	}
	sets, args = append(sets, "updated_at=?"), append(args, now())
	args = append(args, id)
	if _, err := s.db.ExecContext(ctx, `UPDATE issues SET `+strings.Join(sets, ",")+` WHERE id=?`, args...); err != nil {
		return nil, err
	}
	return s.GetIssue(ctx, id)
}

// MarkIssuePublished converts a local issue into a remote one, keeping history.
func (s *Store) MarkIssuePublished(ctx context.Context, id, repoID, number int64, key, url string) (*domain.Issue, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE issues SET source='remote', key=?, repo_id=?, remote_number=?, remote_url=?, updated_at=? WHERE id=?`,
		key, repoID, number, url, now(), id)
	if err != nil {
		return nil, err
	}
	return s.GetIssue(ctx, id)
}

// ---------- Comments ----------

func (s *Store) ListComments(ctx context.Context, issueID int64) ([]domain.Comment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, issue_id, author, body, created_at FROM comments WHERE issue_id=? ORDER BY id`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Comment{}
	for rows.Next() {
		var c domain.Comment
		var t string
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Author, &c.Body, &t); err != nil {
			return nil, err
		}
		c.CreatedAt = parseTime(t)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) AddComment(ctx context.Context, issueID int64, author, body string) (*domain.Comment, error) {
	t := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO comments(issue_id, author, body, created_at) VALUES(?,?,?,?)`, issueID, author, body, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	_, _ = s.db.ExecContext(ctx, `UPDATE issues SET updated_at=? WHERE id=?`, t, issueID)
	return &domain.Comment{ID: id, IssueID: issueID, Author: author, Body: body, CreatedAt: parseTime(t)}, nil
}

// ---------- Runs ----------

const runCols = `id, issue_id, agent, status, branch, workdir, session_id, summary, error, created_at, started_at, ended_at`

func scanRun(sc interface{ Scan(...any) error }) (domain.Run, error) {
	var r domain.Run
	var c string
	var st, en sql.NullString
	err := sc.Scan(&r.ID, &r.IssueID, &r.Agent, &r.Status, &r.Branch, &r.Workdir, &r.SessionID, &r.Summary, &r.Error, &c, &st, &en)
	r.CreatedAt, r.StartedAt, r.EndedAt = parseTime(c), parseTimePtr(st), parseTimePtr(en)
	return r, err
}

func (s *Store) CreateRun(ctx context.Context, issueID int64, agent string) (*domain.Run, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO runs(issue_id, agent, status, created_at) VALUES(?,?,?,?)`, issueID, agent, domain.RunQueued, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetRun(ctx, id)
}

func (s *Store) GetRun(ctx context.Context, id int64) (*domain.Run, error) {
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runCols+` FROM runs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

func (s *Store) ListRuns(ctx context.Context, issueID int64) ([]domain.Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+runCols+` FROM runs WHERE issue_id=? ORDER BY id DESC`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListActiveRuns returns runs that were in progress (used to recover after restart).
func (s *Store) ListActiveRuns(ctx context.Context) ([]domain.Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+runCols+` FROM runs WHERE status IN ('queued','starting','running','awaiting_permission')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RunUpdate holds optional fields to change on a run.
type RunUpdate struct {
	Status    *domain.RunStatus
	Branch    *string
	Workdir   *string
	SessionID *string
	Summary   *string
	Error     *string
	Started   bool
	Ended     bool
}

func (s *Store) UpdateRun(ctx context.Context, id int64, u RunUpdate) (*domain.Run, error) {
	sets, args := []string{}, []any{}
	add := func(col string, v any) { sets, args = append(sets, col+"=?"), append(args, v) }
	if u.Status != nil {
		add("status", *u.Status)
	}
	if u.Branch != nil {
		add("branch", *u.Branch)
	}
	if u.Workdir != nil {
		add("workdir", *u.Workdir)
	}
	if u.SessionID != nil {
		add("session_id", *u.SessionID)
	}
	if u.Summary != nil {
		add("summary", *u.Summary)
	}
	if u.Error != nil {
		add("error", *u.Error)
	}
	if u.Started {
		add("started_at", now())
	}
	if u.Ended {
		add("ended_at", now())
	}
	if len(sets) > 0 {
		args = append(args, id)
		if _, err := s.db.ExecContext(ctx, `UPDATE runs SET `+strings.Join(sets, ",")+` WHERE id=?`, args...); err != nil {
			return nil, err
		}
	}
	return s.GetRun(ctx, id)
}

// ---------- Run events ----------

func (s *Store) AppendEvent(ctx context.Context, runID int64, typ string, data any) (*domain.RunEvent, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	t := now()
	var ev domain.RunEvent
	err = s.db.QueryRowContext(ctx, `INSERT INTO run_events(run_id, seq, type, data, created_at)
		VALUES(?, COALESCE((SELECT MAX(seq) FROM run_events WHERE run_id=?),0)+1, ?, ?, ?) RETURNING id, seq`,
		runID, runID, typ, string(b), t).Scan(&ev.ID, &ev.Seq)
	if err != nil {
		return nil, err
	}
	ev.RunID, ev.Type, ev.Data, ev.CreatedAt = runID, typ, b, parseTime(t)
	return &ev, nil
}

func (s *Store) ListEvents(ctx context.Context, runID, afterSeq int64) ([]domain.RunEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, seq, type, data, created_at FROM run_events WHERE run_id=? AND seq>? ORDER BY seq`, runID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.RunEvent{}
	for rows.Next() {
		var e domain.RunEvent
		var d, t string
		if err := rows.Scan(&e.ID, &e.RunID, &e.Seq, &e.Type, &d, &t); err != nil {
			return nil, err
		}
		e.Data, e.CreatedAt = json.RawMessage(d), parseTime(t)
		out = append(out, e)
	}
	return out, rows.Err()
}
