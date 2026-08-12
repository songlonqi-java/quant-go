package signal

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"quant/internal/strategy"
)

// PortfolioBudget is expressed in percentage points of total account equity.
// Existing exposure is deducted before new recommendations are allocated.
type PortfolioBudget struct {
	MaxTotalPct       float64
	MaxSinglePct      float64
	MaxSectorPct      float64
	MaxBuysPerHorizon int
	// MaxSameSignaturePct caps the combined allocation of candidates that
	// fired the same strategy combo on the same day. They are one bet, not
	// several, so stacking three of them should not triple the exposure.
	MaxSameSignaturePct float64
	ExistingTotalPct    float64
	ExistingCodePct     map[string]float64
	ExistingSectorPct   map[string]float64
}

type PortfolioAllocation struct {
	PotentialBuys int
	AllocatedBuys int
	FilteredBuys  int
	AllocatedPct  float64
}

// DeployablePositionCap translates the market-level position decision into an
// account-wide ceiling. Probe mode deliberately remains small even if the
// configured normal-market ceiling is much larger.
func DeployablePositionCap(decision PositionDecision, configuredMax float64) float64 {
	if configuredMax < 0 {
		return 0
	}
	switch decision.Action {
	case PositionActionActive:
		return configuredMax
	case PositionActionProbe:
		return math.Min(configuredMax, 10)
	default:
		return 0
	}
}

// ApplyPortfolioBudget allocates eligible buys globally by aggregate score instead
// of independently per horizon. It mutates results in place so subsequent
// Top-N selection and reporting see the executable position size.
func ApplyPortfolioBudget(results []SignalResult, budget PortfolioBudget) PortfolioAllocation {
	allocation := PortfolioAllocation{}
	if len(results) == 0 {
		return allocation
	}
	codePct := cloneExposure(budget.ExistingCodePct)
	sectorPct := cloneExposure(budget.ExistingSectorPct)
	signaturePct := make(map[string]float64)
	totalPct := math.Max(0, budget.ExistingTotalPct)

	indices := make([]int, 0, len(results))
	for i := range results {
		if !results[i].Suppressed && rawRecommendation(results[i]) == "买入" {
			indices = append(indices, i)
		}
	}
	allocation.PotentialBuys = len(indices)
	sort.SliceStable(indices, func(i, j int) bool {
		left, right := results[indices[i]], results[indices[j]]
		if left.TotalScore != right.TotalScore {
			return left.TotalScore > right.TotalScore
		}
		if left.Confidence != right.Confidence {
			return left.Confidence > right.Confidence
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Horizon < right.Horizon
	})

	selectedByHorizon := make(map[string]int)
	for _, idx := range indices {
		r := &results[idx]
		horizon := string(r.Horizon)
		if budget.MaxBuysPerHorizon > 0 && selectedByHorizon[horizon] >= budget.MaxBuysPerHorizon {
			r.PositionPct = 0
			r.Suppressed = true
			r.SuppressionReason = "超出组合Top-N名额"
			addUnique(&r.RiskLabels, "组合名额过滤")
			allocation.FilteredBuys++
			continue
		}
		desired := math.Max(0, r.PositionPct)
		allocated := desired
		allocated = math.Min(allocated, math.Max(0, budget.MaxTotalPct-totalPct))
		allocated = math.Min(allocated, math.Max(0, budget.MaxSinglePct-codePct[r.Code]))
		if r.SectorName != "" {
			allocated = math.Min(allocated, math.Max(0, budget.MaxSectorPct-sectorPct[r.SectorName]))
		}
		if signature := buySignatureKey(*r); budget.MaxSameSignaturePct > 0 && signature != "" {
			allocated = math.Min(allocated, math.Max(0, budget.MaxSameSignaturePct-signaturePct[signature]))
		}

		if allocated <= 1e-9 {
			r.PositionPct = 0
			r.Suppressed = true
			r.SuppressionReason = "组合仓位预算不足"
			addUnique(&r.RiskLabels, "组合预算过滤")
			r.Reasons = append(r.Reasons, "组合预算: 已有持仓或更高优先级候选已占满限额")
			allocation.FilteredBuys++
			continue
		}
		if allocated+1e-9 < desired {
			r.PositionPct = allocated
			addUnique(&r.RiskLabels, "组合预算收缩")
			r.Reasons = append(r.Reasons, fmt.Sprintf("组合预算: 建议仓位由%.1f%%收缩至%.1f%%", desired, allocated))
		}
		totalPct += allocated
		codePct[r.Code] += allocated
		if r.SectorName != "" {
			sectorPct[r.SectorName] += allocated
		}
		if signature := buySignatureKey(*r); budget.MaxSameSignaturePct > 0 && signature != "" {
			signaturePct[signature] += allocated
		}
		selectedByHorizon[horizon]++
		allocation.AllocatedBuys++
		allocation.AllocatedPct += allocated
	}
	RefreshRiskPolicy(results)
	return allocation
}

// buySignatureKey joins the buy-side strategy names of a candidate into a
// stable key. Candidates sharing the key are the same strategy bet on the same
// day, so their allocations must share one concentration budget.
func buySignatureKey(r SignalResult) string {
	names := make([]string, 0, len(r.Strategies))
	for name, detail := range r.Strategies {
		if detail.Signal == strategy.Buy {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return strings.Join(names, "+")
}

// ReconcilePositionDecision makes the headline position decision describe the
// executable post-budget list rather than the pre-budget candidate count.
func ReconcilePositionDecision(decision *PositionDecision, allocation PortfolioAllocation) {
	if decision == nil || allocation.PotentialBuys == 0 {
		return
	}
	decision.QualifiedBuys = allocation.AllocatedBuys
	decision.SuppressedBuys = decision.CandidateBuys - decision.QualifiedBuys
	if decision.SuppressedBuys < 0 {
		decision.SuppressedBuys = 0
	}
	if allocation.AllocatedBuys == 0 {
		decision.Action = PositionActionWatch
		decision.Reasons = append(decision.Reasons, "组合总仓位、单票、行业或Top-N预算已满")
		decision.Advice = adviceForAction(decision.Action)
	} else if allocation.FilteredBuys > 0 {
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("%d个候选被组合预算过滤", allocation.FilteredBuys))
	}
}

func cloneExposure(source map[string]float64) map[string]float64 {
	cloned := make(map[string]float64, len(source))
	for key, value := range source {
		if value > 0 {
			cloned[key] = value
		}
	}
	return cloned
}
