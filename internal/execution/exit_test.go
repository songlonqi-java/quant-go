package execution

import (
	"testing"

	"quant/internal/data"
	"quant/internal/strategy"
)

func TestExitStateInitialStopAndTimeStop(t *testing.T) {
	stop := NewExitState(strategy.HorizonShort, "20260101", 10)
	if _, triggered := stop.ObserveSession("20260101", 10, 0, true); triggered {
		t.Fatal("entry day unexpectedly triggered")
	}
	trigger, triggered := stop.ObserveSession("20260102", 9.4, 0, true)
	if !triggered || trigger.Reason != ExitReasonInitialStop || trigger.HoldingDays != 2 {
		t.Fatalf("initial stop trigger = %+v/%v", trigger, triggered)
	}

	timeStop := NewExitState(strategy.HorizonShort, "20260101", 10)
	for day := 1; day < 5; day++ {
		if _, triggered := timeStop.ObserveSession("day", 10, 0, day != 2); triggered {
			t.Fatalf("time stop triggered on day %d", day)
		}
	}
	trigger, triggered = timeStop.ObserveSession("day5", 10, 0, false)
	if !triggered || trigger.Reason != ExitReasonTimeStop || trigger.HoldingDays != 5 {
		t.Fatalf("time stop trigger = %+v/%v", trigger, triggered)
	}
}

func TestExitStateATRTrailingStopOnlyTightens(t *testing.T) {
	state := NewExitState(strategy.HorizonShort, "20260101", 10)
	if _, triggered := state.ObserveSession("20260101", 12, 0.5, true); triggered {
		t.Fatal("strong close unexpectedly triggered")
	}
	if state.TrailingStop != 11 {
		t.Fatalf("trailing stop = %.2f, want 11", state.TrailingStop)
	}
	if _, triggered := state.ObserveSession("20260102", 12.2, 1, true); triggered {
		t.Fatal("higher close unexpectedly triggered")
	}
	if state.TrailingStop != 11 {
		t.Fatalf("trailing stop loosened to %.2f", state.TrailingStop)
	}
	trigger, triggered := state.ObserveSession("20260103", 10.9, 0.5, true)
	if !triggered || trigger.Reason != ExitReasonATRTrailing {
		t.Fatalf("trailing trigger = %+v/%v", trigger, triggered)
	}
}

func TestATRUsesTradePrices(t *testing.T) {
	bars := make([]data.DailyBar, 3)
	for i := range bars {
		bars[i] = data.DailyBar{High: 100, Low: 90, Close: 95, RawHigh: 11, RawLow: 9, RawClose: 10, AdjFactor: 1}
	}
	if got := ATR(bars, 2, 2); got != 2 {
		t.Fatalf("ATR = %.2f, want raw-price ATR 2", got)
	}
}
