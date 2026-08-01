package signal

import (
	"math"
	"strings"
	"testing"

	"quant/internal/market"
	"quant/internal/strategy"
)

func TestRiskPolicyClassifiesExecutionSemantics(t *testing.T) {
	result := SignalResult{
		RiskLabels:     []string{"5日涨幅过高", "20日涨幅过高", "板块资金背离", "板块退潮", "市场偏弱"},
		IntradayLabels: []string{"高开>3%"},
	}
	effects := AssessRiskPolicy(result)
	want := map[string]RiskClass{
		"5日涨幅过高":  RiskClassHardFilter,
		"20日涨幅过高": RiskClassPenalty,
		"板块资金背离":  RiskClassPenalty,
		"板块退潮":    RiskClassHardFilter,
		"市场偏弱":    RiskClassNotice,
		"高开>3%":   RiskClassHardFilter,
	}
	if len(effects) != len(want) {
		t.Fatalf("effects = %#v, want %d entries", effects, len(want))
	}
	for _, effect := range effects {
		if effect.Class != want[effect.Label] {
			t.Fatalf("effect %s class = %s, want %s", effect.Label, effect.Class, want[effect.Label])
		}
	}
}

func TestApplyRiskPolicyStacksPenaltyWithoutCompoundingOnRepeat(t *testing.T) {
	results := []SignalResult{{
		Horizon: strategy.HorizonShort, BuyCount: 3, TotalScore: 3,
		Confidence: 90, PositionPct: 10,
		RiskLabels: []string{"资金分歧"},
	}}

	ApplyRiskPolicy(results)
	if results[0].Confidence != 86 {
		t.Fatalf("confidence = %.1f, want 86", results[0].Confidence)
	}
	if math.Abs(results[0].PositionPct-8.5) > 1e-9 {
		t.Fatalf("position = %.3f, want 8.5", results[0].PositionPct)
	}
	results[0].RiskLabels = append(results[0].RiskLabels, "板块资金背离")
	ApplyRiskPolicy(results)
	if results[0].Confidence != 80 || math.Abs(results[0].PositionPct-6.375) > 1e-9 {
		t.Fatalf("updated policy did not recompute from base: confidence %.1f position %.3f", results[0].Confidence, results[0].PositionPct)
	}
	ApplyRiskPolicy(results)
	if results[0].Confidence != 80 || math.Abs(results[0].PositionPct-6.375) > 1e-9 {
		t.Fatalf("repeated policy compounded result: confidence %.1f position %.3f", results[0].Confidence, results[0].PositionPct)
	}
	if len(results[0].Reasons) != 1 || !strings.Contains(results[0].Reasons[0], "资金分歧") || !strings.Contains(results[0].Reasons[0], "板块资金背离") {
		t.Fatalf("reasons = %#v, want one auditable penalty reason", results[0].Reasons)
	}
}

func TestApplyPositionPolicyHardFiltersOverheatAndSectorRetreat(t *testing.T) {
	for _, label := range []string{"5日涨幅过高", "板块退潮"} {
		t.Run(label, func(t *testing.T) {
			results := []SignalResult{{
				Horizon: strategy.HorizonShort, BuyCount: 3, TotalScore: 3,
				Confidence: 90, PositionPct: 8, RiskLabels: []string{label},
			}}
			decision := ApplyPositionPolicy(results, &market.MarketStatus{Sentiment: "偏多"})
			if decision.QualifiedBuys != 0 || !results[0].Suppressed {
				t.Fatalf("decision/result = %#v / %#v, want hard-filtered", decision, results[0])
			}
			if !strings.Contains(results[0].SuppressionReason, label) {
				t.Fatalf("suppression reason = %q, want %s", results[0].SuppressionReason, label)
			}
		})
	}
}

func TestRiskPenaltyCanCrossQualificationThreshold(t *testing.T) {
	results := []SignalResult{{
		Horizon: strategy.HorizonShort, BuyCount: 3, TotalScore: 3,
		Confidence: 73, PositionPct: 8, RiskLabels: []string{"20日涨幅过高"},
	}}
	decision := ApplyPositionPolicy(results, &market.MarketStatus{Sentiment: "偏多"})
	if results[0].Confidence != 67 {
		t.Fatalf("confidence = %.1f, want 67", results[0].Confidence)
	}
	if decision.QualifiedBuys != 0 || !strings.Contains(results[0].SuppressionReason, "置信度不足") {
		t.Fatalf("decision/reason = %#v / %q, want penalty-driven rejection", decision, results[0].SuppressionReason)
	}
}

func TestNoticeDoesNotChangeQualificationOrSizing(t *testing.T) {
	results := []SignalResult{{
		Horizon: strategy.HorizonShort, BuyCount: 3, TotalScore: 3,
		Confidence: 80, PositionPct: 8, RiskLabels: []string{"市场偏弱", "资金确认"},
	}}
	ApplyRiskPolicy(results)
	if results[0].Confidence != 80 || results[0].PositionPct != 8 {
		t.Fatalf("notice changed confidence/position: %#v", results[0])
	}
	if issues := qualifyingBuyIssues(results[0]); len(issues) != 0 {
		t.Fatalf("notice created qualification issues: %v", issues)
	}
}
