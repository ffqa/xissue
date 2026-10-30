package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/runner"
)

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.st.ListNotifications(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.st.MarkNotificationRead(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if err := s.st.MarkAllNotificationsRead(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListWatches(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := s.st.ListBranchWatches(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAddWatch(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		RepoID int64  `json:"repoId"`
		Branch string `json:"branch"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	bw, err := s.runner.AddBranchWatch(r.Context(), pid, body.RepoID, body.Branch)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, bw)
}

func (s *Server) handleDeleteWatch(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	wid, ok := pathID(w, r, "watchId")
	if !ok {
		return
	}
	if err := s.st.DeleteBranchWatch(r.Context(), pid, wid); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCheckWatches(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	watchesTriggered, prsUpdated, err := s.runner.CheckWatchesAndPRs(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"watchesTriggered": watchesTriggered,
		"prsUpdated":       prsUpdated,
	})
}

func (s *Server) handleRepoBranches(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r, "repoId")
	if !ok {
		return
	}
	branches, err := s.runner.ListRepoBranches(r.Context(), repoID, r.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if branches == nil {
		branches = []string{}
	}
	writeJSON(w, http.StatusOK, branches)
}

func (s *Server) handleRepoCommits(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r, "repoId")
	if !ok {
		return
	}
	branch := r.URL.Query().Get("branch")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	refresh := r.URL.Query().Get("refresh") == "1"
	commits, err := s.runner.ListRepoCommits(r.Context(), repoID, branch, limit, refresh)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if commits == nil {
		commits = []gitx.CommitInfo{}
	}
	writeJSON(w, http.StatusOK, commits)
}

func (s *Server) handleRepoCommitDetail(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r, "repoId")
	if !ok {
		return
	}
	sha := strings.TrimSpace(r.PathValue("sha"))
	if sha == "" {
		writeErr(w, http.StatusBadRequest, errors.New("commit sha is required"))
		return
	}
	detail, err := s.runner.GetRepoCommitDetail(r.Context(), repoID, sha)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCherryPick(w http.ResponseWriter, r *http.Request) {
	pid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req runner.CherryPickRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req.ProjectID = pid
	res, err := s.runner.CherryPick(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
