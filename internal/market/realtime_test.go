package market

import (
	"math"
	"testing"

	"quant/internal/data"
	"quant/internal/realtime"
)

func TestAnalyzeIntradayUsesCoverageBreadthAndExactLimits(t *testing.T) {
	quotes := []realtime.Quote{
		{Code: "000001.SZ", PrevClose: 10, Current: 11, ChangePct: 10, UpdateAt: "2026-07-31 10:00:00"},
		{Code: "000002.SZ", PrevClose: 10, Current: 9, ChangePct: -10, UpdateAt: "2026-07-31 10:00:00"},
		{Code: "000003.SZ", PrevClose: 10, Current: 10, ChangePct: 0, UpdateAt: "2026-07-31 10:00:00"},
	}
	limits := data.NewStkLimitStore([]data.StkLimit{
		{TsCode: "000001.SZ", TradeDate: "20260731", UpLimit: 11, DownLimit: 9},
		{TsCode: "000002.SZ", TradeDate: "20260731", UpLimit: 11, DownLimit: 9},
	})

	status := AnalyzeIntraday(quotes, 4, limits)
	if status.Quoted != 3 || status.CoveragePct != 75 || status.Complete {
		t.Fatalf("coverage = %+v, want 3/4 and incomplete", status)
	}
	if status.RisingCount != 1 || status.FallingCount != 1 || status.FlatCount != 1 || math.Abs(status.ProfitEffect-100.0/3.0) > 0.0001 {
		t.Fatalf("breadth = %+v", status)
	}
	if status.LimitUpCount != 1 || status.LimitDownCount != 1 || status.LimitPriceCovered != 2 {
		t.Fatalf("limits = %+v", status)
	}
}

func TestSortedQuoteCodes(t *testing.T) {
	codes := SortedQuoteCodes(map[string][]data.DailyBar{
		"600000.SH": nil,
		"000001.SZ": nil,
	})
	if len(codes) != 2 || codes[0] != "000001.SZ" || codes[1] != "600000.SH" {
		t.Fatalf("codes = %v", codes)
	}
}
