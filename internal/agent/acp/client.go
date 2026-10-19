// Package acp implements a minimal Agent Client Protocol (ACP) client.
//
// ACP is JSON-RPC 2.0 over newline-delimited stdio. xIssue acts as the client
// ("editor") and spawns the agent CLI (e.g. `opencode acp`, `omp acp`, codex-acp).
// Spec: https://agentclientprotocol.com
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

const ProtocolVersion = 1

// Event is a normalized session update forwarded to the runner.
type Event struct {
	Type string         // message | thought | tool_call | tool_call_update | plan | stderr
	Data map[string]any // payload, see docs/api.md
}

// PermissionOption mirrors ACP's PermissionOption.
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // allow_once | allow_always | reject_once | reject_always
}

// PermissionRequest is sent by the agent before running a sensitive tool call.
type PermissionRequest struct {
	ToolCall map[string]any     `json:"toolCall"`
	Options  []PermissionOption `json:"options"`
}

// Handlers are callbacks invoked by the client. OnPermission blocks until decided;
// return "" to signal cancellation.
type Handlers struct {
	OnEvent      func(Event)
	OnPermission func(ctx context.Context, req PermissionRequest) (optionID string)
}

// Options configure how the agent process is started.
type Options struct {
	Command string
	Args    []string
	Env     map[string]string
	Dir     string
	// Wrap optionally prefixes the command (e.g. systemd-run ... --).
	Wrap []string
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("acp error %d: %s", e.Code, e.Message) }

// ModelOption describes one selectable model exposed by the agent via ACP configOptions.
type ModelOption struct {
	Value    string `json:"value"`    // e.g. "google/gemini-2.5-flash"
	Name     string `json:"name"`     // display name
	Provider string `json:"provider"` // prefix before '/'
}

// Client is one running agent process with (at most) one active session.
type Client struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	writeMu  sync.Mutex
	nextID   atomic.Int64
	pendMu   sync.Mutex
	pending  map[int64]chan rpcMsg
	hMu      sync.RWMutex
	h        Handlers
	done     chan struct{}
	exitErr  error
	ctx      context.Context
	cancel   context.CancelFunc
	suppress atomic.Bool // drop replayed updates during session/load

	SessionID    string
	CanLoad      bool
	AgentInfo    map[string]any
	CurrentModel string
	ModelOptions []ModelOption
}

// Start spawns the agent and performs the ACP initialize handshake.
func Start(ctx context.Context, o Options, h Handlers) (*Client, error) {
	argv := append(append([]string{}, o.Wrap...), o.Command)
	argv = append(argv, o.Args...)
	cctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = o.Dir
	cmd.Env = os.Environ()
	for k, v := range o.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	setProcAttrs(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start %s: %w", o.Command, err)
	}
	c := &Client{cmd: cmd, stdin: stdin, pending: map[int64]chan rpcMsg{}, h: h, done: make(chan struct{}), ctx: cctx, cancel: cancel}
	go c.readLoop(stdout)
	go c.stderrLoop(stderr)
	go func() {
		c.exitErr = cmd.Wait()
		close(c.done)
		c.failPending()
	}()

	var initRes struct {
		ProtocolVersion   int            `json:"protocolVersion"`
		AgentCapabilities map[string]any `json:"agentCapabilities"`
		AgentInfo         map[string]any `json:"agentInfo"`
	}
	err = c.call(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]any{"name": "xissue", "version": "0.1.0"},
	}, &initRes)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if v, ok := initRes.AgentCapabilities["loadSession"].(bool); ok {
		c.CanLoad = v
	}
	c.AgentInfo = initRes.AgentInfo
	return c, nil
}

type rawConfigOption struct {
	ID           string `json:"id"`
	Category     string `json:"category"`
	CurrentValue string `json:"currentValue"`
	Options      []struct {
		Value string `json:"value"`
		Name  string `json:"name"`
	} `json:"options"`
}

