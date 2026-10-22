package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleMCPStreamableHTTP(t *testing.T) {
	srv := &Server{}
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`

	req := httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(initReq))
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	srv.handleMCP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("initialize status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON content type, got %q", ct)
	}
	var resp struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode initialize: %v", err)
	}
	if resp.Result.ProtocolVersion != "2024-11-05" {
		t.Fatalf("protocol version not negotiated: %q", resp.Result.ProtocolVersion)
	}

	// Clients that only accept SSE must still receive a valid one-shot event.
	req = httptest.NewRequest(http.MethodPost, "/api/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"ping"}`))
	req.Header.Set("Accept", "text/event-stream")
	w = httptest.NewRecorder()
	srv.handleMCP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sse ping status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected SSE content type, got %q", ct)
	}
	if !strings.Contains(w.Body.String(), "event: message") || !strings.Contains(w.Body.String(), `"id":2`) {
		t.Fatalf("unexpected SSE body: %q", w.Body.String())
	}

	// Batch requests and stateless DELETE support.
	req = httptest.NewRequest(http.MethodPost, "/api/mcp",
		strings.NewReader(`[{"jsonrpc":"2.0","id":3,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"}]`))
	req.Header.Set("Accept", "application/json")
	w = httptest.NewRecorder()
	srv.handleMCP(w, req)
	if w.Code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(w.Body.String()), "[") {
		t.Fatalf("batch response status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/mcp", nil)
	w = httptest.NewRecorder()
	srv.handleMCP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d", w.Code)
	}
}

func TestMCPHTTPURL(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:7789": "http://127.0.0.1:7789/api/mcp",
		"0.0.0.0:7789":   "http://127.0.0.1:7789/api/mcp",
		"[::]:7789":      "http://127.0.0.1:7789/api/mcp",
	}
	for in, want := range cases {
		if got := MCPHTTPURL(in); got != want {
			t.Errorf("MCPHTTPURL(%q)=%q want %q", in, got, want)
		}
	}
}
