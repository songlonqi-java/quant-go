package execution

import (
	"fmt"
	"math"
)

const CostModelVersion = "round_trip_v1"

// CostModel uses decimal rates: 0.0003 means 0.03%. It intentionally matches
// the historical-validation convention of charging commission and adverse
// slippage on both entry and exit.
type CostModel struct {
	Commission float64
	Slippage   float64
}

type ReturnBreakdown struct {
	GrossReturnPct float64
	CostImpactPct  float64
	NetReturnPct   float64
	EntryCost      float64
	ExitValue      float64
}

func (m CostModel) Validate() error {
	if math.IsNaN(m.Commission) || math.IsInf(m.Commission, 0) || m.Commission < 0 || m.Commission >= 1 {
		return fmt.Errorf("手续费率必须是大于等于0且小于1的有限数")
	}
	if math.IsNaN(m.Slippage) || math.IsInf(m.Slippage, 0) || m.Slippage < 0 || m.Slippage >= 1 {
		return fmt.Errorf("滑点率必须是大于等于0且小于1的有限数")
	}
	return nil
}

// RoundTripReturn calculates a long trade entered at the supplied raw entry
// price and marked/exited at the supplied raw exit price.
func RoundTripReturn(entry, exit float64, model CostModel) (ReturnBreakdown, bool) {
	if entry <= 0 || exit <= 0 || model.Validate() != nil {
		return ReturnBreakdown{}, false
	}
	entryCost := entry * (1 + model.Slippage) * (1 + model.Commission)
	exitValue := exit * (1 - model.Slippage) * (1 - model.Commission)
	if entryCost <= 0 || exitValue < 0 {
		return ReturnBreakdown{}, false
	}
	gross := (exit/entry - 1) * 100
	net := (exitValue/entryCost - 1) * 100
	return ReturnBreakdown{
		GrossReturnPct: gross,
		CostImpactPct:  gross - net,
		NetReturnPct:   net,
		EntryCost:      entryCost,
		ExitValue:      exitValue,
	}, true
}
