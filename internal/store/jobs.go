package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/xpier/xissue/internal/domain"
)

func (s *Store) CreateJob(ctx context.Context, kind string, repoID int64, payload any) (*domain.Job, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO jobs(kind, status, repo_id, payload, created_at) VALUES(?,?,?,?,?)`,
		kind, domain.JobQueued, repoID, string(raw), now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetJob(ctx, id)
}

func (s *Store) GetJob(ctx context.Context, id int64) (*domain.Job, error) {
	var job domain.Job
	var payload, created string
	var started, finished *string
	err := s.db.QueryRowContext(ctx, `SELECT id, kind, status, repo_id, payload, error, created_at, started_at, finished_at FROM jobs WHERE id=?`, id).
		Scan(&job.ID, &job.Kind, &job.Status, &job.RepoID, &payload, &job.Error, &created, &started, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	job.Payload = json.RawMessage(payload)
	job.CreatedAt = parseTime(created)
	if started != nil {
		t := parseTime(*started)
		job.StartedAt = &t
	}
	if finished != nil {
		t := parseTime(*finished)
		job.FinishedAt = &t
	}
	return &job, nil
}

func (s *Store) ListJobs(ctx context.Context, limit int) ([]domain.Job, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, status, repo_id, payload, error, created_at, started_at, finished_at FROM jobs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Job{}
	for rows.Next() {
		var job domain.Job
		var payload, created string
		var started, finished *string
		if err := rows.Scan(&job.ID, &job.Kind, &job.Status, &job.RepoID, &payload, &job.Error, &created, &started, &finished); err != nil {
			return nil, err
		}
		job.Payload = json.RawMessage(payload)
		job.CreatedAt = parseTime(created)
		if started != nil {
			t := parseTime(*started)
			job.StartedAt = &t
		}
		if finished != nil {
			t := parseTime(*finished)
			job.FinishedAt = &t
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// ClaimNextJob atomically moves the oldest queued job to running. The server
// has one worker, but the compare-and-set keeps retries/reconnects safe.
func (s *Store) ClaimNextJob(ctx context.Context) (*domain.Job, error) {
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM jobs WHERE status=? ORDER BY id LIMIT 1`, domain.JobQueued).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, started_at=?, error='' WHERE id=? AND status=?`,
		domain.JobRunning, now(), id, domain.JobQueued)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.GetJob(ctx, id)
}

func (s *Store) FinishJob(ctx context.Context, id int64, status domain.JobStatus, jobErr string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, error=?, finished_at=? WHERE id=?`, status, jobErr, now(), id)
	return err
}

func (s *Store) RequeueRunningJobs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, started_at=NULL, error=? WHERE status=?`,
		domain.JobQueued, "server restarted while job was running", domain.JobRunning)
	return err
}

func (s *Store) RetryJob(ctx context.Context, id int64) (*domain.Job, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, error='', started_at=NULL, finished_at=NULL WHERE id=? AND status IN (?,?)`,
		domain.JobQueued, id, domain.JobFailed, domain.JobCancelled)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, errors.New("only failed or cancelled jobs can be retried")
	}
	return s.GetJob(ctx, id)
}

func (s *Store) CancelJob(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, finished_at=? WHERE id=? AND status=?`,
		domain.JobCancelled, now(), id, domain.JobQueued)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("only queued jobs can be cancelled")
	}
	return nil
}

func (s *Store) PruneJobs(ctx context.Context, keep int) error {
	if keep < 10 {
		keep = 50
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE status IN (?,?,?) AND id NOT IN (
		SELECT id FROM jobs WHERE status IN (?,?,?) ORDER BY id DESC LIMIT ?
	)`, domain.JobCompleted, domain.JobFailed, domain.JobCancelled, domain.JobCompleted, domain.JobFailed, domain.JobCancelled, keep)
	return err
}
