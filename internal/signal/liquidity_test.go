package signal

import (
	"testing"

	"quant/internal/data"
	"quant/internal/execution"
	"quant/internal/strategy"
)

func TestApplyLiquidityPolicyAddsHardFiltersAndAuditMetrics(t *testing.T) {
	bars := []data.DailyBar{{
		TsCode: "000001.SZ", TradeDate: "20260110", Close: 10, RawClose: 10,
		Vol: 1000, Amount: 10_000,
	}}
	results := []SignalResult{{
		Code: "000001.SZ", Name: "ST测试", Date: "20260110", Horizon: strategy.HorizonShort,
		BuyCount: 3, EffectiveBuyVotes: 3, BuyGroupCount: 3, VoteMetricsApplied: true,
		TotalScore: 3, Confidence: 80, PositionPct: 10,
	}}
	ApplyLiquidityPolicy(results, map[string][]data.DailyBar{"000001.SZ": bars}, LiquidityContext{
		Policy:          execution.DefaultLiquidityPolicy(),
		StockInfos:      map[string]data.StockInfo{"000001.SZ": {TsCode: "000001.SZ", ListDate: "20251220"}},
		ReferenceEquity: 100_000, ApplyCurrentST: true,
	})
	got := results[0]
	if !got.LiquidityApplied || got.LiquidityEligible || got.AverageAmountCNY != 10_000_000 {
		t.Fatalf("liquidity result = %+v", got)
	}
	issues := qualifyingBuyIssues(got)
	for _, want := range []string{"ST股票", "上市时间不足", "成交额不足"} {
		if !containsIssue(issues, want) {
			t.Fatalf("missing %q in %v", want, issues)
		}
	}
}

func containsIssue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
