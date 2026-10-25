package store

import (
	"context"
	"time"

	"github.com/xpier/xissue/internal/domain"
)

// UsageTotal is the aggregate usage over a time window.
type UsageTotal struct {
	Runs           int64              `json:"runs"`
	InputTokens    int64              `json:"inputTokens"`
	OutputTokens   int64              `json:"outputTokens"`
	TotalTokens    int64              `json:"totalTokens"`
	CostByCurrency map[string]float64 `json:"costByCurrency"`
}

// UsageDay is one UTC day of usage.
type UsageDay struct {
	Date           string             `json:"date"`
	Runs           int64              `json:"runs"`
	InputTokens    int64              `json:"inputTokens"`
	OutputTokens   int64              `json:"outputTokens"`
	TotalTokens    int64              `json:"totalTokens"`
	CostByCurrency map[string]float64 `json:"costByCurrency"`
}

// UsageReport is returned by GET /api/usage.
type UsageReport struct {
	Since   string       `json:"since"`
	Until   string       `json:"until"`
	Totals  UsageTotal   `json:"totals"`
	Daily   []UsageDay   `json:"daily"`
	TopRuns []domain.Run `json:"topRuns"`
}

// UsageReport aggregates per-run usage. A run's cost is already attributed
// incrementally from ACP cumulative cost, so summing runs does not double count.
func (s *Store) UsageReport(ctx context.Context, since time.Time) (UsageReport, error) {
	sinceStr := since.UTC().Format(time.RFC3339Nano)
	until := time.Now().UTC()
	rep := UsageReport{
		Since:   since.UTC().Format(time.RFC3339Nano),
		Until:   until.Format(time.RFC3339Nano),
		Totals:  UsageTotal{CostByCurrency: map[string]float64{}},
		Daily:   []UsageDay{},
		TopRuns: []domain.Run{},
	}
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(total_tokens),0)
		FROM runs WHERE created_at >= ?`, sinceStr).
		Scan(&rep.Totals.Runs, &rep.Totals.InputTokens, &rep.Totals.OutputTokens, &rep.Totals.TotalTokens)
	if err != nil {
		return rep, err
	}
	if err := s.scanUsageCosts(ctx, `SELECT cost_currency, SUM(cost_amount) FROM runs WHERE created_at >= ? AND cost_amount > 0 GROUP BY cost_currency`, sinceStr, rep.Totals.CostByCurrency); err != nil {
		return rep, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT substr(created_at,1,10) AS day, COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(total_tokens),0)
		FROM runs WHERE created_at >= ? GROUP BY day ORDER BY day`, sinceStr)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var d UsageDay
		d.CostByCurrency = map[string]float64{}
		if err := rows.Scan(&d.Date, &d.Runs, &d.InputTokens, &d.OutputTokens, &d.TotalTokens); err != nil {
			rows.Close()
			return rep, err
		}
		rep.Daily = append(rep.Daily, d)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return rep, err
	}
	rows.Close()
	costRows, err := s.db.QueryContext(ctx, `SELECT substr(created_at,1,10) AS day, cost_currency, SUM(cost_amount)
		FROM runs WHERE created_at >= ? AND cost_amount > 0 GROUP BY day, cost_currency ORDER BY day`, sinceStr)
	if err != nil {
		return rep, err
	}
	dayCosts := map[string]map[string]float64{}
	for costRows.Next() {
		var day, currency string
		var amount float64
		if err := costRows.Scan(&day, &currency, &amount); err != nil {
			costRows.Close()
			return rep, err
		}
		if currency == "" {
			currency = "USD"
		}
		if dayCosts[day] == nil {
			dayCosts[day] = map[string]float64{}
		}
		dayCosts[day][currency] += amount
	}
	if err := costRows.Err(); err != nil {
		costRows.Close()
		return rep, err
	}
	costRows.Close()
	for i := range rep.Daily {
		if costs := dayCosts[rep.Daily[i].Date]; costs != nil {
			rep.Daily[i].CostByCurrency = costs
		}
	}

	top, err := s.db.QueryContext(ctx, `SELECT `+runCols+` FROM runs WHERE created_at >= ? ORDER BY cost_amount DESC, total_tokens DESC LIMIT 10`, sinceStr)
	if err != nil {
		return rep, err
	}
	defer top.Close()
	for top.Next() {
		r, err := scanRun(top)
		if err != nil {
			return rep, err
		}
		rep.TopRuns = append(rep.TopRuns, r)
	}
	return rep, top.Err()
}

func (s *Store) scanUsageCosts(ctx context.Context, query string, since string, dest map[string]float64, extra ...any) error {
	args := []any{since}
	args = append(args, extra...)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var currency string
		var amount float64
		if err := rows.Scan(&currency, &amount); err != nil {
			return err
		}
		if currency == "" {
			currency = "USD"
		}
		dest[currency] += amount
	}
	return rows.Err()
}
