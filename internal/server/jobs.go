package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/store"
)

type branchDeletePayload struct {
	Branch string `json:"branch"`
}

func (s *Server) jobLoop() {
	// A previous process may have died mid-job; make those jobs runnable again.
	_ = s.st.RequeueRunningJobs(s.jobCtx)
	for {
		select {
		case <-s.jobCtx.Done():
			return
		default:
		}
		job, err := s.st.ClaimNextJob(s.jobCtx)
		if errors.Is(err, store.ErrNotFound) {
			select {
			case <-s.jobCtx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		if err != nil {
			select {
			case <-s.jobCtx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}
		s.runJob(job)
	}
}

func (s *Server) runJob(job *domain.Job) {
	ctx, cancel := context.WithTimeout(s.jobCtx, 2*time.Minute)
	defer cancel()
	var jobErr error
	switch job.Kind {
	case "branch_delete":
		var p branchDeletePayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			jobErr = err
			break
		}
		_, jobErr = s.runner.DeleteRepoMergedBranches(ctx, job.RepoID, []string{p.Branch})
	default:
		jobErr = fmt.Errorf("unknown job kind %q", job.Kind)
	}
	status := domain.JobCompleted
	msg := ""
	if jobErr != nil {
		status = domain.JobFailed
		msg = jobErr.Error()
	}
	_ = s.st.FinishJob(context.Background(), job.ID, status, msg)
	_ = s.st.PruneJobs(context.Background(), 50)
}

func (s *Server) enqueueBranchDeletes(ctx context.Context, repoID int64, branches []string) ([]domain.Job, error) {
	seen := map[string]bool{}
	jobs := make([]domain.Job, 0, len(branches))
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if branch == "" || seen[branch] {
			continue
		}
		if err := gitx.ValidateBranchName(branch); err != nil {
			return nil, err
		}
		seen[branch] = true
		job, err := s.st.CreateJob(ctx, "branch_delete", repoID, branchDeletePayload{Branch: branch})
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	if len(jobs) == 0 {
		return nil, errors.New("no branches selected")
	}
	return jobs, nil
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	jobs, err := s.st.ListJobs(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	job, err := s.st.RetryJob(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.st.CancelJob(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
