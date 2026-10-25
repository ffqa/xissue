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
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/xpier/xissue/internal/domain"
)

//go:embed schema.sql
var schemaSQL string

var ErrNotFound = errors.New("not found")

// ErrRepoInUse prevents deleting a repository while issues still reference it.
// SQLite would otherwise null issues.repo_id via ON DELETE SET NULL, leaving
// runs and issues with an empty repository scope.
var ErrRepoInUse = errors.New("repository still has linked issues")

type Store struct {
	db *sql.DB

	obsMu          sync.RWMutex
	onIssueCreated func(*domain.Issue)
	onIssueStatus  func(*domain.Issue, domain.IssueStatus)
}

// SetIssueObservers registers lifecycle callbacks. Callbacks must return
// quickly; slow work belongs in a goroutine. Passing nil clears an observer.
func (s *Store) SetIssueObservers(created func(*domain.Issue), statusChanged func(*domain.Issue, domain.IssueStatus)) {
	s.obsMu.Lock()
	s.onIssueCreated = created
	s.onIssueStatus = statusChanged
	s.obsMu.Unlock()
}

func (s *Store) emitIssueCreated(issue *domain.Issue) {
	if issue == nil {
		return
	}
	s.obsMu.RLock()
	fn := s.onIssueCreated
	s.obsMu.RUnlock()
	if fn != nil {
		fn(issue)
	}
}

func (s *Store) emitIssueStatusChanged(issue *domain.Issue, previous domain.IssueStatus) {
	if issue == nil || issue.Status == previous {
		return
	}
	s.obsMu.RLock()
	fn := s.onIssueStatus
	s.obsMu.RUnlock()
	if fn != nil {
		fn(issue, previous)
	}
}

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// WAL allows readers while a writer is active. Keep one writer in practice
	// (SQLite serializes writes) but let board/event reads use separate
	// connections instead of queueing behind the agent's event inserts.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	// Incremental column migrations for existing databases.
	_, _ = db.Exec(`ALTER TABLE runs ADD COLUMN model TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE runs ADD COLUMN target_branch TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE runs ADD COLUMN pr_url TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE runs ADD COLUMN pr_state TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE runs ADD COLUMN change_record TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE projects ADD COLUMN dev_mode TEXT NOT NULL DEFAULT 'single_main'`)
	_, _ = db.Exec(`ALTER TABLE projects ADD COLUMN target_branch TEXT NOT NULL DEFAULT 'main'`)
	_, _ = db.Exec(`ALTER TABLE projects ADD COLUMN auto_pr_on_confirm INTEGER NOT NULL DEFAULT 1`)
	_, _ = db.Exec(`ALTER TABLE projects ADD COLUMN automation_rules TEXT NOT NULL DEFAULT '[]'`)
	_, _ = db.Exec(`ALTER TABLE projects ADD COLUMN pull_branches TEXT NOT NULL DEFAULT '[]'`)
	_, _ = db.Exec(`ALTER TABLE projects ADD COLUMN auto_develop INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE projects ADD COLUMN auto_develop_agent TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE projects ADD COLUMN auto_develop_labels TEXT NOT NULL DEFAULT '[]'`)
	_, _ = db.Exec(`ALTER TABLE projects ADD COLUMN auto_develop_text INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE projects ADD COLUMN auto_develop_authors TEXT NOT NULL DEFAULT '[]'`)
	_, _ = db.Exec(`ALTER TABLE projects ADD COLUMN auto_develop_daily_limit INTEGER NOT NULL DEFAULT 5`)
	_, _ = db.Exec(`ALTER TABLE runs ADD COLUMN auto_pr INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE comments ADD COLUMN remote_id TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE comments ADD COLUMN remote_url TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_comments_remote ON comments(issue_id, remote_id) WHERE remote_id <> ''`)
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

const projectCols = `id, name, description, dev_mode, target_branch, auto_pr_on_confirm, automation_rules, auto_develop, auto_develop_agent, auto_develop_labels, auto_develop_text, auto_develop_authors, auto_develop_daily_limit, pull_branches, created_at, updated_at`

