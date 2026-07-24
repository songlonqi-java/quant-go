package forward

import (
	"path/filepath"
	"testing"

	"quant/internal/data"
	"quant/internal/market"
	"quant/internal/signal"
	"quant/internal/strategy"
)

func TestValidateStopsAfterInvalidBreakPrevLow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, picksFile)
	if err := writeRows(path, []map[string]string{
		{
			"signal_date": "20260101",
			"target_date": "20260102",
			"rank":        "1",
			"code":        "000001.SZ",
			"name":        "平安银行",
			"close":       "10.00",
			"status":      "pending",
		},
	}); err != nil {
		t.Fatalf("writeRows() error = %v", err)
	}

	bars := []data.DailyBar{
		forwardBar("20260101", 10, 10.2, 9.5, 10),
		forwardBar("20260102", 10.1, 10.3, 9.4, 10.2),
		forwardBar("20260103", 10.2, 10.6, 10.1, 10.5),
		forwardBar("20260104", 10.5, 11.0, 10.3, 10.8),
		forwardBar("20260105", 10.8, 11.2, 10.6, 11.0),
		forwardBar("20260106", 11.0, 11.4, 10.8, 11.2),
	}

	updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}

	rows, err := readRows(path)
	if err != nil {
		t.Fatalf("readRows() error = %v", err)
	}
	row := rows[0]
	if row["status"] != "invalid_break_prev_low" {
		t.Fatalf("status = %q, want invalid_break_prev_low", row["status"])
	}
	if row["day3_close"] != "" || row["day5_close"] != "" {
		t.Fatalf("invalid trade should not have day3/day5 returns: row=%v", row)
	}
}

func TestValidateStopsAfterLimitUpOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, picksFile)
	if err := writeRows(path, []map[string]string{
		{
			"signal_date": "20260101",
			"target_date": "20260102",
			"rank":        "1",
			"code":        "000001.SZ",
			"name":        "平安银行",
			"close":       "10.00",
			"status":      "pending",
		},
	}); err != nil {
		t.Fatalf("writeRows() error = %v", err)
	}

	bars := []data.DailyBar{
		forwardBar("20260101", 10, 10.2, 9.5, 10),
		forwardBar("20260102", 10.5, 10.5, 10.5, 10.5),
		forwardBar("20260103", 10.6, 10.8, 10.4, 10.7),
		forwardBar("20260104", 10.7, 10.9, 10.5, 10.8),
	}
	bars[1].UpLimit = 10.5
	bars[1].DownLimit = 9.5

	updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}

	rows, err := readRows(path)
	if err != nil {
		t.Fatalf("readRows() error = %v", err)
	}
	row := rows[0]
	if row["status"] != "no_trade_limit_up" {
		t.Fatalf("status = %q, want no_trade_limit_up", row["status"])
	}
	if row["next_return_pct"] != "" || row["day3_close"] != "" || row["day5_close"] != "" {
		t.Fatalf("limit-up no-trade should not have returns: row=%v", row)
	}
}

func TestRecordWithDecisionWritesCashRow(t *testing.T) {
	dir := t.TempDir()
	results := []signal.SignalResult{
		{
			Horizon:    strategy.HorizonShort,
			Code:       "000001.SZ",
			Name:       "平安银行",
			Date:       "20260101",
			BuyCount:   3,
			TotalScore: 2.2,
			Confidence: 80,
			Suppressed: true,
		},
	}
	decision := signal.PositionDecision{
		Action:         signal.PositionActionCash,
		CandidateBuys:  1,
		QualifiedBuys:  0,
		SuppressedBuys: 1,
		Reasons:        []string{"市场情绪偏空", "没有通过风控的买入候选"},
		Advice:         "不新增仓位",
	}

	err := RecordWithDecision(dir, results, &market.MarketStatus{Sentiment: "偏空", Advice: "建议空仓"}, 5, []string{"20260101", "20260102"}, decision)
	if err != nil {
		t.Fatalf("RecordWithDecision() error = %v", err)
	}

	rows, err := readRows(filepath.Join(dir, picksFile))
	if err != nil {
		t.Fatalf("readRows() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if row["code"] != "CASH" {
		t.Fatalf("code = %q, want CASH", row["code"])
	}
	if row["status"] != "cash" {
		t.Fatalf("status = %q, want cash", row["status"])
	}
	if row["position_pct"] != "0.0" {
		t.Fatalf("position_pct = %q, want 0.0", row["position_pct"])
	}
}

func forwardBar(date string, open, high, low, close float64) data.DailyBar {
	return data.DailyBar{
		TsCode:    "000001.SZ",
		TradeDate: date,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Vol:       1000,
		RawOpen:   open,
		RawHigh:   high,
		RawLow:    low,
		RawClose:  close,
		AdjFactor: 1,
	}
}
