package forward

import (
	"path/filepath"
	"testing"

	"quant/internal/data"
	"quant/internal/market"
	"quant/internal/signal"
	"quant/internal/strategy"
)

func TestValidateKeepsOpenTradeAfterIntradayBreakPrevLow(t *testing.T) {
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
	if updated != 3 {
		t.Fatalf("updated = %d, want 3", updated)
	}

	rows, err := readRows(path)
	if err != nil {
		t.Fatalf("readRows() error = %v", err)
	}
	row := rows[0]
	if row["status"] != "validated_5d" {
		t.Fatalf("status = %q, want validated_5d", row["status"])
	}
	if row["next_return_pct"] == "" || row["day3_close"] == "" || row["day5_close"] == "" {
		t.Fatalf("open trade should retain all returns after intraday weakness: row=%v", row)
	}
	if row["notes"] == "" {
		t.Fatalf("intraday break should remain visible as a risk note: row=%v", row)
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
	if row["benchmark"] != "MARKET_PROXY_EQUAL_WEIGHT" {
		t.Fatalf("benchmark = %q, want MARKET_PROXY_EQUAL_WEIGHT", row["benchmark"])
	}
}

func TestValidateCashRowWritesMarketProxyReturns(t *testing.T) {
	dir := t.TempDir()
	results := []signal.SignalResult{{Date: "20260101", Horizon: strategy.HorizonShort, Code: "000001.SZ"}}
	decision := signal.PositionDecision{Action: signal.PositionActionCash, Advice: "建议空仓"}
	if err := RecordWithDecision(dir, results, nil, 5, []string{"20260101", "20260102", "20260103", "20260104", "20260105", "20260106"}, decision); err != nil {
		t.Fatalf("RecordWithDecision() error = %v", err)
	}

	codeOne := []data.DailyBar{
		forwardBar("20260101", 10, 10, 10, 10),
		forwardBar("20260102", 10, 11, 10, 11),
		forwardBar("20260103", 11, 12, 11, 12),
		forwardBar("20260104", 12, 13, 12, 13),
		forwardBar("20260105", 13, 14, 13, 14),
		forwardBar("20260106", 14, 15, 14, 15),
	}
	codeOne[0].TsCode = "000001.SZ"
	codeOne[1].TsCode = "000001.SZ"
	codeOne[2].TsCode = "000001.SZ"
	codeOne[3].TsCode = "000001.SZ"
	codeOne[4].TsCode = "000001.SZ"
	codeOne[5].TsCode = "000001.SZ"
	codeTwo := []data.DailyBar{
		forwardBar("20260101", 20, 20, 20, 20),
		forwardBar("20260102", 20, 20, 19, 19),
		forwardBar("20260103", 19, 19, 18, 18),
		forwardBar("20260104", 18, 18, 17, 17),
		forwardBar("20260105", 17, 17, 16, 16),
		forwardBar("20260106", 16, 16, 15, 15),
	}
	for i := range codeTwo {
		codeTwo[i].TsCode = "000002.SZ"
	}

	updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": codeOne, "000002.SZ": codeTwo})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if updated != 3 {
		t.Fatalf("updated = %d, want 3 market-proxy fields", updated)
	}
	rows, err := readRows(filepath.Join(dir, picksFile))
	if err != nil {
		t.Fatalf("readRows() error = %v", err)
	}
	row := rows[0]
	if row["next_return_pct"] == "" || row["day3_return_pct"] == "" || row["day5_return_pct"] == "" {
		t.Fatalf("cash benchmark returns not filled: row=%v", row)
	}
	if row["status"] != "cash_validated_5d" {
		t.Fatalf("status = %q, want cash_validated_5d", row["status"])
	}
}

func TestRecordWithDecisionKeepsLimitPerHorizon(t *testing.T) {
	dir := t.TempDir()
	results := []signal.SignalResult{
		forwardResult(strategy.HorizonShort, "000001.SZ"),
		forwardResult(strategy.HorizonShort, "000002.SZ"),
		forwardResult(strategy.HorizonMid, "000003.SZ"),
		forwardResult(strategy.HorizonMid, "000004.SZ"),
		forwardResult(strategy.HorizonLong, "000005.SZ"),
		forwardResult(strategy.HorizonLong, "000006.SZ"),
	}

	err := RecordWithDecision(dir, results, &market.MarketStatus{Sentiment: "偏多"}, 1, []string{"20260101", "20260102"}, signal.PositionDecision{})
	if err != nil {
		t.Fatalf("RecordWithDecision() error = %v", err)
	}

	rows, err := readRows(filepath.Join(dir, picksFile))
	if err != nil {
		t.Fatalf("readRows() error = %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row["horizon"]] = true
	}
	for _, horizon := range []strategy.Horizon{strategy.HorizonShort, strategy.HorizonMid, strategy.HorizonLong} {
		if !seen[string(horizon)] {
			t.Fatalf("missing horizon %s in rows=%v", horizon, rows)
		}
	}
}

func TestValidateUsesMidHorizonTargets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, picksFile)
	if err := writeRows(path, []map[string]string{
		{
			"signal_date": "20260101",
			"target_date": "20260102",
			"horizon":     string(strategy.HorizonMid),
			"rank":        "1",
			"code":        "000001.SZ",
			"name":        "平安银行",
			"close":       "10.00",
			"status":      "pending",
		},
	}); err != nil {
		t.Fatalf("writeRows() error = %v", err)
	}

	var bars []data.DailyBar
	for i := 0; i < 45; i++ {
		bars = append(bars, forwardBarDateOffset(i, 10+float64(i)*0.1))
	}

	updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if updated != 4 {
		t.Fatalf("updated = %d, want 4", updated)
	}
	rows, err := readRows(path)
	if err != nil {
		t.Fatalf("readRows() error = %v", err)
	}
	row := rows[0]
	if row["day3_close"] != "" || row["day5_close"] != "" {
		t.Fatalf("mid horizon should not fill short fields: row=%v", row)
	}
	if row["day10_close"] == "" || row["day20_close"] == "" || row["day40_close"] == "" {
		t.Fatalf("mid horizon fields not filled: row=%v", row)
	}
	if row["status"] != "validated_40d" {
		t.Fatalf("status = %q, want validated_40d", row["status"])
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

func forwardResult(h strategy.Horizon, code string) signal.SignalResult {
	return signal.SignalResult{
		Horizon:     h,
		Code:        code,
		Name:        code,
		Date:        "20260101",
		BuyCount:    3,
		TotalScore:  3,
		Confidence:  80,
		PositionPct: 5,
	}
}

func forwardBarDateOffset(offset int, close float64) data.DailyBar {
	day := 1 + offset
	date := "202601" + twoDigit(day)
	return forwardBar(date, close, close+0.2, close-0.2, close)
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
