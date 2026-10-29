package server

import (
	"errors"
	"net/http"
)

func (s *Server) handleReorderBacklog(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(body.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("ids is required"))
		return
	}
	if err := s.st.ReorderBacklog(r.Context(), projectID, body.IDs); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
