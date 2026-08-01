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
		"limit_up", "bottom_reversal", "relative_strength", "atr_breakout", "trend_pullback",
	}
	got := resolveBacktestStrategyNames(nil, configured)
	if len(got) != 17 {
		t.Fatalf("default backtest strategies = %d, want 17: %v", len(got), got)
	}
	excluded := map[string]bool{
		"value_ma60": true, "etf_rotation": true, "dividend_deviation": true, "bottom_reversal": true,
	}
	for _, name := range got {
		if excluded[name] {
			t.Fatalf("slow strategy %s leaked into default backtest: %v", name, got)
		}
	}
}

func TestResolveBacktestStrategyNamesPreservesExplicitResearchSet(t *testing.T) {
	requested := []string{"market_neutral_momentum", "value_ma60"}
	got := resolveBacktestStrategyNames(requested, []string{"macd"})
	if !reflect.DeepEqual(got, requested) {
		t.Fatalf("explicit strategies = %v, want %v", got, requested)
	}
	got[0] = "changed"
	if requested[0] != "market_neutral_momentum" {
		t.Fatal("resolved strategy names alias caller slice")
	}
}