func scanProject(s interface{ Scan(...any) error }) (domain.Project, error) {
	var p domain.Project
	var c, u string
	var autoPR, autoDevelop, autoDevelopText, autoDevelopDailyLimit int
	var devMode string
	var autoRulesRaw, autoLabelsRaw, autoAuthorsRaw, pullBranchesRaw string
	err := s.Scan(&p.ID, &p.Name, &p.Description, &devMode, &p.TargetBranch, &autoPR, &autoRulesRaw, &autoDevelop, &p.AutoDevelopAgent, &autoLabelsRaw, &autoDevelopText, &autoAuthorsRaw, &autoDevelopDailyLimit, &pullBranchesRaw, &c, &u)
	if err != nil {
		return p, err
	}
	p.CreatedAt, p.UpdatedAt = parseTime(c), parseTime(u)
	p.AutoPROnConfirm = autoPR != 0
	p.AutoDevelop = autoDevelop != 0
	p.AutoDevelopText = autoDevelopText != 0
	if autoLabelsRaw != "" && autoLabelsRaw != "null" {
		_ = json.Unmarshal([]byte(autoLabelsRaw), &p.AutoDevelopLabels)
	}
	if autoLabelsRaw == "" || autoLabelsRaw == "null" {
		p.AutoDevelopLabels = domain.DefaultAutoDevelopLabels()
	}
	if autoAuthorsRaw != "" && autoAuthorsRaw != "null" {
		_ = json.Unmarshal([]byte(autoAuthorsRaw), &p.AutoDevelopAuthors)
	}
	if pullBranchesRaw != "" && pullBranchesRaw != "null" {
		_ = json.Unmarshal([]byte(pullBranchesRaw), &p.PullBranches)
	}
	if autoDevelopDailyLimit <= 0 {
		autoDevelopDailyLimit = 5
	}
	p.AutoDevelopDailyLimit = autoDevelopDailyLimit
	p.DevMode = domain.DevMode(devMode)
	if p.DevMode != domain.DevModeMainDev && p.DevMode != domain.DevModeSingleMain {
		p.DevMode = domain.DevModeSingleMain
	}
	if p.TargetBranch == "" {
		if p.DevMode == domain.DevModeMainDev {
			p.TargetBranch = "dev"
		} else {
			p.TargetBranch = "main"
		}
	}
	if autoRulesRaw != "" {
		_ = json.Unmarshal([]byte(autoRulesRaw), &p.AutomationRules)
	}
	if p.AutomationRules == nil {
		p.AutomationRules = []domain.AutomationRule{}
	}
	return p, nil
}

func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+projectCols+` FROM projects ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
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
	p, err := scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Repos, err = s.ListRepos(ctx, id)
	return &p, err
}

func (s *Store) CreateProject(ctx context.Context, name, desc string, devMode ...domain.DevMode) (*domain.Project, error) {
	t := now()
	dm := domain.DevModeSingleMain
	if len(devMode) > 0 && devMode[0] == domain.DevModeMainDev {
		dm = domain.DevModeMainDev
	}
	targetBranch := "main"
	if dm == domain.DevModeMainDev {
		targetBranch = "dev"
	}
	labelsJSON, _ := json.Marshal(domain.DefaultAutoDevelopLabels())
	res, err := s.db.ExecContext(ctx, `INSERT INTO projects(name, description, dev_mode, target_branch, auto_pr_on_confirm, auto_develop_labels, auto_develop_daily_limit, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		name, desc, string(dm), targetBranch, 1, string(labelsJSON), 5, t, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetProject(ctx, id)
}

type ProjectPatch struct {
	Name                  *string                  `json:"name"`
	Description           *string                  `json:"description"`
	DevMode               *domain.DevMode          `json:"devMode"`
	TargetBranch          *string                  `json:"targetBranch"`
	AutoPROnConfirm       *bool                    `json:"autoPrOnConfirm"`
	AutoDevelop           *bool                    `json:"autoDevelop"`
	AutoDevelopAgent      *string                  `json:"autoDevelopAgent"`
	AutoDevelopLabels     *[]string                `json:"autoDevelopLabels"`
	AutoDevelopText       *bool                    `json:"autoDevelopText"`
	AutoDevelopAuthors    *[]string                `json:"autoDevelopAuthors"`
	AutoDevelopDailyLimit *int                     `json:"autoDevelopDailyLimit"`
	AutomationRules       *[]domain.AutomationRule `json:"automationRules"`
	PullBranches          *[]string                `json:"pullBranches"`
}

