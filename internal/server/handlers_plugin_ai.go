package server

import (
	"net/http"
	"strings"
)

type pluginAIRequest struct {
	Prompt string `json:"prompt"`
	Agent  string `json:"agent,omitempty"`
	Model  string `json:"model,omitempty"`
}

func (s *Server) handlePluginAIInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.runner.PluginAIDefault())
}

func (s *Server) handlePluginAI(w http.ResponseWriter, r *http.Request) {
	var body pluginAIRequest
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.runner.PluginAI(r.Context(), body.Prompt, body.Agent, body.Model)
	if err != nil {
		status := http.StatusBadGateway
		if strings.TrimSpace(body.Prompt) == "" {
			status = http.StatusBadRequest
		}
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
