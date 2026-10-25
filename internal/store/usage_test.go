package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageReportAggregatesRunsByCurrencyAndDay(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, err := st.CreateProject(ctx, "Usage", "")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := st.CreateLocalIssue(ctx, p.ID, "usage", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	r1, err := st.CreateRun(ctx, issue.ID, "fake", "")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := st.CreateRun(ctx, issue.ID, "fake", "")
	if err != nil {
		t.Fatal(err)
	}
	in1, out1, total1 := int64(100), int64(20), int64(120)
	used1, size1 := int64(1000), int64(200000)
	cost1, cum1 := 0.02, 0.02
	usd := "USD"
	if _, err := st.UpdateRun(ctx, r1.ID, RunUpdate{
		InputTokens: &in1, OutputTokens: &out1, TotalTokens: &total1,
		ContextUsed: &used1, ContextSize: &size1,
		CostAmount: &cost1, CostCurrency: &usd, UsageCumulativeCost: &cum1,
	}); err != nil {
		t.Fatal(err)
	}
	in2, out2, total2 := int64(50), int64(10), int64(60)
	cost2, cum2 := 0.03, 0.03
	eur := "EUR"
	if _, err := st.UpdateRun(ctx, r2.ID, RunUpdate{
		InputTokens: &in2, OutputTokens: &out2, TotalTokens: &total2,
		CostAmount: &cost2, CostCurrency: &eur, UsageCumulativeCost: &cum2,
	}); err != nil {
		t.Fatal(err)
	}

	rep, err := st.UsageReport(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Runs != 2 || rep.Totals.TotalTokens != 180 || rep.Totals.InputTokens != 150 || rep.Totals.OutputTokens != 30 {
		t.Fatalf("totals = %+v", rep.Totals)
	}
	if rep.Totals.CostByCurrency["USD"] != 0.02 || rep.Totals.CostByCurrency["EUR"] != 0.03 {
		t.Fatalf("cost totals = %+v", rep.Totals.CostByCurrency)
	}
	if len(rep.Daily) != 1 || rep.Daily[0].TotalTokens != 180 || rep.Daily[0].Runs != 2 {
		t.Fatalf("daily = %+v", rep.Daily)
	}
	if len(rep.TopRuns) != 2 || rep.TopRuns[0].CostAmount != 0.03 {
		t.Fatalf("top runs = %+v", rep.TopRuns)
	}
}