func (s *Store) UpdateProject(ctx context.Context, id int64, patch ProjectPatch) (*domain.Project, error) {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	if patch.Name != nil {
		p.Name = *patch.Name
	}
	if patch.Description != nil {
		p.Description = *patch.Description
	}
	if patch.DevMode != nil {
		dm := *patch.DevMode
		if dm == domain.DevModeMainDev {
			p.DevMode = domain.DevModeMainDev
		} else {
			p.DevMode = domain.DevModeSingleMain
		}
	}
	if patch.TargetBranch != nil {
		p.TargetBranch = strings.TrimSpace(*patch.TargetBranch)
	}
	if p.TargetBranch == "" {
		if p.DevMode == domain.DevModeMainDev {
			p.TargetBranch = "dev"
		} else {
			p.TargetBranch = "main"
		}
	}
	if patch.AutoPROnConfirm != nil {
		p.AutoPROnConfirm = *patch.AutoPROnConfirm
	}
	if patch.AutoDevelop != nil {
		p.AutoDevelop = *patch.AutoDevelop
	}
	if patch.AutoDevelopAgent != nil {
		p.AutoDevelopAgent = strings.TrimSpace(*patch.AutoDevelopAgent)
	}
	if patch.AutoDevelopLabels != nil {
		labels := make([]string, 0, len(*patch.AutoDevelopLabels))
		for _, label := range *patch.AutoDevelopLabels {
			if label = strings.TrimSpace(label); label != "" {
				labels = append(labels, label)
			}
		}
		p.AutoDevelopLabels = labels
	}
	if patch.AutoDevelopText != nil {
		p.AutoDevelopText = *patch.AutoDevelopText
	}
	if patch.AutoDevelopAuthors != nil {
		authors := make([]string, 0, len(*patch.AutoDevelopAuthors))
		for _, author := range *patch.AutoDevelopAuthors {
			if author = strings.TrimSpace(strings.TrimPrefix(author, "@")); author != "" {
				authors = append(authors, author)
			}
		}
		p.AutoDevelopAuthors = authors
	}
	if patch.AutoDevelopDailyLimit != nil {
		limit := *patch.AutoDevelopDailyLimit
		if limit <= 0 {
			limit = 5
		}
		if limit > 100 {
			limit = 100
		}
		p.AutoDevelopDailyLimit = limit
	}
	if p.AutoDevelopDailyLimit <= 0 {
		p.AutoDevelopDailyLimit = 5
	}
	if patch.AutomationRules != nil {
		p.AutomationRules = *patch.AutomationRules
	}
	if patch.PullBranches != nil {
		branches := make([]string, 0, len(*patch.PullBranches))
		for _, b := range *patch.PullBranches {
			if b = strings.TrimSpace(b); b != "" {
				branches = append(branches, b)
			}
		}
		p.PullBranches = branches
	}
	autoInt := 0
	if p.AutoPROnConfirm {
		autoInt = 1
	}
	autoDevInt := 0
	if p.AutoDevelop {
		autoDevInt = 1
	}
	autoDevTextInt := 0
	if p.AutoDevelopText {
		autoDevTextInt = 1
	}
	rulesJSON, _ := json.Marshal(p.AutomationRules)
	pullBranchesJSON, _ := json.Marshal(p.PullBranches)
	labelsJSON, _ := json.Marshal(p.AutoDevelopLabels)
	authorsJSON, _ := json.Marshal(p.AutoDevelopAuthors)
	if _, err := s.db.ExecContext(ctx, `UPDATE projects SET name=?, description=?, dev_mode=?, target_branch=?, auto_pr_on_confirm=?, automation_rules=?, auto_develop=?, auto_develop_agent=?, auto_develop_labels=?, auto_develop_text=?, auto_develop_authors=?, auto_develop_daily_limit=?, pull_branches=?, updated_at=? WHERE id=?`,
		p.Name, p.Description, string(p.DevMode), p.TargetBranch, autoInt, string(rulesJSON), autoDevInt, p.AutoDevelopAgent, string(labelsJSON), autoDevTextInt, string(authorsJSON), p.AutoDevelopDailyLimit, string(pullBranchesJSON), now(), id); err != nil {
		return nil, err
	}
	return s.GetProject(ctx, id)
}

