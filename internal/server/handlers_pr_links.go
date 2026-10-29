package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) handleListIssuePRLinks(w http.ResponseWriter, r *http.Request) {
	issueID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	list, err := s.st.ListIssuePRLinks(r.Context(), issueID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAddIssuePRLink(w http.ResponseWriter, r *http.Request) {
	issueID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	raw := strings.TrimSpace(body.URL)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeErr(w, http.StatusBadRequest, errors.New("PR URL must be http(s)"))
		return
	}
	link, err := s.st.AddIssuePRLink(r.Context(), issueID, raw, strings.TrimSpace(body.Title))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	_ = s.st.AddIssueEvent(r.Context(), issueID, "pr_link", "me", "关联 PR "+raw)
	writeJSON(w, http.StatusCreated, link)
}

func (s *Server) handleDeleteIssuePRLink(w http.ResponseWriter, r *http.Request) {
	issueID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	linkID, ok := pathID(w, r, "linkID")
	if !ok {
		return
	}
	if err := s.st.DeleteIssuePRLink(r.Context(), issueID, linkID); err != nil {
		writeStoreErr(w, err)
		return
	}
	_ = s.st.AddIssueEvent(r.Context(), issueID, "pr_link", "me", "移除关联 PR")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
