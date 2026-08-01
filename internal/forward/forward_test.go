package forward

import (
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quant/internal/data"
	"quant/internal/execution"
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

	updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars}, execution.CostModel{})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if updated != 4 {
		t.Fatalf("updated = %d, want 4 including managed exit state", updated)
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

	updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars}, execution.CostModel{})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if updated != 2 {
		t.Fatalf("updated = %d, want 2 including managed no-trade state", updated)
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

func TestValidateWritesAndRecalculatesGrossCostAndNetReturns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, picksFile)
	if err := writeRows(path, []map[string]string{{
		"signal_date": "20260101", "target_date": "20260102", "horizon": string(strategy.HorizonShort),
		"rank": "1", "code": "000001.SZ", "name": "平安银行", "close": "10.00", "status": "pending",
	}}); err != nil {
		t.Fatal(err)
	}
	bars := []data.DailyBar{
		forwardBar("20260101", 10, 10.2, 9.8, 10),
		forwardBar("20260102", 10, 10.6, 9.9, 10.5),
		forwardBar("20260103", 10.5, 10.8, 10.4, 10.7),
		forwardBar("20260104", 10.7, 11.2, 10.6, 11),
		forwardBar("20260105", 11, 11.3, 10.9, 11.2),
		forwardBar("20260106", 11.2, 11.7, 11.1, 11.5),
	}
	zeroCost := execution.CostModel{}
	if updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars}, zeroCost); err != nil || updated != 4 {
		t.Fatalf("zero-cost Validate() = %d, %v, want 4, nil", updated, err)
	}
	rows, err := readRows(path)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["next_return_pct"] != rows[0]["next_net_return_pct"] || rows[0]["next_cost_pct"] != "0.00" {
		t.Fatalf("zero-cost row = %v, want gross equal net", rows[0])
	}

	costModel := execution.CostModel{Commission: 0.001, Slippage: 0.002}
	if updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars}, costModel); err != nil || updated != 3 {
		t.Fatalf("cost refresh Validate() = %d, %v, want 3, nil", updated, err)
	}
	rows, err = readRows(path)
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row["cost_model"] != execution.CostModelVersion || row["commission_rate"] != "0.00100000" || row["slippage_rate"] != "0.00200000" {
		t.Fatalf("cost metadata = %q/%q/%q", row["cost_model"], row["commission_rate"], row["slippage_rate"])
	}
	for _, fields := range [][4]string{
		{"next_return_pct", "next_cost_pct", "next_net_return_pct", "next_close"},
		{"day3_return_pct", "day3_cost_pct", "day3_net_return_pct", "day3_close"},
		{"day5_return_pct", "day5_cost_pct", "day5_net_return_pct", "day5_close"},
	} {
		gross := parseFloat(row[fields[0]])
		cost := parseFloat(row[fields[1]])
		net := parseFloat(row[fields[2]])
		if cost <= 0 || net >= gross || math.Abs((gross-net)-cost) > 0.02 {
			t.Fatalf("return fields %v = gross %.2f cost %.2f net %.2f", fields, gross, cost, net)
		}
	}
	if !strings.Contains(row["notes"], "交易成本口径已重算") {
		t.Fatalf("cost refresh note missing: %q", row["notes"])
	}
	if updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars}, costModel); err != nil || updated != 0 {
		t.Fatalf("unchanged cost Validate() = %d, %v, want 0, nil", updated, err)
	}
}

func TestValidateWithExecutionRejectsOversizedOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, picksFile)
	if err := writeRows(path, []map[string]string{{
		"signal_date": "20260101", "target_date": "20260102", "horizon": string(strategy.HorizonShort),
		"rank": "1", "code": "000001.SZ", "name": "平安银行", "position_pct": "10", "status": "pending",
	}}); err != nil {
		t.Fatal(err)
	}
	bars := []data.DailyBar{
		forwardBar("20260101", 10, 10.2, 9.8, 10),
		forwardBar("20260102", 10, 10.4, 9.9, 10.2),
	}
	for i := range bars {
		bars[i].Amount = 1_000 // 1 million CNY
	}
	policy := execution.LiquidityPolicy{Enabled: true, AmountLookback: 1, MaxParticipationPct: 5, ImpactCoefficient: 0.005, MaxImpactRate: 0.02}
	if _, err := ValidateWithExecution(dir, map[string][]data.DailyBar{"000001.SZ": bars}, execution.CostModel{}, policy, 1_000_000); err != nil {
		t.Fatal(err)
	}
	rows, err := readRows(path)
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row["status"] != "no_trade_liquidity" || row["next_net_return_pct"] != "" {
		t.Fatalf("oversized order was not rejected: %v", row)
	}
	if row["cost_model"] != execution.ImpactCostModelVersion || parseFloat(row["entry_participation_pct"]) != 10 {
		t.Fatalf("liquidity audit fields = %v", row)
	}
}

