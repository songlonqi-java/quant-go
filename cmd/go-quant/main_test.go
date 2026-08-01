package main

import (
	"reflect"
	"testing"
)

func TestResolveBacktestStrategyNamesMatchesDailyDefaults(t *testing.T) {
	configured := []string{
		"ma_crossover", "macd", "rsi", "bollinger", "volume_breakout",
		"value_ma60", "etf_rotation", "dividend_deviation", "bull_flag",
		"kdj", "williams_r", "donchian", "mfi", "sar", "roc", "ma_sticky",
		"limit_up", "bottom_reversal", "relative_strength", "market_neutral_momentum",
		"atr_breakout", "trend_pullback", "quality_value", "earnings_growth",
	}
	got := resolveBacktestStrategyNames(nil, configured)
	if len(got) != 10 {
		t.Fatalf("default backtest strategies = %d, want 10: %v", len(got), got)
	}
	excluded := map[string]bool{
		"value_ma60": true, "etf_rotation": true, "dividend_deviation": true, "bull_flag": true,
		"williams_r": true, "donchian": true, "mfi": true, "roc": true, "ma_sticky": true,
		"limit_up": true, "bottom_reversal": true, "market_neutral_momentum": true,
		"quality_value": true, "earnings_growth": true,
	}
	for _, name := range got {
		if excluded[name] {
			t.Fatalf("retired/slow strategy %s leaked into default backtest: %v", name, got)
		}
	}
}

func TestResolveBacktestStrategyNamesPreservesExplicitResearchSet(t *testing.T) {
	requested := []string{"quality_value", "earnings_growth"}
	got := resolveBacktestStrategyNames(requested, []string{"macd"})
	if !reflect.DeepEqual(got, requested) {
		t.Fatalf("explicit strategies = %v, want %v", got, requested)
	}
	got[0] = "changed"
	if requested[0] != "quality_value" {
		t.Fatal("resolved strategy names alias caller slice")
	}
}
