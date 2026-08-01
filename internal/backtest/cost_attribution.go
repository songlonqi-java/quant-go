package backtest

// CostAttribution decomposes the result along the exact executed trade path.
// GrossPnL adds recorded execution costs back to net P&L; it is an accounting
// attribution, not a counterfactual rerun with larger cost-free position sizes.
type CostAttribution struct {
	GrossPnLAmount   float64
	NetPnLAmount     float64
	CommissionAmount float64
	SlippageAmount   float64
	ImpactAmount     float64
	TotalCostAmount  float64
	GrossReturnPct   float64
	NetReturnPct     float64
	CostDragPct      float64
}

func CalculateCostAttribution(result *Result, initialCapital float64) CostAttribution {
	if result == nil {
		return CostAttribution{}
	}
	attribution := CostAttribution{NetPnLAmount: result.FinalEquity - initialCapital}
	for _, trade := range result.Trades {
		attribution.CommissionAmount += trade.CommissionAmount
		attribution.SlippageAmount += trade.SlippageAmount
		attribution.ImpactAmount += trade.ImpactAmount
	}
	attribution.TotalCostAmount = attribution.CommissionAmount + attribution.SlippageAmount + attribution.ImpactAmount
	attribution.GrossPnLAmount = attribution.NetPnLAmount + attribution.TotalCostAmount
	if initialCapital > 0 {
		attribution.GrossReturnPct = attribution.GrossPnLAmount / initialCapital * 100
		attribution.NetReturnPct = attribution.NetPnLAmount / initialCapital * 100
		attribution.CostDragPct = attribution.TotalCostAmount / initialCapital * 100
	}
	return attribution
}