func (s *Store) DeleteProject(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Delete all run_events belonging to issues of this project
	_, _ = tx.ExecContext(ctx, `DELETE FROM run_events WHERE run_id IN (
		SELECT id FROM runs WHERE issue_id IN (SELECT id FROM issues WHERE project_id = ?)
	)`, id)

	// 2. Delete all comments belonging to issues of this project
	_, _ = tx.ExecContext(ctx, `DELETE FROM comments WHERE issue_id IN (
		SELECT id FROM issues WHERE project_id = ?
	)`, id)

	// 3. Delete all runs belonging to issues of this project
	_, _ = tx.ExecContext(ctx, `DELETE FROM runs WHERE issue_id IN (
		SELECT id FROM issues WHERE project_id = ?
	)`, id)

	// 4. Delete all issues
	_, _ = tx.ExecContext(ctx, `DELETE FROM issues WHERE project_id = ?`, id)

	// 5. Delete all branch_watches
	_, _ = tx.ExecContext(ctx, `DELETE FROM branch_watches WHERE project_id = ?`, id)

	// 6. Delete all repos
	_, _ = tx.ExecContext(ctx, `DELETE FROM repos WHERE project_id = ?`, id)

	// 7. Delete related notifications (exact project boundary; avoid /projects/1
	// matching /projects/12 or /projects/100).
	relPrefix := "/projects/" + fmt.Sprintf("%d", id)
	absSuffix := "%/projects/" + fmt.Sprintf("%d", id)
	metaA := `%"projectId":` + fmt.Sprintf("%d", id) + `,%`
	metaB := `%"projectId":` + fmt.Sprintf("%d", id) + `}%`
	_, _ = tx.ExecContext(ctx, `DELETE FROM notifications WHERE link = ? OR link LIKE ? OR link LIKE ? OR link LIKE ? OR link LIKE ? OR link LIKE ? OR meta LIKE ? OR meta LIKE ?`,
		relPrefix, relPrefix+"/%", relPrefix+"?%", absSuffix, absSuffix+"/%", absSuffix+"?%", metaA, metaB)

	// 8. Delete project record
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id); err != nil {
		return err
	}

	return tx.Commit()
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
	r.Name = strings.TrimSpace(r.Name)
	if err := domain.ValidateRepoName(r.Name); err != nil {
		return nil, err
	}
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

