package data

import "testing"

func TestApplyAdjFactorsMatchesByCodeAndDate(t *testing.T) {
	bars := []DailyBar{
		{TsCode: "000001.SZ", TradeDate: "20260101", Open: 10, High: 11, Low: 9, Close: 10},
		{TsCode: "000001.SZ", TradeDate: "20260102", Open: 10, High: 11, Low: 9, Close: 10},
	}
	factors := []AdjFactor{
		{TsCode: "000001.SZ", TradeDate: "20260101", AdjFactor: 2},
		{TsCode: "000001.SZ", TradeDate: "20260102", AdjFactor: 3},
	}

	got := ApplyAdjFactors(bars, factors)

	if got[0].Close != 20 || got[1].Close != 30 {
		t.Fatalf("adjusted closes = %.2f, %.2f; want 20, 30", got[0].Close, got[1].Close)
	}
	if got[0].RawClose != 10 || got[1].RawClose != 10 {
		t.Fatalf("raw closes = %.2f, %.2f; want preserved raw 10, 10", got[0].RawClose, got[1].RawClose)
	}
	if got[0].AdjFactor != 2 || got[1].AdjFactor != 3 {
		t.Fatalf("adj factors = %.2f, %.2f; want 2, 3", got[0].AdjFactor, got[1].AdjFactor)
	}
}
