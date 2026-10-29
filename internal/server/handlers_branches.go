package server

import (
	"errors"
	"net/http"

	"github.com/xpier/xissue/internal/gitx"
)

func (s *Server) handleListMergedBranches(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r, "repoId")
	if !ok {
		return
	}
	branches, err := s.runner.ListRepoMergedBranches(r.Context(), repoID, r.URL.Query().Get("refresh") != "0")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if branches == nil {
		branches = []gitx.MergedBranch{}
	}
	writeJSON(w, http.StatusOK, branches)
}

func (s *Server) handleDeleteMergedBranches(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r, "repoId")
	if !ok {
		return
	}
	var body struct {
		Branches []string `json:"branches"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(body.Branches) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("branches is required"))
		return
	}
	jobs, err := s.enqueueBranchDeletes(r.Context(), repoID, body.Branches)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "queued": len(jobs)})
}
