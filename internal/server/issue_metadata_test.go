package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xpier/xissue/internal/store"

	"github.com/xpier/xissue/internal/domain"
)

func doJSON(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Host = "127.0.0.1:7789"
	req.Header.Set("X-XIssue", "1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func TestIssuePriorityLabelsAndEvents(t *testing.T) {
	srv, st, _ := newTestServer(t)
	proj, _ := st.CreateProject(t.Context(), "Meta", "")
	w := doJSON(t, srv, http.MethodPost, "/api/projects/"+itoa(proj.ID)+"/issues", map[string]any{
		"title": "urgent issue", "body": "body", "labels": []string{"bug", "frontend"}, "priority": "urgent",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var issue domain.Issue
	if err := json.Unmarshal(w.Body.Bytes(), &issue); err != nil {
		t.Fatal(err)
	}
	if issue.Priority != domain.IssuePriorityUrgent || len(issue.Labels) != 2 {
		t.Fatalf("issue = %+v", issue)
	}
	events, err := st.ListIssueEvents(t.Context(), issue.ID)
	if err != nil || len(events) != 1 || events[0].Kind != "created" {
		t.Fatalf("events = %+v, %v", events, err)
	}

	w = doJSON(t, srv, http.MethodPatch, "/api/issues/"+itoa(issue.ID), map[string]any{
		"status": "review", "priority": "high", "labels": []string{"bug"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	events, _ = st.ListIssueEvents(t.Context(), issue.ID)
	kinds := map[string]bool{}
	for _, e := range events {
		kinds[e.Kind] = true
	}
	if !kinds["status"] || !kinds["priority"] || !kinds["labels"] {
		t.Fatalf("missing events: %+v", events)
	}
}

func TestAttachmentUploadListServeDelete(t *testing.T) {
	srv, st, dir := newTestServer(t)
	proj, _ := st.CreateProject(t.Context(), "Files", "")
	issue, err := st.CreateLocalIssue(t.Context(), proj.ID, "with image", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "shot.png")
	_, _ = fw.Write([]byte("\x89PNG\r\n\x1a\nfake"))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/issues/"+itoa(issue.ID)+"/attachments", &buf)
	req.Host = "127.0.0.1:7789"
	req.Header.Set("X-XIssue", "1")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	var att domain.Attachment
	if err := json.Unmarshal(w.Body.Bytes(), &att); err != nil {
		t.Fatal(err)
	}
	if att.Name != "shot.png" || att.Size == 0 || att.URL == "" {
		t.Fatalf("attachment = %+v", att)
	}
	files, _ := os.ReadDir(filepath.Join(dir, "attachments", itoa(issue.ID)))
	if len(files) != 1 {
		t.Fatalf("stored files = %+v", files)
	}

	req = httptest.NewRequest(http.MethodGet, att.URL, nil)
	req.Host = "127.0.0.1:7789"
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "PNG") {
		t.Fatalf("serve: %d %q", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, att.URL, nil)
	req.Host = "127.0.0.1:7789"
	req.Header.Set("X-XIssue", "1")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	list, _ := st.ListAttachments(t.Context(), issue.ID)
	if len(list) != 0 {
		t.Fatalf("attachments remain: %+v", list)
	}
}

func TestCreateIssueSyncsRemoteAndBindsNumber(t *testing.T) {
	srv, st, _ := newTestServer(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repos/acme/demo/issues":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number":7,"title":"remote issue","body":"body","html_url":"https://gitea.test/acme/demo/issues/7","state":"open"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	t.Setenv("XISSUE_GITEA_URL", upstream.URL)
	t.Setenv("XISSUE_GITEA_TOKEN", "test-token")
	t.Setenv("GITEA_URL", "")
	t.Setenv("GITEA_TOKEN", "")

	proj, _ := st.CreateProject(t.Context(), "Remote", "")
	repo, err := st.CreateRepo(t.Context(), domain.Repo{
		ProjectID: proj.ID, Name: "demo", Provider: domain.ProviderGitea,
		OwnerRepo: "acme/demo", RemoteURL: "https://gitea.test/acme/demo.git", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	w := doJSON(t, srv, http.MethodPost, "/api/projects/"+itoa(proj.ID)+"/issues", map[string]any{
		"title": "remote issue", "body": "body", "repoId": repo.ID, "syncRemote": true,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var issue domain.Issue
	if err := json.Unmarshal(w.Body.Bytes(), &issue); err != nil {
		t.Fatal(err)
	}
	if issue.Source != "remote" || issue.Key != "acme/demo#7" || issue.RemoteNumber == nil || *issue.RemoteNumber != 7 {
		t.Fatalf("issue = %+v", issue)
	}
	if issue.RemoteURL != "https://gitea.test/acme/demo/issues/7" {
		t.Fatalf("remote url = %q", issue.RemoteURL)
	}
	events, _ := st.ListIssueEvents(t.Context(), issue.ID)
	found := false
	for _, e := range events {
		if e.Kind == "remote_sync" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing remote_sync event: %+v", events)
	}
	// Re-importing the same remote issue must update the bound row, not duplicate it.
	if _, err := st.UpsertRemoteIssue(t.Context(), domain.Issue{
		ProjectID: proj.ID, Key: "acme/demo#7", RepoID: &repo.ID, RemoteNumber: issue.RemoteNumber,
		RemoteURL: issue.RemoteURL, Title: "remote issue updated", Body: "updated", Labels: []string{"bug"},
	}); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListIssues(t.Context(), proj.ID)
	if len(list) != 1 || list[0].Title != "remote issue updated" {
		t.Fatalf("duplicate or stale issue: %+v", list)
	}
}

func TestSubIssueCreateAndList(t *testing.T) {
	srv, st, _ := newTestServer(t)
	proj, _ := st.CreateProject(t.Context(), "Sub", "")
	parent, err := st.CreateLocalIssue(t.Context(), proj.ID, "parent", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := doJSON(t, srv, http.MethodPost, "/api/projects/"+itoa(proj.ID)+"/issues", map[string]any{
		"title": "child", "body": "child body", "parentId": parent.ID, "priority": "high",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create child: %d %s", w.Code, w.Body.String())
	}
	var child domain.Issue
	if err := json.Unmarshal(w.Body.Bytes(), &child); err != nil {
		t.Fatal(err)
	}
	if child.ParentIssueID == nil || *child.ParentIssueID != parent.ID {
		t.Fatalf("parent id = %+v", child.ParentIssueID)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/issues/"+itoa(parent.ID)+"/subissues", nil)
	req.Host = "127.0.0.1:7789"
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var children []domain.Issue
	if err := json.Unmarshal(w.Body.Bytes(), &children); err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].ID != child.ID {
		t.Fatalf("children = %+v", children)
	}
	events, _ := st.ListIssueEvents(t.Context(), parent.ID)
	found := false
	for _, e := range events {
		if e.Kind == "subissue" && strings.Contains(e.Message, child.Key) {
			found = true
		}
	}
	if !found {
		t.Fatalf("parent timeline missing subissue event: %+v", events)
	}
}

func TestSchedulesEndpointListsLoops(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/schedules", nil)
	req.Host = "127.0.0.1:7789"
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("schedules: %d %s", w.Code, w.Body.String())
	}
	var tasks []ScheduledTask
	if err := json.Unmarshal(w.Body.Bytes(), &tasks); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, task := range tasks {
		ids[task.ID] = true
	}
	for _, want := range []string{"watcher", "issue-sync", "auto-update", "retention", "log-prune"} {
		if !ids[want] {
			t.Fatalf("missing schedule %q: %+v", want, tasks)
		}
	}
}

func TestIssuePRLinkManualAssociation(t *testing.T) {
	srv, st, _ := newTestServer(t)
	proj, _ := st.CreateProject(t.Context(), "Links", "")
	issue, _ := st.CreateLocalIssue(t.Context(), proj.ID, "linked", "", nil)
	w := doJSON(t, srv, http.MethodPost, "/api/issues/"+itoa(issue.ID)+"/pr-links", map[string]any{
		"url": "https://github.com/acme/demo/pull/7", "title": "fix it",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("add link: %d %s", w.Code, w.Body.String())
	}
	var link domain.IssuePRLink
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListIssuePRLinks(t.Context(), issue.ID)
	if len(list) != 1 || list[0].URL != link.URL {
		t.Fatalf("links = %+v", list)
	}
	w = doJSON(t, srv, http.MethodDelete, "/api/issues/"+itoa(issue.ID)+"/pr-links/"+itoa(link.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete link: %d %s", w.Code, w.Body.String())
	}
	list, _ = st.ListIssuePRLinks(t.Context(), issue.ID)
	if len(list) != 0 {
		t.Fatalf("links remain: %+v", list)
	}
}

func TestBacklogReorder(t *testing.T) {
	srv, st, _ := newTestServer(t)
	proj, _ := st.CreateProject(t.Context(), "Backlog", "")
	a, _ := st.CreateLocalIssue(t.Context(), proj.ID, "a", "", nil)
	b, _ := st.CreateLocalIssue(t.Context(), proj.ID, "b", "", nil)
	c, _ := st.CreateLocalIssue(t.Context(), proj.ID, "c", "", nil)
	backlog := domain.IssueBacklog
	for _, issue := range []*domain.Issue{a, b, c} {
		if _, err := st.UpdateIssue(t.Context(), issue.ID, store.IssuePatch{Status: &backlog}); err != nil {
			t.Fatal(err)
		}
	}
	w := doJSON(t, srv, http.MethodPost, "/api/projects/"+itoa(proj.ID)+"/backlog/reorder", map[string]any{
		"ids": []int64{c.ID, a.ID, b.ID},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("reorder: %d %s", w.Code, w.Body.String())
	}
	ranks := map[int64]int64{}
	for _, issue := range []*domain.Issue{c, a, b} {
		got, _ := st.GetIssue(t.Context(), issue.ID)
		ranks[got.ID] = got.BacklogRank
	}
	if ranks[c.ID] != 0 || ranks[a.ID] != 1 || ranks[b.ID] != 2 {
		t.Fatalf("ranks = %+v", ranks)
	}
}

func TestBranchDeleteQueueRunsInWorker(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "init", "-q", "-b", "main")
	mustGit(t, work, "config", "user.name", "Queue User")
	mustGit(t, work, "config", "user.email", "queue@example.com")
	mustGit(t, work, "commit", "-q", "--allow-empty", "-m", "init")
	mustGit(t, work, "checkout", "-q", "-b", "cleanup")
	mustGit(t, work, "commit", "-q", "--allow-empty", "-m", "cleanup")
	mustGit(t, work, "checkout", "-q", "main")
	mustGit(t, work, "merge", "-q", "--no-ff", "-m", "merge cleanup", "cleanup")

	srv, st, _ := newTestServer(t)
	proj, _ := st.CreateProject(t.Context(), "Queue", "")
	repo, err := st.CreateRepo(t.Context(), domain.Repo{
		ProjectID: proj.ID, Name: "demo", Provider: domain.ProviderPlain,
		RemoteURL: work, DefaultBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	w := doJSON(t, srv, http.MethodPost, "/api/repos/"+itoa(repo.ID)+"/branches/delete", map[string]any{"branches": []string{"cleanup"}})
	if w.Code != http.StatusOK {
		t.Fatalf("enqueue: %d %s", w.Code, w.Body.String())
	}
	var enqueued struct {
		Queued int `json:"queued"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &enqueued)
	if enqueued.Queued != 1 {
		t.Fatalf("queued = %d", enqueued.Queued)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		jobs, _ := st.ListJobs(t.Context(), 10)
		if len(jobs) == 1 && jobs[0].Status == domain.JobCompleted {
			out, _ := exec.Command("git", "-C", work, "branch", "--list", "cleanup").Output()
			if strings.TrimSpace(string(out)) != "" {
				t.Fatalf("branch still exists: %q", out)
			}
			return
		}
		if len(jobs) == 1 && jobs[0].Status == domain.JobFailed {
			t.Fatalf("job failed: %+v", jobs[0])
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timeout waiting for queued branch delete")
}
