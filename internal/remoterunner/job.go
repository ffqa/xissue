package remoterunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/domain"
	"github.com/xpier/xissue/internal/gitx"
	"github.com/xpier/xissue/internal/remote"
)

type job struct {
	job remote.Job

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	workdir string
	usage   *acp.Usage

	clientMu sync.Mutex
	client   *acp.Client
}

func (j *job) closeClient() {
	j.clientMu.Lock()
	c := j.client
	j.client = nil
	j.clientMu.Unlock()
	if c != nil {
		c.Close()
	}
}

func (j *job) cancelClient() {
	j.clientMu.Lock()
	c := j.client
	j.clientMu.Unlock()
	if c != nil {
		c.Cancel()
		c.Close()
	}
}

func (r *Runner) getJob(runID int64) (*job, error) {
	r.jobsMu.Lock()
	jb := r.jobs[runID]
	r.jobsMu.Unlock()
	if jb == nil {
		return nil, fmt.Errorf("run %d is not known on this remote runner", runID)
	}
	return jb, nil
}

func (r *Runner) prepare(ctx context.Context, jb *job) error {
	j := &jb.job
	workdir := strings.TrimSpace(j.Workdir)
	if workdir == "" {
		workdir = filepath.Join(r.opts.DataDir, "runs", fmt.Sprintf("run-%d", j.RunID))
	}
	root := filepath.Clean(r.opts.DataDir)
	workdir = filepath.Clean(workdir)
	if workdir != root && !strings.HasPrefix(workdir, root+string(filepath.Separator)) {
		// A server must never point a remote runner outside its data directory.
		return fmt.Errorf("workdir %q is not inside %q", workdir, root)
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return err
	}
	jb.mu.Lock()
	jb.workdir = workdir
	jb.mu.Unlock()

	for _, repo := range j.Repos {
		if err := domain.ValidateRepoName(repo.Name); err != nil {
			return fmt.Errorf("invalid repository name %q: %w", repo.Name, err)
		}
		wt, err := safeJoin(workdir, repo.Name)
		if err != nil {
			return err
		}
		if _, err := os.Stat(wt); err == nil {
			continue
		}
		mirror := filepath.Join(r.opts.DataDir, "mirrors", fmt.Sprintf("repo-%d.git", repo.ID))
		// Fetch before creating a new worktree. Resume turns have an existing
		// worktree and skip this branch entirely.
		if err := gitx.EnsureMirror(ctx, repo.RemoteURL, mirror); err != nil {
			return fmt.Errorf("mirror %s: %w", repo.Name, err)
		}
		if err := gitx.AddWorktree(ctx, mirror, wt, j.Branch, repo.DefaultBranch); err != nil {
			return fmt.Errorf("worktree %s: %w", repo.Name, err)
		}
	}
	if strings.TrimSpace(j.WorkspaceAGENTS) != "" {
		_ = os.WriteFile(filepath.Join(workdir, "AGENTS.md"), []byte(j.WorkspaceAGENTS), 0o644)
	}
	return nil
}

func (r *Runner) runPrompt(ctx context.Context, jb *job) error {
	j := &jb.job
	r.sendEvent(j.RunID, remote.Event{Type: "system", Data: map[string]any{
		"text": fmt.Sprintf("Remote runner %q preparing agent %s…", r.opts.Name, j.AgentName),
	}})

	handlers := acp.Handlers{
		OnEvent: func(ev acp.Event) {
			r.sendEvent(j.RunID, remote.Event{Type: ev.Type, Data: ev.Data})
		},
		OnPermission: func(pctx context.Context, req acp.PermissionRequest) string {
			title, _ := req.ToolCall["title"].(string)
			kind, _ := req.ToolCall["kind"].(string)
			if title == "" {
				title = "Tool execution permission"
			}
			return r.askPermission(pctx, j.RunID, remote.PermissionRequest{
				Title: title, Kind: kind, ToolCall: req.ToolCall, Options: req.Options,
			})
		},
	}

	client, err := acp.Start(ctx, acp.Options{
		Command: j.Agent.Command,
		Args:    j.Agent.Args,
		Env:     j.Agent.Env,
		Dir:     jb.workdir,
	}, handlers)
	if err != nil {
		return fmt.Errorf("start agent: %w", err)
	}
	jb.clientMu.Lock()
	jb.client = client
	jb.clientMu.Unlock()

	prompt := j.Prompt
	loaded := false
	if j.SessionID != "" && client.CanLoad {
		if err := client.LoadSession(ctx, jb.workdir, j.SessionID); err != nil {
			return fmt.Errorf("session/load: %w", err)
		}
		loaded = true
		r.sendEvent(j.RunID, remote.Event{Type: "system", Data: map[string]any{
			"text": fmt.Sprintf("♻️ 已在远端恢复 ACP 会话 (%s)", j.SessionID),
		}})
	} else {
		if err := client.NewSession(ctx, jb.workdir); err != nil {
			return fmt.Errorf("session/new: %w", err)
		}
		j.SessionID = client.SessionID
	}
	if j.IsResume && !loaded {
		prompt = fmt.Sprintf("Continuing work on [%s] %s in this existing workspace.\n\nUser feedback / follow-up:\n%s", j.IssueKey, j.IssueTitle, j.Prompt)
	}
	if strings.TrimSpace(j.Model) != "" {
		if err := client.SetModel(ctx, j.Model); err != nil {
			return fmt.Errorf("set model %q: %w", j.Model, err)
		}
		r.sendEvent(j.RunID, remote.Event{Type: "system", Data: map[string]any{
			"text": fmt.Sprintf("Using model: %s (%s)", j.Model, j.AgentName),
		}})
	}
	r.sendStatus(j.RunID, "running", jb.workdir, j.SessionID, "", nil)
	res, err := client.Prompt(ctx, prompt)
	if err != nil {
		return err
	}
	jb.mu.Lock()
	jb.usage = res.Usage
	jb.mu.Unlock()
	return nil
}

