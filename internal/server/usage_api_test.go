package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xpier/xissue/internal/store"
)

func TestUsageAPIReportsRunUsage(t *testing.T) {
	srv, st, _ := newTestServer(t)
	ctx := context.Background()
	p, err := st.CreateProject(ctx, "Usage", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, p.ID, "usage", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.CreateRun(ctx, issue.ID, "fake", "model-x")
	if err != nil {
		t.Fatal(err)
	}
	in, out, total := int64(12), int64(3), int64(15)
	cost, cum := 0.0042, 0.0042
	cur := "USD"
	if _, err := st.UpdateRun(ctx, run.ID, store.RunUpdate{
		InputTokens: &in, OutputTokens: &out, TotalTokens: &total,
		CostAmount: &cost, CostCurrency: &cur, UsageCumulativeCost: &cum,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/usage?days=7", nil)
	req.Host = "127.0.0.1:7789"
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET usage: %d %s", w.Code, w.Body.String())
	}
	var rep store.UsageReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Runs != 1 || rep.Totals.TotalTokens != 15 || rep.Totals.CostByCurrency["USD"] != 0.0042 {
		t.Fatalf("totals = %+v", rep.Totals)
	}
	if len(rep.TopRuns) != 1 || rep.TopRuns[0].ID != run.ID || rep.TopRuns[0].Model != "model-x" {
		t.Fatalf("top runs = %+v", rep.TopRuns)
	}
}
