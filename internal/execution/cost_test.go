package execution

import (
	"math"
	"testing"
)

func TestRoundTripReturnSeparatesGrossCostAndNet(t *testing.T) {
	model := CostModel{Commission: 0.001, Slippage: 0.002}
	result, ok := RoundTripReturn(100, 110, model)
	if !ok {
		t.Fatal("RoundTripReturn() = not ok")
	}
	wantEntry := 100 * 1.002 * 1.001
	wantExit := 110 * 0.998 * 0.999
	wantNet := (wantExit/wantEntry - 1) * 100
	if math.Abs(result.GrossReturnPct-10) > 1e-9 || math.Abs(result.NetReturnPct-wantNet) > 1e-9 {
		t.Fatalf("result = %+v, want gross 10 and net %.9f", result, wantNet)
	}
	if math.Abs(result.CostImpactPct-(result.GrossReturnPct-result.NetReturnPct)) > 1e-9 {
		t.Fatalf("cost impact = %.9f, want gross-net", result.CostImpactPct)
	}
}

func TestRoundTripReturnRejectsInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		entry float64
		exit  float64
		model CostModel
	}{
		{entry: 0, exit: 10},
		{entry: 10, exit: 0},
		{entry: 10, exit: 11, model: CostModel{Commission: -0.1}},
		{entry: 10, exit: 11, model: CostModel{Commission: 1}},
		{entry: 10, exit: 11, model: CostModel{Slippage: math.NaN()}},
	} {
		if _, ok := RoundTripReturn(test.entry, test.exit, test.model); ok {
			t.Fatalf("RoundTripReturn(%v, %v, %+v) unexpectedly succeeded", test.entry, test.exit, test.model)
		}
	}
}
