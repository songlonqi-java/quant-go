package backtest

import (
	"math"
	"testing"
)

func TestCompareAblationReportsYearlyStabilityAndTurnover(t *testing.T) {
	baseline := &Result{
		FinalEquity: 110,
		EquityCurve: []EquityPoint{
			{Date: "20250102", Value: 100}, {Date: "20251231", Value: 105},
			{Date: "20260102", Value: 104}, {Date: "20261231", Value: 110},
		},
		Trades: []Trade{
			{Date: "20250102", Action: "BUY", Price: 10, Shares: 5, CommissionAmount: 0.1},
			{Date: "20251231", Action: "SELL", Price: 11, Shares: 5, HasReturn: true, ReturnPct: 10, SlippageAmount: 0.2},
		},
	}
	variant := &Result{
		FinalEquity: 115,
		EquityCurve: []EquityPoint{
			{Date: "20250102", Value: 100}, {Date: "20251231", Value: 108},
			{Date: "20260102", Value: 107}, {Date: "20261231", Value: 115},
		},
		Trades: []Trade{
			{Date: "20250102", Action: "BUY", Price: 10, Shares: 10, CommissionAmount: 0.2},
			{Date: "20251231", Action: "SELL", Price: 11, Shares: 10, HasReturn: true, ReturnPct: 10, SlippageAmount: 0.4},
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
	if math.Abs(comparison.BaselineCosts.TotalCostAmount-0.3) > 1e-9 || math.Abs(comparison.VariantCosts.TotalCostAmount-0.6) > 1e-9 {
		t.Fatalf("cost attribution = %+v/%+v", comparison.BaselineCosts, comparison.VariantCosts)
	}
	if comparison.Admission.Passed || len(comparison.Admission.Reasons) == 0 {
		t.Fatalf("two-year comparison should fail conservative admission: %#v", comparison.Admission)
	}
}

func TestAssessAblationAdmissionPassesStableProfitableImprovement(t *testing.T) {
	comparison := AblationComparison{
		BaselineMetrics:  PerformanceMetrics{AnnualizedReturn: 5, MaxDrawdown: 15},
		VariantMetrics:   PerformanceMetrics{AnnualizedReturn: 8, MaxDrawdown: 12},
		BaselineTurnover: 100, VariantTurnover: 104,
		ComparablePeriods: 6, PositivePeriods: 4,
	}
	admission := assessAblationAdmission(comparison)
	if !admission.Passed || len(admission.Reasons) != 0 {
		t.Fatalf("admission = %#v, want pass", admission)
	}
}

func TestAssessAblationAdmissionRejectsLosingPortfolioAndUnstableYears(t *testing.T) {
	comparison := AblationComparison{
		BaselineMetrics:  PerformanceMetrics{AnnualizedReturn: -16, MaxDrawdown: 64},
		VariantMetrics:   PerformanceMetrics{AnnualizedReturn: -15, MaxDrawdown: 63},
		BaselineTurnover: 100, VariantTurnover: 120,
		ComparablePeriods: 6, PositivePeriods: 3,
	}
	admission := assessAblationAdmission(comparison)
	if admission.Passed || len(admission.Reasons) != 3 {
		t.Fatalf("admission = %#v, want profitability, turnover and stability failures", admission)
	}
}
