// Package remoterunner implements the process that executes xIssue runs on a
// machine other than the server. It dials the server over WebSocket, so the
// remote host does not need an inbound port or a public address.
package remoterunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/remote"
)

// Options configure a remote runner connection.
type Options struct {
	Server  string
	Token   string
	Name    string
	DataDir string
	Version string
	Logger  *log.Logger
}

// Runner owns the WebSocket connection and all jobs running on this host.
type Runner struct {
	opts Options

	connMu sync.Mutex
	conn   *websocket.Conn

	writeMu sync.Mutex
	jobsMu  sync.Mutex
	jobs    map[int64]*job
	permMu  sync.Mutex
	perms   map[string]chan string

	nextID atomic.Int64
}

// Run connects and reconnects until ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	if strings.TrimSpace(opts.Server) == "" {
		return errors.New("remote runner: server URL is required")
	}
	if strings.TrimSpace(opts.Name) == "" {
		return errors.New("remote runner: name is required")
	}
	r := &Runner{
		opts:  opts,
		jobs:  map[int64]*job{},
		perms: map[string]chan string{},
	}
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		started := time.Now()
		err := r.connectAndServe(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Since(started) >= 10*time.Second {
			backoff = time.Second
		}
		if err != nil && opts.Logger != nil {
			opts.Logger.Printf("remote runner disconnected: %v", err)
		}
		// A dropped connection invalidates every in-flight job. Mark them
		// failed locally; the server also notices the socket closing.
		r.failAllJobs(fmt.Errorf("remote runner disconnected: %v", err))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (r *Runner) connectAndServe(ctx context.Context) error {
	wsURL, err := websocketURL(r.opts.Server)
	if err != nil {
		return err
	}
	header := http.Header{}
	header.Set("X-XIssue-Runner", r.opts.Name)
	if r.opts.Token != "" {
		header.Set("Authorization", "Bearer "+r.opts.Token)
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, _, err := websocket.Dial(dctx, wsURL, &websocket.DialOptions{HTTPHeader: header})
	cancel()
	if err != nil {
		return err
	}
	conn.SetReadLimit(remote.MaxFrameBytes)
	r.connMu.Lock()
	r.conn = conn
	r.connMu.Unlock()
	defer func() {
		r.connMu.Lock()
		if r.conn == conn {
			r.conn = nil
		}
		r.connMu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	if err := r.write(ctx, remote.Frame{
		Type: "hello", Name: r.opts.Name, Version: r.opts.Version,
	}); err != nil {
		return err
	}

	// First frame must be welcome so protocol mismatches fail early.
	readCtx, readCancel := context.WithTimeout(ctx, 20*time.Second)
	_, data, err := conn.Read(readCtx)
	readCancel()
	if err != nil {
		return err
	}
	var welcome remote.Frame
	if err := json.Unmarshal(data, &welcome); err != nil {
		return fmt.Errorf("read welcome: %w", err)
	}
	if welcome.Type == "error" {
		return errors.New(welcome.Error)
	}
	if welcome.Type != "welcome" {
		return fmt.Errorf("unexpected handshake frame %q", welcome.Type)
	}
	if r.opts.Logger != nil {
		r.opts.Logger.Printf("remote runner %q connected as %s", r.opts.Name, welcome.RunnerID)
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var f remote.Frame
		if err := json.Unmarshal(data, &f); err != nil {
			if r.opts.Logger != nil {
				r.opts.Logger.Printf("remote runner: bad frame: %v", err)
			}
			continue
		}
		r.handleFrame(ctx, f)
	}
}

func (r *Runner) handleFrame(ctx context.Context, f remote.Frame) {
	switch f.Type {
	case "job_start", "job_message":
		if f.Job == nil {
			return
		}
		go r.startJob(ctx, *f.Job)
	case "cancel":
		r.cancelJob(f.RunID)
	case "permission_result":
		r.deliverPermission(f.RequestID, f.OptionID)
	case "rpc":
		go r.handleRPC(ctx, f)
	case "pong":
	default:
	}
}

func (r *Runner) startJob(parent context.Context, j remote.Job) {
	j.Prompt = strings.TrimSpace(j.Prompt)
	if j.Prompt == "" {
		r.sendStatus(j.RunID, "failed", "", "", "empty prompt", nil)
		return
	}
	ctx, cancel := context.WithCancel(parent)
	jb := &job{job: j, ctx: ctx, cancel: cancel}
	r.jobsMu.Lock()
	if old := r.jobs[j.RunID]; old != nil {
		old.cancel()
	}
	r.jobs[j.RunID] = jb
	r.jobsMu.Unlock()
	defer func() {
		// Keep the job in the map after the turn finishes so the server can
		// still request diffs, file edits, submit, or a follow-up turn. The
		// job is removed by discard/delete or when the connection drops.
		jb.closeClient()
		cancel()
	}()

	r.sendStatus(j.RunID, "starting", j.Workdir, j.SessionID, "", nil)
	if err := r.prepare(ctx, jb); err != nil {
		r.sendStatus(j.RunID, "failed", jb.workdir, jb.job.SessionID, err.Error(), nil)
		return
	}
	if err := r.runPrompt(ctx, jb); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			r.sendStatus(j.RunID, "cancelled", jb.workdir, jb.job.SessionID, "", nil)
			return
		}
		r.sendStatus(j.RunID, "failed", jb.workdir, jb.job.SessionID, err.Error(), nil)
		return
	}
	r.sendStatus(j.RunID, "completed", jb.workdir, jb.job.SessionID, "", jb.usage)
}

func (r *Runner) cancelJob(runID int64) {
	r.jobsMu.Lock()
	jb := r.jobs[runID]
	r.jobsMu.Unlock()
	if jb == nil {
		// The server may ask to cancel a run after a runner restart. Acknowledge
		// it so the server does not leave the run in a running state forever.
		r.sendStatus(runID, "cancelled", "", "", "", nil)
		return
	}
	jb.cancelClient()
	jb.cancel()
}

func (r *Runner) failAllJobs(err error) {
	r.jobsMu.Lock()
	jobs := make([]*job, 0, len(r.jobs))
	for _, jb := range r.jobs {
		jobs = append(jobs, jb)
	}
	r.jobs = map[int64]*job{}
	r.jobsMu.Unlock()
	for _, jb := range jobs {
		jb.cancelClient()
		jb.cancel()
	}
	r.permMu.Lock()
	r.perms = map[string]chan string{}
	r.permMu.Unlock()
}

func (r *Runner) write(ctx context.Context, f remote.Frame) error {
	r.connMu.Lock()
	conn := r.conn
	r.connMu.Unlock()
	if conn == nil {
		return errors.New("not connected")
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

func (r *Runner) sendStatus(runID int64, status, workdir, sessionID, errText string, usage *acp.Usage) {
	f := remote.Frame{Type: "status", RunID: runID, Status: status, Workdir: workdir, SessionID: sessionID, Error: errText, Usage: usage}
	_ = r.write(context.Background(), f)
}

func (r *Runner) sendEvent(runID int64, ev remote.Event) {
	_ = r.write(context.Background(), remote.Frame{Type: "event", RunID: runID, Event: &ev})
}

func (r *Runner) askPermission(ctx context.Context, runID int64, req remote.PermissionRequest) string {
	reqID := fmt.Sprintf("remote-perm-%d", r.nextID.Add(1))
	ch := make(chan string, 1)
	r.permMu.Lock()
	r.perms[reqID] = ch
	r.permMu.Unlock()
	defer func() {
		r.permMu.Lock()
		delete(r.perms, reqID)
		r.permMu.Unlock()
	}()
	raw, _ := json.Marshal(req)
	if err := r.write(ctx, remote.Frame{Type: "permission", RunID: runID, RequestID: reqID, Params: raw}); err != nil {
		return ""
	}
	select {
	case <-ctx.Done():
		return ""
	case opt := <-ch:
		return opt
	}
}

func (r *Runner) deliverPermission(requestID, optionID string) {
	r.permMu.Lock()
	ch := r.perms[requestID]
	r.permMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- optionID:
	default:
	}
}

func (r *Runner) handleRPC(ctx context.Context, f remote.Frame) {
	res := remote.Frame{Type: "rpc_result", ID: f.ID, RequestID: f.RequestID, RunID: f.RunID}
	result, err := r.rpc(ctx, f)
	if err != nil {
		res.Type = "rpc_error"
		res.Error = err.Error()
	} else if result != nil {
		raw, merr := json.Marshal(result)
		if merr != nil {
			res.Type = "rpc_error"
			res.Error = merr.Error()
		} else {
			res.Result = raw
		}
	}
	if err := r.write(ctx, res); err != nil && r.opts.Logger != nil {
		r.opts.Logger.Printf("remote runner: rpc response: %v", err)
	}
}

func websocketURL(server string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported server scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/runners/ws"
	u.RawQuery = ""
	return u.String(), nil
}
