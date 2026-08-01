package signal

import (
	"math"
	"strings"
	"testing"

	"quant/internal/data"
	"quant/internal/market"
	"quant/internal/strategy"
)

func TestCorrelatedReversalSignalsDoNotCreateFormalConsensus(t *testing.T) {
	strategies := []strategy.Strategy{
		fixedVoteStrategy{name: "rsi", signal: strategy.Buy},
		fixedVoteStrategy{name: "kdj", signal: strategy.Buy},
		fixedVoteStrategy{name: "bollinger", signal: strategy.Buy},
	}
	results := GenerateWithContext(map[string][]data.DailyBar{"000001.SZ": voteBars()}, strategies, 0, nil, &market.MarketStatus{Sentiment: "偏多"})
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	got := results[0]
	if got.BuyCount != 3 || got.BuyGroupCount != 1 || got.EffectiveBuyVotes != 1.5 {
		t.Fatalf("vote metrics = raw %d, effective %.2f/%d groups", got.BuyCount, got.EffectiveBuyVotes, got.BuyGroupCount)
	}
	decision := ApplyPositionPolicy(results, &market.MarketStatus{Sentiment: "偏多"})
	if decision.QualifiedBuys != 0 || !results[0].Suppressed {
		t.Fatalf("decision/result = %+v / %+v, want correlated signals rejected", decision, results[0])
	}
	if !strings.Contains(results[0].SuppressionReason, "独立策略组不足") || !strings.Contains(results[0].SuppressionReason, "相关性调整后买入票数不足") {
		t.Fatalf("suppression reason = %q", results[0].SuppressionReason)
	}
}

func TestIndependentGroupsRetainShortTermQualification(t *testing.T) {
	strategies := []strategy.Strategy{
		fixedVoteStrategy{name: "rsi", signal: strategy.Buy},
		fixedVoteStrategy{name: "kdj", signal: strategy.Buy},
		fixedVoteStrategy{name: "sar", signal: strategy.Buy},
	}
	results := GenerateWithContext(map[string][]data.DailyBar{"000001.SZ": voteBars()}, strategies, 0, nil, &market.MarketStatus{Sentiment: "偏多"})
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	got := results[0]
	if got.BuyGroupCount != 2 || math.Abs(got.EffectiveBuyVotes-2.25) > 1e-9 {
		t.Fatalf("effective consensus = %.2f/%d groups, want 2.25/2", got.EffectiveBuyVotes, got.BuyGroupCount)
	}
	decision := ApplyPositionPolicy(results, &market.MarketStatus{Sentiment: "偏多"})
	if decision.QualifiedBuys != 1 || results[0].Suppressed || results[0].Confidence < 70 {
		t.Fatalf("decision/result = %+v / %+v, want independent consensus qualified", decision, results[0])
	}
}

func TestVoteMetricsAreExposedInStructuredOutput(t *testing.T) {
	result := SignalResult{
		BuyCount: 3, BuyGroupCount: 2, EffectiveBuyVotes: 2.25, VoteMetricsApplied: true,
	}
	output := toJSONOutput([]SignalResult{result}, false)
	if len(output) != 1 || !output[0].VoteMetricsApplied || output[0].BuyGroups != 2 || output[0].EffectiveBuyVotes != 2.25 {
		t.Fatalf("output = %+v", output)
	}
	if summary := result.SignalSummary(); !strings.Contains(summary, "有效2.25/2组") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestEffectiveVoteConfidenceRemainsDirectionSymmetric(t *testing.T) {
	buy := SignalResult{Strategies: map[string]SignalDetail{
		"rsi": {Signal: strategy.Buy}, "kdj": {Signal: strategy.Buy}, "sar": {Signal: strategy.Buy},
	}, BuyCount: 3, TotalScore: 2.6}
	sell := SignalResult{Strategies: map[string]SignalDetail{
		"rsi": {Signal: strategy.Sell}, "kdj": {Signal: strategy.Sell}, "sar": {Signal: strategy.Sell},
	}, SellCount: 3, TotalScore: -2.6}
	applyEffectiveVotes(&buy)
	applyEffectiveVotes(&sell)
	if buy.EffectiveBuyVotes != sell.EffectiveSellVotes || buy.BuyGroupCount != sell.SellGroupCount {
		t.Fatalf("buy/sell metrics = %+v / %+v", buy, sell)
	}
	if confidenceScore(buy) != confidenceScore(sell) {
		t.Fatalf("confidence = %.2f / %.2f", confidenceScore(buy), confidenceScore(sell))
	}
}

func TestHistoricalEvaluatorAppliesCorrelationAdjustment(t *testing.T) {
	evaluator := NewHistoricalEvaluator([]strategy.Strategy{
		fixedVoteStrategy{name: "rsi", signal: strategy.Buy},
		fixedVoteStrategy{name: "kdj", signal: strategy.Buy},
	})
	rows := evaluator.Evaluate(voteBars(), 1, "测试", &market.MarketStatus{Sentiment: "偏多"}, nil)
	if len(rows) != 1 || !rows[0].VoteMetricsApplied || rows[0].BuyGroupCount != 1 || rows[0].EffectiveBuyVotes != 1.25 {
		t.Fatalf("historical rows = %+v", rows)
	}
}

type fixedVoteStrategy struct {
	name   string
	signal strategy.SignalType
}

func (s fixedVoteStrategy) Name() string { return s.name }
func (s fixedVoteStrategy) Warmup() int  { return 0 }
func (s fixedVoteStrategy) Signal(_ []data.DailyBar, _ int) strategy.SignalType {
	return s.signal
}
func (s fixedVoteStrategy) Score(_ []data.DailyBar, _ int) float64 { return 10 }

func voteBars() []data.DailyBar {
	return []data.DailyBar{
		{TsCode: "000001.SZ", TradeDate: "20260101", Close: 10, RawClose: 10, AdjFactor: 1},
		{TsCode: "000001.SZ", TradeDate: "20260102", Close: 10.1, RawClose: 10.1, AdjFactor: 1},
	}
}