func (s *Store) UpdateRepo(ctx context.Context, r domain.Repo) (*domain.Repo, error) {
	r.Name = strings.TrimSpace(r.Name)
	if err := domain.ValidateRepoName(r.Name); err != nil {
		return nil, err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE repos SET name=?, provider=?, remote_url=?, owner_repo=?, default_branch=?, test_command=? WHERE id=? AND project_id=?`,
		r.Name, r.Provider, r.RemoteURL, r.OwnerRepo, r.DefaultBranch, r.TestCommand, r.ID, r.ProjectID)
	if err != nil {
		return nil, err
	}
	return s.GetRepo(ctx, r.ID)
}

func (s *Store) DeleteRepo(ctx context.Context, projectID, repoID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM repos WHERE id=? AND project_id=? AND NOT EXISTS (SELECT 1 FROM issues WHERE repo_id=?)`, repoID, projectID, repoID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	repo, err := s.GetRepo(ctx, repoID)
	if err != nil {
		return err
	}
	if repo.ProjectID != projectID {
		return ErrNotFound
	}
	var count int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE repo_id=?`, repoID).Scan(&count)
	return fmt.Errorf("%w（%d 个 Issue 仍在引用）", ErrRepoInUse, count)
}

// ---------- Issues ----------

const issueCols = `i.id, i.project_id, i.key, i.source, i.repo_id, i.remote_number, i.remote_url, i.title, i.body, i.labels, i.status, i.created_at, i.updated_at,
  (SELECT r.id FROM runs r WHERE r.issue_id=i.id ORDER BY r.id DESC LIMIT 1),
  (SELECT r.status FROM runs r WHERE r.issue_id=i.id ORDER BY r.id DESC LIMIT 1),
  COALESCE((SELECT r.pr_url FROM runs r WHERE r.issue_id=i.id AND r.pr_url != '' ORDER BY r.id DESC LIMIT 1), ''),
  COALESCE((SELECT r.pr_state FROM runs r WHERE r.issue_id=i.id AND r.pr_url != '' ORDER BY r.id DESC LIMIT 1), '')`

func scanIssue(sc interface{ Scan(...any) error }) (domain.Issue, error) {
	var i domain.Issue
	var repoID, num, runID sql.NullInt64
	var labels, c, u string
	var runStatus sql.NullString
	err := sc.Scan(&i.ID, &i.ProjectID, &i.Key, &i.Source, &repoID, &num, &i.RemoteURL, &i.Title, &i.Body, &labels, &i.Status, &c, &u, &runID, &runStatus, &i.PRURL, &i.PRState)
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

// AddIssueLabels merges labels case-insensitively and returns the updated issue.
func (s *Store) AddIssueLabels(ctx context.Context, id int64, add ...string) (*domain.Issue, error) {
	issue, err := s.GetIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, label := range issue.Labels {
		seen[strings.ToLower(strings.TrimSpace(label))] = true
	}
	changed := false
	for _, label := range add {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		key := strings.ToLower(label)
		if seen[key] {
			continue
		}
		seen[key] = true
		issue.Labels = append(issue.Labels, label)
		changed = true
	}
	if !changed {
		return issue, nil
	}
	lb, _ := json.Marshal(issue.Labels)
	if _, err := s.db.ExecContext(ctx, `UPDATE issues SET labels=?, updated_at=? WHERE id=?`, string(lb), now(), id); err != nil {
		return nil, err
	}
	return s.GetIssue(ctx, id)
}

// CreateLocalIssue allocates the next LOCAL-n key for the project.
func (s *Store) CreateLocalIssue(ctx context.Context, projectID int64, title, body string, labels []string) (*domain.Issue, error) {
	return s.CreateLocalIssueInRepo(ctx, projectID, nil, title, body, labels)
}

// CreateLocalIssueInRepo creates a local issue optionally scoped to one repo.
// A nil repoID means the issue is project-wide and the agent may choose.
func (s *Store) CreateLocalIssueInRepo(ctx context.Context, projectID int64, repoID *int64, title, body string, labels []string) (*domain.Issue, error) {
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
	var repoArg any
	if repoID != nil {
		repoArg = *repoID
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO issues(project_id, key, source, repo_id, title, body, labels, status, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		projectID, fmt.Sprintf("LOCAL-%d", seq), "local", repoArg, title, body, string(lb), domain.IssueTodo, t, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	issue, err := s.GetIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	s.emitIssueCreated(issue)
	return issue, nil
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
	if err != nil {
		return false, err
	}
	if createdIssue, gerr := s.FindIssueByKey(ctx, i.ProjectID, i.Key); gerr == nil {
		s.emitIssueCreated(createdIssue)
	}
	return true, nil
}

type IssuePatch struct {
	Title  *string             `json:"title"`
	Body   *string             `json:"body"`
	Status *domain.IssueStatus `json:"status"`
}

func (s *Store) UpdateIssue(ctx context.Context, id int64, p IssuePatch) (*domain.Issue, error) {
	sets, args := []string{}, []any{}
	previousStatus := domain.IssueStatus("")
	if p.Status != nil {
		if !domain.ValidIssueStatus(*p.Status) {
			return nil, fmt.Errorf("invalid status %q", *p.Status)
		}
		if err := s.db.QueryRowContext(ctx, `SELECT status FROM issues WHERE id=?`, id).Scan(&previousStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	if p.Title != nil {
		sets, args = append(sets, "title=?"), append(args, *p.Title)
	}
	if p.Body != nil {
		sets, args = append(sets, "body=?"), append(args, *p.Body)
	}
	if p.Status != nil {
		sets, args = append(sets, "status=?"), append(args, *p.Status)
	}
	sets, args = append(sets, "updated_at=?"), append(args, now())
	args = append(args, id)
	if _, err := s.db.ExecContext(ctx, `UPDATE issues SET `+strings.Join(sets, ",")+` WHERE id=?`, args...); err != nil {
		return nil, err
	}
	issue, err := s.GetIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Status != nil {
		s.emitIssueStatusChanged(issue, previousStatus)
	}
	return issue, nil
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

// BindIssueRepo associates an issue with a repository.
func (s *Store) BindIssueRepo(ctx context.Context, issueID, repoID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE issues SET repo_id=?, updated_at=? WHERE id=?`, repoID, now(), issueID)
	return err
}

