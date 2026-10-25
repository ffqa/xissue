package runner

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/xpier/xissue/internal/agent/acp"
	"github.com/xpier/xissue/internal/store"
)

func asInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func asFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

func usageCost(v any) (amount float64, currency string, ok bool) {
	m, isMap := v.(map[string]any)
	if !isMap || m == nil {
		return 0, "", false
	}
	amount = asFloat64(m["amount"])
	currency = strings.TrimSpace(stringValue(m["currency"]))
	return amount, currency, true
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

// recordUsage handles a stable ACP usage_update. ACP cost is cumulative for
// the session, so only the delta since the previous update is attributed to
// this run. Context usage is a snapshot, not additive.
func (r *Runner) recordUsage(act *activeRun, runID, issueID int64, data map[string]any) {
	used := asInt64(data["used"])
	size := asInt64(data["size"])
	amount, currency, hasCost := usageCost(data["cost"])
	_, hasUsed := data["used"]
	_, hasSize := data["size"]
	if !hasUsed && !hasSize && !hasCost {
		return
	}

	act.usageMu.Lock()
	defer act.usageMu.Unlock()
	ctx := context.Background()
	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		return
	}
	upd := store.RunUpdate{}
	now := time.Now()
	upd.UsageUpdatedAt = &now
	if hasUsed {
		upd.ContextUsed = &used
	}
	if hasSize {
		upd.ContextSize = &size
	}
	if hasCost {
		delta := amount - run.UsageCumulativeCost
		if delta < 0 {
			// A new session can reset cumulative cost to zero.
			delta = amount
		}
		if delta < 0 {
			delta = 0
		}
		total := run.CostAmount + delta
		upd.CostAmount = &total
		upd.UsageCumulativeCost = &amount
		if currency != "" {
			upd.CostCurrency = &currency
		}
		act.usageSawCost = true
	}
	if raw, err := json.Marshal(data); err == nil {
		s := string(raw)
		upd.UsageJSON = &s
	}
	if u, err := r.st.UpdateRun(ctx, runID, upd); err == nil && r.notify != nil {
		r.notify.BroadcastRunUpdated(u)
	}
}

// recordTurnUsage consumes the optional end-turn token usage object. Token
// counts are additive per run; prompt-level cost is only used when the agent
// did not send stable usage_update cost during the turn.
func (r *Runner) recordTurnUsage(act *activeRun, runID, issueID int64, usage *acp.Usage) {
	if usage == nil {
		return
	}
	act.usageMu.Lock()
	defer act.usageMu.Unlock()
	ctx := context.Background()
	run, err := r.st.GetRun(ctx, runID)
	if err != nil {
		return
	}
	upd := store.RunUpdate{}
	input := run.InputTokens + usage.InputTokens
	output := run.OutputTokens + usage.OutputTokens
	total := run.TotalTokens + usage.TotalTokens
	if usage.TotalTokens == 0 {
		total = run.TotalTokens + usage.InputTokens + usage.OutputTokens
	}
	upd.InputTokens = &input
	upd.OutputTokens = &output
	upd.TotalTokens = &total
	if usage.Cost != nil && !act.usageSawCost && run.UsageCumulativeCost == 0 {
		cost := run.CostAmount + usage.Cost.Amount
		upd.CostAmount = &cost
		// The prompt fallback is only used before any cumulative report, so
		// this total is also the current session baseline. Persisting it stops
		// a later usage_update from re-attributing the same spend.
		upd.UsageCumulativeCost = &cost
		if strings.TrimSpace(usage.Cost.Currency) != "" {
			cur := strings.TrimSpace(usage.Cost.Currency)
			upd.CostCurrency = &cur
		}
		act.usageSawCost = true
	}
	now := time.Now()
	upd.UsageUpdatedAt = &now
	if raw, err := json.Marshal(usage); err == nil {
		s := string(raw)
		upd.UsageJSON = &s
	}
	if u, err := r.st.UpdateRun(ctx, runID, upd); err == nil && r.notify != nil {
		r.notify.BroadcastRunUpdated(u)
	}
}
