package server

import (
	"net/http"

	"github.com/xpier/xissue/internal/config"
	"github.com/xpier/xissue/internal/gitx"
)

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.runner.ListAgents())
}

func (s *Server) handleReloadAgents(w http.ResponseWriter, r *http.Request) {
	list, err := s.runner.ReloadAgents()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleUpsertAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body config.AgentConfig
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	st, err := s.runner.UpsertAgent(name, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.runner.DeleteAgent(name); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentModels(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	refresh := r.URL.Query().Get("refresh") == "1"
	res, err := s.runner.ListAgentModels(r.Context(), name, refresh)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleBrowseDir(w http.ResponseWriter, r *http.Request) {
	res, err := gitx.BrowseDir(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleInspectDir(w http.ResponseWriter, r *http.Request) {
	res, err := gitx.InspectDir(r.Context(), r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
