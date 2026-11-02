package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/provider"
	"github.com/xpier/xissue/internal/runner"
)

func (s *Server) handleDetectProjectHooks(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	hooks := s.runner.DetectProjectHooks(r.Context(), id)
	writeJSON(w, http.StatusOK, hooks)
}

func (s *Server) handleListProjectPRs(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	s.repairReposBeforePRList(r.Context(), id)
	prs, err := s.runner.ListProjectPRs(r.Context(), id, state)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if prs == nil {
		prs = []provider.RemotePR{}
	}
	writeJSON(w, http.StatusOK, prs)
}

func (s *Server) handleListProjectPRPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	s.repairReposBeforePRList(r.Context(), id)
	page, err := s.runner.ListProjectPRPage(r.Context(), id, r.URL.Query().Get("state"), r.URL.Query().Get("cursor"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// repairReposBeforePRList refreshes repos that look like local directories or
// plain remotes. Detection may upgrade them to their git origin, or downgrade a
// stale platform provider that would otherwise fetch another repo's PRs.
func (s *Server) repairReposBeforePRList(ctx context.Context, id int64) {
	proj, err := s.st.GetProject(ctx, id)
	if err != nil {
		return
	}
	needsDetect := false
	for _, repo := range proj.Repos {
		if repo.Provider == domain.ProviderPlain || gitx.IsLocalDir(repo.RemoteURL) {
			needsDetect = true
			break
		}
	}
	if !needsDetect {
		return
	}
	dctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	s.refreshProjectRepos(dctx, proj)
	s.runner.InvalidateProjectPRCache(id)
}

func (s *Server) handleMergeProjectPR(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	numStr := r.PathValue("number")
	number, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid PR number: %v", numStr))
		return
	}
	var body struct {
		RepoID int64  `json:"repoId"`
		Method string `json:"method"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	repo, err := s.st.GetRepo(r.Context(), body.RepoID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if repo.ProjectID != projectID {
		writeErr(w, http.StatusBadRequest, errors.New("repository does not belong to this project"))
		return
	}
	msg, err := s.runner.MergeProjectPR(r.Context(), projectID, body.RepoID, number, body.Method)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "merged": true, "message": msg})
}

func (s *Server) handleCloseProjectPR(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	numStr := r.PathValue("number")
	number, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid PR number: %v", numStr))
		return
	}
	var body struct {
		RepoID int64 `json:"repoId"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	repo, err := s.st.GetRepo(r.Context(), body.RepoID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if repo.ProjectID != projectID {
		writeErr(w, http.StatusBadRequest, errors.New("repository does not belong to this project"))
		return
	}
	if err := s.runner.CloseProjectPR(r.Context(), projectID, body.RepoID, number); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "closed": true})
}

func (s *Server) handlePullProjectStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	status, err := s.runner.PullProjectStatus(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handlePullProjectRepos(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	results, err := s.runner.PullProjectRepos(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	pulled := make([]string, 0, len(results))
	failed := false
	for _, res := range results {
		if res.LocalUpdated {
			pulled = append(pulled, res.RepoName)
		}
		if res.Error != "" {
			failed = true
		}
	}
	message := "没有可更新的本地仓库（当前分支不是目标分支、工作区不干净或未启用拉取）"
	if len(pulled) > 0 {
		message = fmt.Sprintf("已更新 %d 个本地仓库", len(pulled))
	}
	if failed {
		message = "部分仓库拉取失败"
	}
	body := map[string]any{"ok": !failed && len(pulled) > 0, "pulledRepos": pulled, "repos": results, "message": message}
	if failed {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": message + "：" + runner.FormatPullResults(results), "ok": false, "pulledRepos": pulled, "repos": results})
		return
	}
	writeJSON(w, http.StatusOK, body)
}
