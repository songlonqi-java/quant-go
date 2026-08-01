package backtest

import "testing"

func TestCalculateCostAttributionUsesExactLedgerAmounts(t *testing.T) {
	result := &Result{
		FinalEquity: 85,
		Trades: []Trade{
			{CommissionAmount: 0.4, SlippageAmount: 1.0, ImpactAmount: 0.6},
			{CommissionAmount: 0.6, SlippageAmount: 1.0, ImpactAmount: 2.4},
		},
	}
	got := CalculateCostAttribution(result, 100)
	if got.CommissionAmount != 1 || got.SlippageAmount != 2 || got.ImpactAmount != 3 || got.TotalCostAmount != 6 {
		t.Fatalf("cost components = %+v", got)
	}
	if got.NetPnLAmount != -15 || got.GrossPnLAmount != -9 || got.NetReturnPct != -15 || got.GrossReturnPct != -9 || got.CostDragPct != 6 {
		t.Fatalf("gross/net attribution = %+v", got)
	}
}
