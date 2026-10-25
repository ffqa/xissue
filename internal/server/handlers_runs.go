package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/runner"
)

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := s.st.ListRuns(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	issueID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Agent  string `json:"agent"`
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	agent := strings.TrimSpace(body.Agent)
	if agent == "" {
		agent = s.runner.AgentForRoleOrDefault(runner.AgentRoleIssueDevelop)
	}
	if agent == "" {
		writeErr(w, http.StatusBadRequest, errors.New("no agent configured; assign one in global settings"))
		return
	}
	run, err := s.runner.StartRun(r.Context(), issueID, agent, body.Model, body.Prompt)
	if err != nil {
		if errors.Is(err, runner.ErrQueueFull) {
			writeErr(w, http.StatusTooManyRequests, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	run, err := s.st.GetRun(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	var list []domain.RunEvent
	var err error
	if after == 0 {
		// Opening a historical run must not pull thousands of rows into the
		// browser. Live runs still append through the WS and use after=seq.
		list, err = s.st.ListEventsTail(r.Context(), id, 500)
	} else {
		list, err = s.st.ListEvents(r.Context(), id, after)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.runner.SendMessage(r.Context(), id, body.Text); err != nil {
		if errors.Is(err, runner.ErrQueueFull) {
			writeErr(w, http.StatusTooManyRequests, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePermission(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	reqID := r.PathValue("requestId")
	var body struct {
		OptionID string `json:"optionId"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.runner.RespondPermission(id, reqID, body.OptionID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.runner.Cancel(id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunChanges(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ch, err := s.runner.Changes(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) handleGetRunArchive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	dir, err := s.runner.ArchiveRun(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	analysisBytes, _ := os.ReadFile(filepath.Join(dir, "analysis.md"))
	eventsBytes, _ := os.ReadFile(filepath.Join(dir, "events.log"))
	writeJSON(w, http.StatusOK, map[string]any{
		"dir":      dir,
		"analysis": string(analysisBytes),
		"events":   string(eventsBytes),
	})
}

func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	repoID, _ := strconv.ParseInt(r.URL.Query().Get("repoId"), 10, 64)
	path := r.URL.Query().Get("path")
	side := r.URL.Query().Get("side")
	content, err := s.runner.ReadFile(r.Context(), id, repoID, path, side)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": content})
}

func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	repoID, _ := strconv.ParseInt(r.URL.Query().Get("repoId"), 10, 64)
	path := r.URL.Query().Get("path")
	var body struct {
		Content string `json:"content"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.runner.WriteFile(r.Context(), id, repoID, path, body.Content); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSubmitRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req runner.SubmitRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.runner.Submit(r.Context(), id, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if res == nil {
		res = []runner.SubmitResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": res})
}

func (s *Server) handleDiscardRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.runner.Discard(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.runner.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
