package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestSystemStatus(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Host = "127.0.0.1:7789"
	req.Header.Set("X-XIssue", "1")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	var st SystemStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Version != Version || st.PID <= 0 || st.Memory.HeapAlloc == 0 || st.CPU.Cores != runtime.NumCPU() {
		t.Fatalf("unexpected status: %+v", st)
	}
	if st.Disk.Total == 0 || st.Disk.Free == 0 {
		t.Fatalf("disk stats missing: %+v", st.Disk)
	}
	if st.GC.NextGCBytes == 0 {
		t.Fatalf("GC stats missing: %+v", st.GC)
	}
}
