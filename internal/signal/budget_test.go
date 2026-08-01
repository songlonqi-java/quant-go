package signal

import (
	"testing"

	"quant/internal/strategy"
)

func TestApplyPortfolioBudgetDeductsExistingAndEnforcesAllCaps(t *testing.T) {
	results := []SignalResult{
		{Horizon: strategy.HorizonShort, Code: "000001.SZ", SectorName: "银行", BuyCount: 3, TotalScore: 3, Confidence: 90, PositionPct: 8},
		{Horizon: strategy.HorizonMid, Code: "000002.SZ", SectorName: "银行", BuyCount: 3, TotalScore: 2, Confidence: 80, PositionPct: 8},
		{Horizon: strategy.HorizonShort, Code: "000003.SZ", SectorName: "科技", BuyCount: 3, TotalScore: 1, Confidence: 70, PositionPct: 8},
	}
	allocation := ApplyPortfolioBudget(results, PortfolioBudget{
		MaxTotalPct:       30,
		MaxSinglePct:      10,
		MaxSectorPct:      15,
		ExistingTotalPct:  20,
		ExistingCodePct:   map[string]float64{"000001.SZ": 5},
		ExistingSectorPct: map[string]float64{"银行": 10},
	})

	// The first candidate consumes the remaining bank-sector capacity. The
	// second is therefore filtered despite total account capacity remaining.
	if results[0].PositionPct != 5 || !results[1].Suppressed || results[1].PositionPct != 0 {
		t.Fatalf("bank allocations = %.1f/%+.1f suppressed=%v", results[0].PositionPct, results[1].PositionPct, results[1].Suppressed)
	}
	if results[2].PositionPct != 5 {
		t.Fatalf("technology allocation = %.1f, want remaining total budget 5", results[2].PositionPct)
	}
	if !containsString(results[0].RiskLabels, "组合预算收缩") || !containsString(results[1].RiskLabels, "组合预算过滤") {
		t.Fatalf("risk labels = %v / %v", results[0].RiskLabels, results[1].RiskLabels)
	}
	if allocation.AllocatedBuys != 2 || allocation.FilteredBuys != 1 || allocation.AllocatedPct != 10 {
		t.Fatalf("allocation = %+v", allocation)
	}
}

func TestDeployablePositionCapHonorsMarketAction(t *testing.T) {
	if got := DeployablePositionCap(PositionDecision{Action: PositionActionProbe}, 70); got != 10 {
		t.Fatalf("probe cap = %.1f, want 10", got)
	}
	if got := DeployablePositionCap(PositionDecision{Action: PositionActionCash}, 70); got != 0 {
		t.Fatalf("cash cap = %.1f, want 0", got)
	}
	if got := DeployablePositionCap(PositionDecision{Action: PositionActionActive}, 70); got != 70 {
		t.Fatalf("active cap = %.1f, want 70", got)
	}
}

func TestReconcilePositionDecisionReportsFullBudget(t *testing.T) {
	decision := PositionDecision{Action: PositionActionActive, CandidateBuys: 2, QualifiedBuys: 2}
	ReconcilePositionDecision(&decision, PortfolioAllocation{PotentialBuys: 2, FilteredBuys: 2})
	if decision.Action != PositionActionWatch || decision.QualifiedBuys != 0 || decision.SuppressedBuys != 2 {
		t.Fatalf("decision = %+v", decision)
	}
}
