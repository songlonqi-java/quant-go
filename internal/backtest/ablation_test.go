package backtest

import "testing"

func TestCompareAblationReportsYearlyStabilityAndTurnover(t *testing.T) {
	baseline := &Result{
		FinalEquity: 110,
		EquityCurve: []EquityPoint{
			{Date: "20250102", Value: 100}, {Date: "20251231", Value: 105},
			{Date: "20260102", Value: 104}, {Date: "20261231", Value: 110},
		},
		Trades: []Trade{
			{Date: "20250102", Action: "BUY", Price: 10, Shares: 5},
			{Date: "20251231", Action: "SELL", Price: 11, Shares: 5, HasReturn: true, ReturnPct: 10},
		},
	}
	variant := &Result{
		FinalEquity: 115,
		EquityCurve: []EquityPoint{
			{Date: "20250102", Value: 100}, {Date: "20251231", Value: 108},
			{Date: "20260102", Value: 107}, {Date: "20261231", Value: 115},
		},
		Trades: []Trade{
			{Date: "20250102", Action: "BUY", Price: 10, Shares: 10},
			{Date: "20251231", Action: "SELL", Price: 11, Shares: 10, HasReturn: true, ReturnPct: 10},
		},
	}

	comparison := CompareAblation(baseline, variant, 100, 0, 252)
	if comparison.VariantMetrics.TotalReturn <= comparison.BaselineMetrics.TotalReturn {
		t.Fatalf("metrics = %#v", comparison)
	}
	if comparison.ComparablePeriods != 2 || comparison.PositivePeriods != 2 {
		t.Fatalf("period stability = %d/%d, want 2/2", comparison.PositivePeriods, comparison.ComparablePeriods)
	}
	if comparison.VariantTurnover <= comparison.BaselineTurnover || len(comparison.VariantPeriods) != 2 {
		t.Fatalf("turnover/periods = %.2f/%.2f %#v", comparison.BaselineTurnover, comparison.VariantTurnover, comparison.VariantPeriods)
	}
}