func (s *Store) DeleteIssue(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM issues WHERE id=?`, id)
	return err
}

func (s *Store) BatchDeleteIssues(ctx context.Context, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM issues WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ---------- Comments ----------

func (s *Store) ListComments(ctx context.Context, issueID int64) ([]domain.Comment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, issue_id, author, body, remote_id, remote_url, created_at FROM comments WHERE issue_id=? ORDER BY created_at, id`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Comment{}
	for rows.Next() {
		var c domain.Comment
		var t string
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Author, &c.Body, &c.RemoteID, &c.RemoteURL, &t); err != nil {
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

// UpsertRemoteComment stores a comment fetched from the remote tracker. It is
// idempotent by (issue_id, remote_id), and updates edited comments in place.
func (s *Store) UpsertRemoteComment(ctx context.Context, issueID int64, remoteID, author, body, remoteURL string, createdAt time.Time) (bool, bool, error) {
	remoteID = strings.TrimSpace(remoteID)
	if remoteID == "" {
		return false, false, nil
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	ts := createdAt.UTC().Format(time.RFC3339Nano)
	// xIssue may have posted a comment remotely after storing a local copy.
	// Adopt that local row instead of importing a duplicate.
	var localID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM comments WHERE issue_id=? AND remote_id='' AND body IN (?,?,?) ORDER BY id LIMIT 1`,
		issueID, body, "```text\n"+body+"\n```", "```\n"+body+"\n```",
	).Scan(&localID); err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE comments SET remote_id=?, remote_url=? WHERE id=?`, remoteID, remoteURL, localID)
		_, _ = s.db.ExecContext(ctx, `UPDATE issues SET updated_at=? WHERE id=?`, now(), issueID)
		return false, true, nil
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO comments(issue_id, author, body, remote_id, remote_url, created_at) VALUES(?,?,?,?,?,?)`,
		issueID, author, body, remoteID, remoteURL, ts,
	)
	if err != nil {
		return false, false, err
	}
	added, _ := res.RowsAffected()
	if added > 0 {
		_, _ = s.db.ExecContext(ctx, `UPDATE issues SET updated_at=? WHERE id=?`, now(), issueID)
		return true, false, nil
	}
	upd, err := s.db.ExecContext(ctx,
		`UPDATE comments SET author=?, body=?, remote_url=? WHERE issue_id=? AND remote_id=? AND (author<>? OR body<>? OR remote_url<>?)`,
		author, body, remoteURL, issueID, remoteID, author, body, remoteURL,
	)
	if err != nil {
		return false, false, err
	}
	changed, _ := upd.RowsAffected()
	if changed > 0 {
		_, _ = s.db.ExecContext(ctx, `UPDATE issues SET updated_at=? WHERE id=?`, now(), issueID)
	}
	return false, changed > 0, nil
}

// ---------- Runs ----------

const runCols = `id, issue_id, agent, model, status, branch, target_branch, workdir, session_id, summary, error, pr_url, pr_state, change_record, auto_pr, created_at, started_at, ended_at`

func scanRun(sc interface{ Scan(...any) error }) (domain.Run, error) {
	var r domain.Run
	var c string
	var st, en sql.NullString
	err := sc.Scan(&r.ID, &r.IssueID, &r.Agent, &r.Model, &r.Status, &r.Branch, &r.TargetBranch, &r.Workdir, &r.SessionID, &r.Summary, &r.Error, &r.PRURL, &r.PRState, &r.ChangeRecord, &r.AutoPR, &c, &st, &en)
	r.CreatedAt, r.StartedAt, r.EndedAt = parseTime(c), parseTimePtr(st), parseTimePtr(en)
	return r, err
}

func (s *Store) CreateRun(ctx context.Context, issueID int64, agent, model string) (*domain.Run, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO runs(issue_id, agent, model, status, created_at) VALUES(?,?,?,?,?)`, issueID, agent, model, domain.RunQueued, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetRun(ctx, id)
}

// CountAutoDevelopRunsSince counts automatic pre-development runs for a project
// created at or after the given time.
func (s *Store) CountAutoDevelopRunsSince(ctx context.Context, projectID int64, since time.Time) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs r JOIN issues i ON i.id = r.issue_id WHERE i.project_id = ? AND r.auto_pr = 1 AND r.created_at >= ?`,
		projectID, since.UTC().Format(time.RFC3339Nano)).Scan(&count)
	return count, err
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

func (s *Store) DeleteRun(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM runs WHERE id=?`, id)
	return err
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

// ListOpenPRRuns returns runs that have an open PR URL to monitor.
func (s *Store) ListOpenPRRuns(ctx context.Context) ([]domain.Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+runCols+` FROM runs WHERE pr_url != '' AND (pr_state = '' OR pr_state = 'open') ORDER BY id DESC`)
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
	Status       *domain.RunStatus
	Model        *string
	Branch       *string
	TargetBranch *string
	Workdir      *string
	SessionID    *string
	Summary      *string
	Error        *string
	PRURL        *string
	PRState      *string
	ChangeRecord *string
	AutoPR       *bool
	Started      bool
	Ended        bool
	Reopen       bool // clear ended_at when a completed run is queued for a follow-up
}

