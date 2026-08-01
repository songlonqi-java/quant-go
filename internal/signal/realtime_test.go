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

func TestRealtimeLimitStatusTrustsExactPriceBeforeFallback(t *testing.T) {
	quote := realtime.Quote{Code: "300001.SZ", PrevClose: 10, Current: 11, UpdateAt: "2026-07-24 10:30:00"}
	limits := data.NewStkLimitStore([]data.StkLimit{{TsCode: "300001.SZ", TradeDate: "20260724", UpLimit: 12, DownLimit: 8}})
	up, down := realtimeLimitStatus(quote, limits)
	if up || down {
		t.Fatalf("exact limit status = %v/%v, want tradable", up, down)
	}

	quote.Current = 11.96
	up, down = realtimeLimitStatus(quote, nil)
	if !up || down {
		t.Fatalf("board fallback status = %v/%v, want limit-up", up, down)
	}
}

func TestSellAtLimitUpIsNotMarkedAsBuyExecutionRisk(t *testing.T) {
	result := SignalResult{Code: "600000.SH", SellCount: 2, TotalScore: -2}
	quote := realtime.Quote{Code: result.Code, PrevClose: 10, Open: 10.5, Current: 11, ChangePct: 10, UpdateAt: "2026-07-24 10:30:00"}
	limits := data.NewStkLimitStore([]data.StkLimit{{TsCode: result.Code, TradeDate: "20260724", UpLimit: 11, DownLimit: 9}})

	labels := intradayLabels(result, quote, limits)
	if containsString(labels, "涨停风险") || containsString(labels, "高开>3%") {
		t.Fatalf("sell labels = %v, should not reuse buy-side execution risks", labels)
	}
	if !containsString(labels, "卖出缓和") {
		t.Fatalf("sell labels = %v, want 卖出缓和", labels)
	}
}