func safeJoin(root, name string) (string, error) {
	base := filepath.Clean(root)
	p := filepath.Clean(filepath.Join(base, strings.TrimSpace(name)))
	if p == base || !strings.HasPrefix(p, base+string(filepath.Separator)) {
		return "", fmt.Errorf("repository %q escapes run workspace", name)
	}
	return p, nil
}

func (r *Runner) rpc(ctx context.Context, f remote.Frame) (any, error) {
	if f.Method == "cleanup" {
		var p struct {
			Workdir string `json:"workdir"`
		}
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return nil, err
		}
		return nil, r.cleanupPath(ctx, p.Workdir)
	}
	jb, err := r.getJob(f.RunID)
	if err != nil {
		return nil, err
	}
	switch f.Method {
	case "changes":
		return r.changes(ctx, jb)
	case "read_file":
		var p struct {
			RepoID int64  `json:"repoId"`
			Path   string `json:"path"`
			Side   string `json:"side"`
		}
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return nil, err
		}
		content, err := r.readFile(ctx, jb, p.RepoID, p.Path, p.Side)
		if err != nil {
			return nil, err
		}
		return map[string]string{"content": content}, nil
	case "write_file":
		var p struct {
			RepoID  int64  `json:"repoId"`
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return nil, err
		}
		return nil, r.writeFile(ctx, jb, p.RepoID, p.Path, p.Content)
	case "submit":
		var p struct {
			CommitMessage string `json:"commitMessage"`
			TargetBranch  string `json:"targetBranch"`
		}
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return nil, err
		}
		return r.submit(ctx, jb, p.CommitMessage, p.TargetBranch)
	case "discard", "delete":
		if err := r.cleanup(ctx, jb); err != nil {
			return nil, err
		}
		r.jobsMu.Lock()
		delete(r.jobs, f.RunID)
		r.jobsMu.Unlock()
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown remote RPC %q", f.Method)
	}
}

func (r *Runner) repo(jb *job, repoID int64) (domain.Repo, string, error) {
	for _, repo := range jb.job.Repos {
		if repo.ID != repoID {
			continue
		}
		jb.mu.Lock()
		workdir := jb.workdir
		jb.mu.Unlock()
		wt, err := safeJoin(workdir, repo.Name)
		if err != nil {
			return repo, "", err
		}
		return repo, wt, nil
	}
	return domain.Repo{}, "", fmt.Errorf("repository %d is not part of this run", repoID)
}

func (r *Runner) changes(ctx context.Context, jb *job) (*remote.ChangesResult, error) {
	out := &remote.ChangesResult{}
	for _, repo := range jb.job.Repos {
		jb.mu.Lock()
		workdir := jb.workdir
		jb.mu.Unlock()
		wt, err := safeJoin(workdir, repo.Name)
		if err != nil {
			return nil, err
		}
		files, err := gitx.Changes(ctx, wt, repo.DefaultBranch)
		if err != nil {
			files = nil
		}
		rr := remote.RunnerRepoChanges{RepoID: repo.ID, RepoName: repo.Name, Branch: jb.job.Branch}
		for _, f := range files {
			rr.Files = append(rr.Files, remote.RunnerFileChange{
				Path: f.Path, Status: f.Status, Additions: f.Additions, Deletions: f.Deletions,
			})
		}
		out.Changes = append(out.Changes, rr)
	}
	return out, nil
}