func (s *Store) UpdateRun(ctx context.Context, id int64, u RunUpdate) (*domain.Run, error) {
	sets, args := []string{}, []any{}
	add := func(col string, v any) { sets, args = append(sets, col+"=?"), append(args, v) }
	if u.Status != nil {
		add("status", *u.Status)
	}
	if u.Model != nil {
		add("model", *u.Model)
	}
	if u.Branch != nil {
		add("branch", *u.Branch)
	}
	if u.TargetBranch != nil {
		add("target_branch", *u.TargetBranch)
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
	if u.PRURL != nil {
		add("pr_url", *u.PRURL)
	}
	if u.PRState != nil {
		add("pr_state", *u.PRState)
	}
	if u.ChangeRecord != nil {
		add("change_record", *u.ChangeRecord)
	}
	if u.AutoPR != nil {
		add("auto_pr", *u.AutoPR)
	}
	if u.Started {
		add("started_at", now())
	}
	if u.Ended {
		add("ended_at", now())
	}
	if u.Reopen {
		add("ended_at", nil)
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

func scanEvents(rows *sql.Rows) ([]domain.RunEvent, error) {
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

func (s *Store) ListEvents(ctx context.Context, runID, afterSeq int64) ([]domain.RunEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, seq, type, data, created_at FROM run_events WHERE run_id=? AND seq>? ORDER BY seq`, runID, afterSeq)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

// ListEventsTail returns at most limit newest events, restored to ascending seq
// order. It keeps opening a long historical run from loading every event row.
func (s *Store) ListEventsTail(ctx context.Context, runID int64, limit int) ([]domain.RunEvent, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, run_id, seq, type, data, created_at FROM (SELECT id, run_id, seq, type, data, created_at FROM run_events WHERE run_id=? ORDER BY seq DESC LIMIT ?) ORDER BY seq`, runID, limit)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

// ListEndedRunsBefore returns terminal runs whose effective end time is older
// than cutoff, for retention cleanup.
func (s *Store) ListEndedRunsBefore(ctx context.Context, cutoff time.Time) ([]domain.Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+runCols+` FROM runs WHERE status IN ('completed','submitted','failed','cancelled','discarded') AND ended_at != '' AND ended_at < ? ORDER BY id`, cutoff.UTC().Format(time.RFC3339Nano))
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

func (s *Store) DeleteRunEvents(ctx context.Context, runID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM run_events WHERE run_id=?`, runID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------- Branch Watches ----------

const watchCols = `w.id, w.project_id, w.repo_id, COALESCE(r.name, ''), w.branch, w.last_sha, w.last_message, w.last_author, w.updated_at`

func scanBranchWatch(sc interface{ Scan(...any) error }) (domain.BranchWatch, error) {
	var w domain.BranchWatch
	var u string
	err := sc.Scan(&w.ID, &w.ProjectID, &w.RepoID, &w.RepoName, &w.Branch, &w.LastSHA, &w.LastMessage, &w.LastAuthor, &u)
	w.UpdatedAt = parseTime(u)
	return w, err
}

func (s *Store) ListBranchWatches(ctx context.Context, projectID int64) ([]domain.BranchWatch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+watchCols+` FROM branch_watches w LEFT JOIN repos r ON r.id=w.repo_id WHERE w.project_id=? ORDER BY w.id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.BranchWatch{}
	for rows.Next() {
		w, err := scanBranchWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) ListAllBranchWatches(ctx context.Context) ([]domain.BranchWatch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+watchCols+` FROM branch_watches w LEFT JOIN repos r ON r.id=w.repo_id ORDER BY w.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.BranchWatch{}
	for rows.Next() {
		w, err := scanBranchWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) GetBranchWatch(ctx context.Context, id int64) (*domain.BranchWatch, error) {
	w, err := scanBranchWatch(s.db.QueryRowContext(ctx, `SELECT `+watchCols+` FROM branch_watches w LEFT JOIN repos r ON r.id=w.repo_id WHERE w.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &w, err
}

func (s *Store) UpsertBranchWatch(ctx context.Context, projectID, repoID int64, branch, sha, msg, author string) (*domain.BranchWatch, error) {
	t := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO branch_watches(project_id, repo_id, branch, last_sha, last_message, last_author, updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(repo_id, branch) DO UPDATE SET last_sha=excluded.last_sha, last_message=excluded.last_message, last_author=excluded.last_author, updated_at=excluded.updated_at`,
		projectID, repoID, branch, sha, msg, author, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		_ = s.db.QueryRowContext(ctx, `SELECT id FROM branch_watches WHERE repo_id=? AND branch=?`, repoID, branch).Scan(&id)
	}
	return s.GetBranchWatch(ctx, id)
}

func (s *Store) UpdateBranchWatchTip(ctx context.Context, id int64, sha, msg, author string) (*domain.BranchWatch, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE branch_watches SET last_sha=?, last_message=?, last_author=?, updated_at=? WHERE id=?`, sha, msg, author, now(), id)
	if err != nil {
		return nil, err
	}
	return s.GetBranchWatch(ctx, id)
}

func (s *Store) DeleteBranchWatch(ctx context.Context, projectID, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM branch_watches WHERE id=? AND project_id=?`, id, projectID)
	return err
}

// ---------- Notifications ----------

func scanNotification(sc interface{ Scan(...any) error }) (domain.Notification, error) {
	var n domain.Notification
	var readInt int
	var c string
	err := sc.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.Meta, &readInt, &c)
	n.Read = readInt != 0
	n.CreatedAt = parseTime(c)
	return n, err
}

func (s *Store) CreateNotification(ctx context.Context, kind, title, body, link, meta string) (*domain.Notification, error) {
	t := now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO notifications(kind, title, body, link, meta, read, created_at) VALUES(?,?,?,?,?,0,?)`,
		kind, title, body, link, meta, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	n, err := scanNotification(s.db.QueryRowContext(ctx, `SELECT id, kind, title, body, link, meta, read, created_at FROM notifications WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) ListNotifications(ctx context.Context, limit int) ([]domain.Notification, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, title, body, link, meta, read, created_at FROM notifications ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Notification{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) MarkNotificationRead(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notifications SET read=1 WHERE id=?`, id)
	return err
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notifications SET read=1 WHERE read=0`)
	return err
}

// ---------- PR Handled Events ----------

func (s *Store) IsPREventHandled(ctx context.Context, prURL, eventKey string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pr_handled_events WHERE pr_url=? AND event_key=?`, prURL, eventKey).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) RecordPREventHandled(ctx context.Context, prURL, eventKey, actionLog string) error {
	t := now()
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO pr_handled_events(pr_url, event_key, action_log, created_at) VALUES(?,?,?,?)`,
		prURL, eventKey, actionLog, t)
	return err
}
