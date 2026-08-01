package signal

import "testing"

func TestEvidenceFormattingDistinguishesQualificationAndPrior(t *testing.T) {
	result := SignalResult{HistoricalEvidence: &HistoricalEvidence{
		Available:    true,
		Basis:        "同策略组合 + 同市场状态",
		PriorBasis:   "同周期 + 同市场状态",
		PriorSamples: 12,
		PriorWeight:  12,
	}}
	if got := formatEvidenceBasis(result); got != "同策略组合 + 同市场状态" {
		t.Fatalf("basis = %q", got)
	}
	if got := formatEvidencePriorForResult(result); got != "同周期 + 同市场状态(12日,权重12)" {
		t.Fatalf("prior = %q", got)
	}

	result.HistoricalEvidence = &HistoricalEvidence{Available: false}
	if got := formatEvidenceBasis(result); got != "-" {
		t.Fatalf("unavailable basis = %q", got)
	}
	if got := formatEvidencePriorForResult(result); got != "-" {
		t.Fatalf("unavailable prior = %q", got)
	}
}
