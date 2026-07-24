package data

import "testing"

func TestApplyStkLimitsCopiesLimitPrices(t *testing.T) {
	bars := []DailyBar{
		{TsCode: "000001.SZ", TradeDate: "20260101", Open: 10, Close: 10},
		{TsCode: "000002.SZ", TradeDate: "20260101", Open: 20, Close: 20},
	}
	store := NewStkLimitStore([]StkLimit{
		{TsCode: "000001.SZ", TradeDate: "20260101", UpLimit: 11, DownLimit: 9},
	})

	got := ApplyStkLimits(bars, store)

	if got[0].UpLimit != 11 || got[0].DownLimit != 9 {
		t.Fatalf("limit prices = %.2f/%.2f, want 11/9", got[0].UpLimit, got[0].DownLimit)
	}
	if got[1].UpLimit != 0 || got[1].DownLimit != 0 {
		t.Fatalf("unmatched limit prices = %.2f/%.2f, want 0/0", got[1].UpLimit, got[1].DownLimit)
	}
	if bars[0].UpLimit != 0 || bars[0].DownLimit != 0 {
		t.Fatalf("original bars mutated: %.2f/%.2f", bars[0].UpLimit, bars[0].DownLimit)
	}
}

func TestMoneyflowLargeNetAmount(t *testing.T) {
	flow := Moneyflow{
		BuyLgAmount:   300,
		SellLgAmount:  120,
		BuyElgAmount:  500,
		SellElgAmount: 80,
	}

	if got := flow.LargeNetAmount(); got != 600 {
		t.Fatalf("LargeNetAmount = %.0f, want 600", got)
	}
}
