package server

import (
	"encoding/json"
	"testing"
)

func TestBroadcastStatusUsesWebSocketMessage(t *testing.T) {
	h := newHub()
	ch := make(chan []byte, 1)
	h.clients[ch] = struct{}{}
	if h.ClientCount() != 1 {
		t.Fatalf("ClientCount = %d, want 1", h.ClientCount())
	}
	h.BroadcastStatus(SystemStatus{Version: "9.9.9", CPU: CPUStatus{ProcessPercent: 1.5}})
	select {
	case raw := <-ch:
		var msg struct {
			Type   string       `json:"type"`
			Status SystemStatus `json:"status"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Type != "status" || msg.Status.Version != "9.9.9" || msg.Status.CPU.ProcessPercent != 1.5 {
			t.Fatalf("unexpected status message: %+v", msg)
		}
	default:
		t.Fatal("expected status message")
	}
}