func (r *Runner) readFile(ctx context.Context, jb *job, repoID int64, path, side string) (string, error) {
	repo, wt, err := r.repo(jb, repoID)
	if err != nil {
		return "", err
	}
	if side == "base" {
		return gitx.ShowBase(ctx, wt, repo.DefaultBranch, path)
	}
	root, err := os.OpenRoot(wt)
	if err != nil {
		return "", err
	}
	defer root.Close()
	b, err := root.ReadFile(filepath.FromSlash(path))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func (r *Runner) writeFile(ctx context.Context, jb *job, repoID int64, path, content string) error {
	_, wt, err := r.repo(jb, repoID)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(wt)
	if err != nil {
		return err
	}
	defer root.Close()
	rel := filepath.FromSlash(path)
	if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return err
	}
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (r *Runner) submit(ctx context.Context, jb *job, commitMessage, targetBranch string) ([]remote.SubmitResult, error) {
	j := &jb.job
	var out []remote.SubmitResult
	for _, repo := range j.Repos {
		wt, err := safeJoin(jb.workdir, repo.Name)
		if err != nil {
			out = append(out, remote.SubmitResult{RepoID: repo.ID, RepoName: repo.Name, Error: err.Error()})
			continue
		}
		files, err := gitx.Changes(ctx, wt, repo.DefaultBranch)
		if err != nil {
			files = nil
		}
		ahead := hasUnpushedCommit(ctx, wt, repo.DefaultBranch)
		if len(files) == 0 && !ahead {
			continue
		}
		res := remote.SubmitResult{RepoID: repo.ID, RepoName: repo.Name, Branch: j.Branch, TargetBranch: targetBranch}
		for _, f := range files {
			res.Files = append(res.Files, remote.RunnerFileChange{Path: f.Path, Status: f.Status, Additions: f.Additions, Deletions: f.Deletions})
		}
		if len(files) > 0 {
			msg := strings.TrimSpace(commitMessage)
			if msg == "" {
				msg = fmt.Sprintf("agent: resolve %s", j.IssueKey)
			}
			sha, err := gitx.CommitAll(ctx, wt, msg)
			if err != nil {
				res.Error = err.Error()
				out = append(out, res)
				continue
			}
			res.Commit = sha
		} else {
			// A previous attempt committed successfully but failed to push.
			// Reuse that commit instead of silently dropping the work.
			res.Commit, _ = gitx.Run(ctx, wt, "rev-parse", "HEAD")
		}
		if err := gitx.Push(ctx, wt, j.Branch); err != nil {
			res.Error = err.Error()
		}
		out = append(out, res)
	}
	return out, nil
}

func (r *Runner) cleanup(ctx context.Context, jb *job) error {
	jb.closeClient()
	jb.cancel()
	jb.mu.Lock()
	workdir := jb.workdir
	jb.mu.Unlock()
	if workdir == "" {
		return nil
	}
	for _, repo := range jb.job.Repos {
		mirror := filepath.Join(r.opts.DataDir, "mirrors", fmt.Sprintf("repo-%d.git", repo.ID))
		if wt, err := safeJoin(workdir, repo.Name); err == nil {
			_ = gitx.RemoveWorktree(ctx, mirror, wt, jb.job.Branch)
		}
	}
	return os.RemoveAll(workdir)
}

func hasUnpushedCommit(ctx context.Context, dir, base string) bool {
	head, err := gitx.Run(ctx, dir, "rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(head) == "" {
		return false
	}
	base = strings.TrimSpace(base)
	baseSHA, err := gitx.Run(ctx, dir, "rev-parse", "--verify", base)
	if err != nil || strings.TrimSpace(baseSHA) == "" {
		baseSHA, err = gitx.Run(ctx, dir, "rev-parse", "--verify", "origin/"+base)
	}
	if err != nil || strings.TrimSpace(baseSHA) == "" {
		return false
	}
	return strings.TrimSpace(head) != strings.TrimSpace(baseSHA)
}

// cleanupPath removes a run workspace without requiring the in-memory job. It
// is used after a runner/server restart, when the worktree still exists but the
// job state is gone. The path is constrained to this runner's data directory.
func (r *Runner) cleanupPath(ctx context.Context, workdir string) error {
	root := filepath.Clean(r.opts.DataDir)
	wd := filepath.Clean(strings.TrimSpace(workdir))
	if wd == "" || wd == root || !strings.HasPrefix(wd, root+string(filepath.Separator)) {
		return fmt.Errorf("workdir %q is not inside %q", workdir, root)
	}
	if err := os.RemoveAll(wd); err != nil {
		return err
	}
	mirrors, _ := filepath.Glob(filepath.Join(root, "mirrors", "*.git"))
	for _, mirror := range mirrors {
		_, _ = gitx.Run(ctx, mirror, "worktree", "prune")
	}
	return nil
}
