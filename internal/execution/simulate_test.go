package execution

import (
	"testing"

	"quant/internal/data"
	"quant/internal/strategy"
)

func TestSimulateManagedExitDelaysTimeStopThroughLimitDown(t *testing.T) {
	dates := []string{"20260101", "20260102", "20260103", "20260104", "20260105", "20260106", "20260107", "20260108"}
	bars := make([]data.DailyBar, len(dates))
	for i, date := range dates {
		bars[i] = managedBar(date, 10)
	}
	// Signal on day 1, enter day 2, and trigger the five-day time stop at
	// day 6 close. Day 7 is limit-down, so execution waits for day 8.
	bars[6].Open = 9
	bars[6].RawOpen = 9
	bars[6].Close = 9
	bars[6].RawClose = 9
	bars[6].DownLimit = 9

	outcome := SimulateManagedExit(bars, dates, "20260101", strategy.HorizonShort, SimulationOptions{MaxEntryGapPct: 3})
	if !outcome.Completed || outcome.Reason != ExitReasonTimeStop || outcome.TriggerDate != "20260106" || outcome.ExitDate != "20260108" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if outcome.DelayDays != 1 || outcome.HoldingDays != 5 {
		t.Fatalf("delay/holding = %d/%d, want 1/5", outcome.DelayDays, outcome.HoldingDays)
	}
}

func TestSimulateManagedExitRequiresExactNextMarketSessionEntry(t *testing.T) {
	dates := []string{"20260101", "20260102", "20260103", "20260104", "20260105", "20260106", "20260107"}
	bars := []data.DailyBar{
		managedBar("20260101", 10),
		// Suspended on the next market session; a day order must not drift to day 3.
		managedBar("20260103", 10), managedBar("20260104", 10), managedBar("20260105", 10),
		managedBar("20260106", 10), managedBar("20260107", 10),
	}
	outcome := SimulateManagedExit(bars, dates, "20260101", strategy.HorizonShort, SimulationOptions{MaxEntryGapPct: 3})
	if outcome.EntryFeasible {
		t.Fatalf("outcome = %+v, suspended next-session buy should expire", outcome)
	}
}

func managedBar(date string, price float64) data.DailyBar {
	return data.DailyBar{
		TsCode: "000001.SZ", TradeDate: date,
		Open: price, High: price + 0.1, Low: price - 0.1, Close: price,
		RawOpen: price, RawHigh: price + 0.1, RawLow: price - 0.1, RawClose: price,
		AdjFactor: 1, Vol: 1000,
	}
}