func (c *Client) parseConfigOptions(opts []rawConfigOption) {
	for _, o := range opts {
		if o.ID == "model" || o.Category == "model" {
			c.CurrentModel = o.CurrentValue
			models := make([]ModelOption, 0, len(o.Options))
			for _, item := range o.Options {
				val := strings.TrimSpace(item.Value)
				if val == "" {
					continue
				}
				prov := "default"
				if idx := strings.IndexByte(val, '/'); idx > 0 {
					prov = val[:idx]
				}
				name := strings.TrimSpace(item.Name)
				if name == "" {
					name = val
				}
				models = append(models, ModelOption{
					Value:    val,
					Name:     name,
					Provider: prov,
				})
			}
			c.ModelOptions = models
			return
		}
	}
}

// NewSession creates a fresh session rooted at cwd.
func (c *Client) NewSession(ctx context.Context, cwd string) error {
	var res struct {
		SessionID     string            `json:"sessionId"`
		ConfigOptions []rawConfigOption `json:"configOptions"`
	}
	if err := c.call(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}, &res); err != nil {
		return err
	}
	c.SessionID = res.SessionID
	c.parseConfigOptions(res.ConfigOptions)
	return nil
}

// LoadSession resumes an existing session. Replayed history is not re-emitted.
func (c *Client) LoadSession(ctx context.Context, cwd, sessionID string) error {
	if !c.CanLoad {
		return errors.New("agent does not support session/load")
	}
	c.suppress.Store(true)
	defer c.suppress.Store(false)
	var res struct {
		ConfigOptions []rawConfigOption `json:"configOptions"`
	}
	if err := c.call(ctx, "session/load", map[string]any{"sessionId": sessionID, "cwd": cwd, "mcpServers": []any{}}, &res); err != nil {
		return err
	}
	c.SessionID = sessionID
	c.parseConfigOptions(res.ConfigOptions)
	return nil
}

// SetModel switches the session's active model via ACP session/set_config_option.
func (c *Client) SetModel(ctx context.Context, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	var res struct {
		ConfigOptions []rawConfigOption `json:"configOptions"`
	}
	if err := c.call(ctx, "session/set_config_option", map[string]any{
		"sessionId": c.SessionID,
		"configId":  "model",
		"value":     model,
	}, &res); err != nil {
		return err
	}
	c.parseConfigOptions(res.ConfigOptions)
	return nil
}

// Prompt sends a user turn and blocks until the agent finishes. Returns the stop reason.
func (c *Client) Prompt(ctx context.Context, text string) (string, error) {
	var res struct {
		StopReason string `json:"stopReason"`
	}
	err := c.call(ctx, "session/prompt", map[string]any{
		"sessionId": c.SessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": text}},
	}, &res)
	return res.StopReason, err
}

// SetHandlers updates the event and permission callbacks (used when reusing a warm Client across turns).
func (c *Client) SetHandlers(h Handlers) {
	c.hMu.Lock()
	c.h = h
	c.hMu.Unlock()
}

func (c *Client) handlers() Handlers {
	c.hMu.RLock()
	defer c.hMu.RUnlock()
	return c.h
}

// Alive reports whether the underlying agent process is still running.
func (c *Client) Alive() bool {
	if c == nil {
		return false
	}
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// Cancel asks the agent to stop the current turn.
func (c *Client) Cancel() {
	_ = c.notify("session/cancel", map[string]any{"sessionId": c.SessionID})
}

// Done is closed when the agent process exits.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close terminates the agent process.
func (c *Client) Close() {
	_ = c.stdin.Close()
	killGroup(c.cmd)
	c.cancel()
}

// ---------- transport ----------

func (c *Client) send(m rpcMsg) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

func (c *Client) notify(method string, params any) error {
	p, _ := json.Marshal(params)
	return c.send(rpcMsg{Method: method, Params: p})
}

func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	raw := json.RawMessage(fmt.Sprintf("%d", id))
	ch := make(chan rpcMsg, 1)
	c.pendMu.Lock()
	c.pending[id] = ch
	c.pendMu.Unlock()
	defer func() {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
	}()
	p, _ := json.Marshal(params)
	if err := c.send(rpcMsg{ID: &raw, Method: method, Params: p}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.exitError()
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if out != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	}
}

