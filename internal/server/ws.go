package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/xpier/xissue/internal/domain"
)

// Hub fans out server events to all connected browser WebSocket clients.
type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func newHub() *Hub {
	return &Hub{clients: map[chan []byte]struct{}{}}
}

// ClientCount reports how many browser WebSockets are connected.
func (h *Hub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) broadcast(payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- b:
		default:
		}
	}
}

func (h *Hub) BroadcastRunEvent(runID, issueID int64, ev *domain.RunEvent) {
	h.broadcast(map[string]any{
		"type":    "run_event",
		"runId":   runID,
		"issueId": issueID,
		"event":   ev,
	})
}

func (h *Hub) BroadcastRunUpdated(run *domain.Run) {
	h.broadcast(map[string]any{
		"type": "run_updated",
		"run":  run,
	})
}

func (h *Hub) BroadcastIssueUpdated(issue *domain.Issue) {
	h.broadcast(map[string]any{
		"type":  "issue_updated",
		"issue": issue,
	})
}

func (h *Hub) BroadcastNotification(n *domain.Notification) {
	h.broadcast(map[string]any{
		"type":         "notification",
		"notification": n,
	})
}

// BroadcastStatus pushes the live system status over the existing WebSocket,
// so the status bar does not need to poll HTTP while connected.
func (h *Hub) BroadcastStatus(st SystemStatus) {
	h.broadcast(map[string]any{
		"type":   "status",
		"status": st,
	})
}

func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	patterns := []string{
		"127.0.0.1:*",
		"localhost:*",
		"[::1]:*",
	}
	if host, _, err := net.SplitHostPort(r.Host); err == nil && host != "" {
		patterns = append(patterns, host+":*", host)
	} else if r.Host != "" {
		patterns = append(patterns, r.Host+":*", r.Host)
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: patterns,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}()

	ctx := conn.CloseRead(r.Context())
	pingTicker := time.NewTicker(25 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pingTicker.C:
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
			wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
			_ = conn.Write(wctx, websocket.MessageText, []byte(`{"type":"heartbeat"}`))
			wcancel()
		case msg := <-ch:
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
