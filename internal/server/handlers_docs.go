package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/knowledge"
)

func (s *Server) handleListProjectDocs(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	docs, err := knowledge.ScanProjectDocs(r.Context(), s.cfg.DataDir, p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if docs == nil {
		docs = []domain.DocItem{}
	}
	writeJSON(w, http.StatusOK, docs)
}

func (s *Server) handleListProjectReadmes(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	readmes, err := knowledge.ProjectReadmes(r.Context(), s.cfg.DataDir, p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if readmes == nil {
		readmes = []domain.ProjectReadme{}
	}
	writeJSON(w, http.StatusOK, readmes)
}

func (s *Server) handleGetProjectDocDetail(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	docID := r.URL.Query().Get("docId")
	if docID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("缺少 docId 参数"))
		return
	}
	p, err := s.st.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	detail, err := knowledge.GetDocDetail(r.Context(), s.cfg.DataDir, p, docID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleSyncProjectWiki(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	var req struct {
		RepoID int64 `json:"repoId"`
	}
	_ = readJSON(r, &req)

	synced := 0
	var syncErrors []string

	for _, repo := range p.Repos {
		if req.RepoID > 0 && repo.ID != req.RepoID {
			continue
		}
		if repo.Provider != domain.ProviderGitHub || repo.OwnerRepo == "" {
			continue
		}
		if err := knowledge.SyncGitHubWiki(r.Context(), s.cfg.DataDir, repo); err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", repo.Name, err))
		} else {
			synced++
		}
	}

	msg := fmt.Sprintf("已完成同步 %d 个仓库的 GitHub Wiki", synced)
	if len(syncErrors) > 0 {
		msg += fmt.Sprintf(" (部分提示: %s)", strings.Join(syncErrors, "; "))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"syncedCount": synced,
		"syncErrors":  syncErrors,
		"message":     msg,
	})
}

func (s *Server) handleSearchProjectDocs(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	results, err := knowledge.SearchDocs(r.Context(), s.cfg.DataDir, p, req.Query, req.Limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if results == nil {
		results = []domain.DocSearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleSaveProjectDoc(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		DocID   string `json:"docId"`
		Content string `json:"content"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if !knowledge.IsWritableDoc(req.DocID) {
		writeErr(w, http.StatusBadRequest, errors.New("GitHub Wiki 与镜像文档为只读，请在原始仓库中修改"))
		return
	}
	if err := knowledge.SaveDoc(r.Context(), s.cfg.DataDir, p, req.DocID, req.Content); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "文档已成功保存",
	})
}