func (c *Client) exitError() error {
	if c.exitErr != nil {
		return fmt.Errorf("agent exited: %w", c.exitErr)
	}
	return errors.New("agent exited")
}

func (c *Client) failPending() {
	c.pendMu.Lock()
	defer c.pendMu.Unlock()
	for id, ch := range c.pending {
		select {
		case ch <- rpcMsg{Error: &rpcError{Code: -32000, Message: "agent exited"}}:
		default:
		}
		delete(c.pending, id)
	}
}

func (c *Client) readLoop(r io.Reader) {
	defer func() {
		_ = recover()
	}()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var m rpcMsg
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		switch {
		case m.Method != "" && m.ID != nil:
			go c.handleRequest(m)
		case m.Method != "":
			c.handleNotification(m)
		case m.ID != nil:
			var id int64
			if err := json.Unmarshal(*m.ID, &id); err != nil {
				continue
			}
			c.pendMu.Lock()
			ch := c.pending[id]
			c.pendMu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
}

func (c *Client) stderrLoop(r io.Reader) {
	defer func() {
		_ = recover()
	}()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		h := c.handlers()
		if h.OnEvent != nil {
			func() {
				defer func() { _ = recover() }()
				h.OnEvent(Event{Type: "stderr", Data: map[string]any{"text": sc.Text()}})
			}()
		}
	}
}

func (c *Client) reply(id *json.RawMessage, result any, rerr *rpcError) {
	m := rpcMsg{ID: id, Error: rerr}
	if rerr == nil {
		b, _ := json.Marshal(result)
		m.Result = b
	}
	_ = c.send(m)
}

func (c *Client) handleRequest(m rpcMsg) {
	defer func() {
		if rec := recover(); rec != nil {
			c.reply(m.ID, map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}, nil)
		}
	}()
	switch m.Method {
	case "session/request_permission":
		var req PermissionRequest
		_ = json.Unmarshal(m.Params, &req)
		optionID := ""
		h := c.handlers()
		if h.OnPermission != nil {
			optionID = h.OnPermission(c.ctx, req)
		}
		if optionID == "" {
			c.reply(m.ID, map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}, nil)
			return
		}
		c.reply(m.ID, map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": optionID}}, nil)
	default:
		c.reply(m.ID, nil, &rpcError{Code: -32601, Message: "method not found: " + m.Method})
	}
}

func (c *Client) handleNotification(m rpcMsg) {
	defer func() {
		_ = recover()
	}()
	h := c.handlers()
	if m.Method != "session/update" || h.OnEvent == nil || c.suppress.Load() {
		return
	}
	var p struct {
		Update map[string]any `json:"update"`
	}
	if err := json.Unmarshal(m.Params, &p); err != nil || p.Update == nil {
		return
	}
	u := p.Update
	kind, _ := u["sessionUpdate"].(string)
	switch kind {
	case "agent_message_chunk":
		if t := contentText(u["content"]); t != "" {
			h.OnEvent(Event{Type: "message", Data: map[string]any{"text": t}})
		}
	case "agent_thought_chunk":
		if t := contentText(u["content"]); t != "" {
			h.OnEvent(Event{Type: "thought", Data: map[string]any{"text": t}})
		}
	case "tool_call":
		delete(u, "sessionUpdate")
		h.OnEvent(Event{Type: "tool_call", Data: u})
	case "tool_call_update":
		delete(u, "sessionUpdate")
		h.OnEvent(Event{Type: "tool_call_update", Data: u})
	case "plan":
		h.OnEvent(Event{Type: "plan", Data: map[string]any{"entries": u["entries"]}})
	}
}

func contentText(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	if t, _ := m["type"].(string); t == "text" {
		s, _ := m["text"].(string)
		return s
	}
	return ""
}