func TestValidateWithExecutionAddsImpactCost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, picksFile)
	if err := writeRows(path, []map[string]string{{
		"signal_date": "20260101", "target_date": "20260102", "horizon": string(strategy.HorizonShort),
		"rank": "1", "code": "000001.SZ", "name": "平安银行", "position_pct": "10", "status": "pending",
	}}); err != nil {
		t.Fatal(err)
	}
	bars := []data.DailyBar{
		forwardBar("20260101", 10, 10.2, 9.8, 10),
		forwardBar("20260102", 10, 10.6, 9.9, 10.5),
		forwardBar("20260103", 10.5, 10.8, 10.4, 10.7),
		forwardBar("20260104", 10.7, 11.2, 10.6, 11),
		forwardBar("20260105", 11, 11.3, 10.9, 11.2),
		forwardBar("20260106", 11.2, 11.7, 11.1, 11.5),
	}
	for i := range bars {
		bars[i].Amount = 1_000 // 1 million CNY
	}
	policy := execution.LiquidityPolicy{Enabled: true, AmountLookback: 1, MaxParticipationPct: 20, ImpactCoefficient: 0.02, MaxImpactRate: 0.05}
	if _, err := ValidateWithExecution(dir, map[string][]data.DailyBar{"000001.SZ": bars}, execution.CostModel{}, policy, 100_000); err != nil {
		t.Fatal(err)
	}
	rows, err := readRows(path)
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row["cost_model"] != execution.ImpactCostModelVersion || parseFloat(row["entry_impact_pct"]) <= 0 {
		t.Fatalf("impact metadata missing: %v", row)
	}
	if parseFloat(row["next_cost_pct"]) <= 0 || parseFloat(row["next_net_return_pct"]) >= parseFloat(row["next_return_pct"]) {
		t.Fatalf("impact was not included in net return: %v", row)
	}
}

func TestValidateMigratesPreCostRowAndKeepsIntradayBreakTrade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, picksFile)
	legacy := map[string]string{
		"signal_date": "20260101", "target_date": "20260102", "horizon": string(strategy.HorizonShort),
		"rank": "1", "code": "000001.SZ", "next_open": "10.00", "next_close": "10.50",
		"next_return_pct": "5.00", "status": "invalid_break_prev_low",
	}
	if err := writeCSVWithHeader(path, preCostHeaders, legacy); err != nil {
		t.Fatal(err)
	}
	bars := []data.DailyBar{
		forwardBar("20260101", 10, 10.2, 9.8, 10),
		forwardBar("20260102", 10, 10.6, 9.7, 10.5),
	}
	updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars}, execution.CostModel{Commission: 0.001, Slippage: 0.002})
	if err != nil || updated != 2 {
		t.Fatalf("Validate() = %d, %v, want 2, nil", updated, err)
	}
	canonical, err := hasCanonicalHeader(path)
	if err != nil || !canonical {
		t.Fatalf("canonical header = %v, %v", canonical, err)
	}
	rows, err := readRows(path)
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row["next_return_pct"] != "5.00" || row["next_net_return_pct"] == "" || row["status"] != "validated_1d" {
		t.Fatalf("migrated row = %v", row)
	}
	if !strings.Contains(row["notes"], "历史记录已迁移") {
		t.Fatalf("migration note missing: %q", row["notes"])
	}
}

