package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/xpier/xissue/internal/store"
)

func pathInt(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("invalid "+key))
		return 0, false
	}
	return id, true
}

func writeTaskErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeErr(w, http.StatusBadRequest, err)
}

func (s *Server) handleListProjectTodos(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	items, err := s.st.ListProjectTodos(r.Context(), id, r.URL.Query().Get("q"), r.URL.Query().Get("status"))
	if err != nil {
		writeTaskErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleCreateProjectTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Title         string `json:"title"`
		Detail        string `json:"detail"`
		SourceIssueID *int64 `json:"sourceIssueId"`
		SourceKey     string `json:"sourceKey"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	item, err := s.st.CreateProjectTodo(r.Context(), id, body.Title, body.Detail, body.SourceIssueID, body.SourceKey)
	if err != nil {
		writeTaskErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateProjectTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	itemID, ok := pathInt(w, r, "todoId")
	if !ok {
		return
	}
	var patch store.ProjectTodoPatch
	if err := readJSON(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	item, err := s.st.UpdateProjectTodo(r.Context(), id, itemID, patch)
	if err != nil {
		writeTaskErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeleteProjectTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	itemID, ok := pathInt(w, r, "todoId")
	if !ok {
		return
	}
	if err := s.st.DeleteProjectTodo(r.Context(), id, itemID); err != nil {
		writeTaskErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListProjectTestPlans(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	items, err := s.st.ListProjectTestPlans(r.Context(), id, r.URL.Query().Get("q"), r.URL.Query().Get("status"))
	if err != nil {
		writeTaskErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleCreateProjectTestPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Title         string `json:"title"`
		Detail        string `json:"detail"`
		SourceIssueID *int64 `json:"sourceIssueId"`
		SourceKey     string `json:"sourceKey"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	item, err := s.st.CreateProjectTestPlan(r.Context(), id, body.Title, body.Detail, body.SourceIssueID, body.SourceKey)
	if err != nil {
		writeTaskErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleUpdateProjectTestPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	itemID, ok := pathInt(w, r, "planId")
	if !ok {
		return
	}
	var patch store.ProjectTestPlanPatch
	if err := readJSON(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	item, err := s.st.UpdateProjectTestPlan(r.Context(), id, itemID, patch)
	if err != nil {
		writeTaskErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeleteProjectTestPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	itemID, ok := pathInt(w, r, "planId")
	if !ok {
		return
	}
	if err := s.st.DeleteProjectTestPlan(r.Context(), id, itemID); err != nil {
		writeTaskErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
