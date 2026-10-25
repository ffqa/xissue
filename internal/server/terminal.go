package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/coder/websocket"
)

type terminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

func clampTerminalSize(cols, rows int) (int, int) {
	if cols < 20 {
		cols = 20
	}
	if cols > 400 {
		cols = 400
	}
	if rows < 5 {
		rows = 5
	}
	if rows > 200 {
		rows = 200
	}
	return cols, rows
}

type terminalOutput struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
}

// handleRunTerminal upgrades to a WebSocket backed by a PTY rooted at the run
// workspace. It is intentionally scoped to an existing Run; callers cannot
// choose an arbitrary directory.
func (s *Server) handleRunTerminal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	run, err := s.st.GetRun(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if run.RunnerID != "" {
		writeErr(w, http.StatusConflict, errors.New("remote run workspaces are on the remote runner; use a local run for the terminal"))
		return
	}
	if run.Workdir == "" {
		writeErr(w, http.StatusConflict, errors.New("run has no workspace"))
		return
	}
	workdir := filepath.Clean(run.Workdir)
	info, err := os.Stat(workdir)
	if err != nil || !info.IsDir() {
		writeErr(w, http.StatusGone, errors.New("run workspace is no longer available"))
		return
	}

	ptmx, cmd, err := startRunPTY(workdir, 100, 30)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: websocketOriginPatterns(r),
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	ptyDone := make(chan struct{})
	go func() {
		defer close(ptyDone)
		buf := make([]byte, 8192)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				payload, _ := json.Marshal(terminalOutput{
					Type: "output",
					Data: base64.StdEncoding.EncodeToString(buf[:n]),
				})
				wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
				werr := conn.Write(wctx, websocket.MessageText, payload)
				wcancel()
				if werr != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()
	go func() {
		<-ptyDone
		payload, _ := json.Marshal(terminalOutput{Type: "exit"})
		wctx, wcancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = conn.Write(wctx, websocket.MessageText, payload)
		wcancel()
		_ = conn.Close(websocket.StatusNormalClosure, "shell exited")
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg terminalMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "input":
			if msg.Data != "" {
				if _, err := ptmx.Write([]byte(msg.Data)); err != nil {
					return
				}
			}
		case "resize":
			cols, rows := clampTerminalSize(msg.Cols, msg.Rows)
			if cols > 0 && rows > 0 {
				_ = resizePTY(ptmx, cols, rows)
			}
		}
	}
}