func TestMigratePreservesPreVoteMultiHorizonRow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, picksFile)
	legacy := map[string]string{
		"signal_date": "20260723", "target_date": "20260724", "horizon": string(strategy.HorizonMid),
		"rank": "2", "code": "601061.SH", "confidence": "81", "position_pct": "4.5",
		"benchmark": "CSI300", "day20_return_pct": "3.25", "status": "validated_20d",
	}
	if err := writeCSVWithHeader(path, preVoteHeaders, legacy); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(dir); err != nil {
		t.Fatal(err)
	}
	rows, err := readRows(path)
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	for field, want := range map[string]string{
		"horizon": string(strategy.HorizonMid), "code": "601061.SH", "confidence": "81",
		"position_pct": "4.5", "benchmark": "CSI300", "day20_return_pct": "3.25", "status": "validated_20d",
	} {
		if row[field] != want {
			t.Fatalf("migrated %s = %q, want %q; row=%v", field, row[field], want, row)
		}
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

	updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": codeOne, "000002.SZ": codeTwo}, execution.CostModel{Commission: 0.001, Slippage: 0.002})
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
	if parseFloat(row["next_cost_pct"]) <= 0 || parseFloat(row["next_net_return_pct"]) >= parseFloat(row["next_return_pct"]) {
		t.Fatalf("cash benchmark cost/net returns not filled: row=%v", row)
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
	if rows[0]["buy_effective_votes"] != "3.00" || rows[0]["buy_groups"] != "3" {
		t.Fatalf("effective vote fields missing: row=%v", rows[0])
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

	updated, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars}, execution.CostModel{})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if updated != 5 {
		t.Fatalf("updated = %d, want 5 including managed exit state", updated)
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

func TestValidateRecordsManagedExitDelayAtLimitDown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, picksFile)
	if err := writeRows(path, []map[string]string{{
		"signal_date": "20260101", "target_date": "20260102", "horizon": string(strategy.HorizonShort),
		"rank": "1", "code": "000001.SZ", "status": "pending",
	}}); err != nil {
		t.Fatal(err)
	}
	bars := []data.DailyBar{
		forwardBar("20260101", 10, 10.1, 9.9, 10),
		forwardBar("20260102", 10, 10.1, 9.9, 10),
		forwardBar("20260103", 10, 10.1, 9.9, 10),
		forwardBar("20260104", 10, 10.1, 9.9, 10),
		forwardBar("20260105", 10, 10.1, 9.9, 10),
		forwardBar("20260106", 10, 10.1, 9.9, 10),
		forwardBar("20260107", 9, 9, 9, 9),
		forwardBar("20260108", 8.9, 9, 8.8, 8.9),
	}
	bars[6].DownLimit = 9

	if _, err := Validate(dir, map[string][]data.DailyBar{"000001.SZ": bars}, execution.CostModel{}); err != nil {
		t.Fatal(err)
	}
	rows, err := readRows(path)
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row["exit_model"] != execution.ExitModelVersion || row["managed_exit_status"] != "completed" || row["exit_reason"] != string(execution.ExitReasonTimeStop) {
		t.Fatalf("managed exit = %v", row)
	}
	if row["exit_trigger_date"] != "20260106" || row["exit_date"] != "20260108" || row["exit_delay_days"] != "1" {
		t.Fatalf("managed exit timing = %v", row)
	}
	if row["exit_net_return_pct"] == "" || parseFloat(row["exit_net_return_pct"]) >= 0 {
		t.Fatalf("managed exit return = %v", row)
	}
}

func TestManagedExitVersionChangeRebuildsStaleFields(t *testing.T) {
	row := map[string]string{
		"signal_date": "20260101", "horizon": string(strategy.HorizonShort),
		"exit_model": "legacy", "managed_exit_status": "completed", "exit_date": "19990101", "exit_reason": "legacy",
	}
	bars := make([]data.DailyBar, 8)
	dates := make([]string, 8)
	for i := range bars {
		dates[i] = "2026010" + string(rune('1'+i))
		bars[i] = forwardBar(dates[i], 10, 10.1, 9.9, 10)
	}
	updated, dirty := validateManagedExit(row, bars, dates, execution.CostModel{}, false)
	if updated != 1 || !dirty || row["exit_model"] != execution.ExitModelVersion {
		t.Fatalf("version refresh = updated=%d dirty=%v row=%v", updated, dirty, row)
	}
	if row["exit_date"] != "20260107" || row["exit_reason"] != string(execution.ExitReasonTimeStop) {
		t.Fatalf("stale exit fields not rebuilt: %v", row)
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
		Horizon:            h,
		Code:               code,
		Name:               code,
		Date:               "20260101",
		BuyCount:           3,
		BuyGroupCount:      3,
		EffectiveBuyVotes:  3,
		VoteMetricsApplied: true,
		TotalScore:         3,
		Confidence:         80,
		PositionPct:        5,
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

func writeCSVWithHeader(path string, header []string, row map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	values := make([]string, len(header))
	for i, field := range header {
		values[i] = row[field]
	}
	if err := w.Write(values); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}
