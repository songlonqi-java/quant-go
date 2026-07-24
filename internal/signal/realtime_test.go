package signal

import (
	"testing"

	"quant/internal/data"
	"quant/internal/realtime"
)

func TestApplyRealtimeQuotesAddsIntradayLabels(t *testing.T) {
	results := []SignalResult{
		{
			Code:       "600000.SH",
			Date:       "20260723",
			Close:      10.00,
			BuyCount:   2,
			TotalScore: 2,
		},
	}
	quotes := map[string]realtime.Quote{
		"600000.SH": {
			Code:      "600000.SH",
			Open:      10.50,
			PrevClose: 10.00,
			Current:   11.00,
			ChangePct: 10.02,
			UpdateAt:  "2026-07-24 10:30:00",
		},
	}
	limits := data.NewStkLimitStore([]data.StkLimit{
		{TsCode: "600000.SH", TradeDate: "20260724", UpLimit: 11.00, DownLimit: 9.00},
	})

	ApplyRealtimeQuotes(results, quotes, limits)

	if !results[0].HasRealtime {
		t.Fatal("HasRealtime = false, want true")
	}
	if results[0].RealtimePrice != 11.00 {
		t.Fatalf("RealtimePrice = %.2f, want 11.00", results[0].RealtimePrice)
	}
	if !containsString(results[0].IntradayLabels, "涨停风险") {
		t.Fatalf("IntradayLabels = %v, want 涨停风险", results[0].IntradayLabels)
	}
	if !containsString(results[0].IntradayLabels, "高开>3%") {
		t.Fatalf("IntradayLabels = %v, want 高开>3%%", results[0].IntradayLabels)
	}
}
