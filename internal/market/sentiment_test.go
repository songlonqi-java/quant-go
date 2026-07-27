package market

import (
	"testing"

	"quant/internal/data"
)

func TestAnalyzeTradingStatsCapturesAShareRisk(t *testing.T) {
	codeMap := make(map[string][]data.DailyBar)
	for i := 0; i < 20; i++ {
		code := "000001.SZ"
		if i < 10 {
			code = "00000" + string(rune('0'+i)) + ".SZ"
		} else {
			code = "0000" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + ".SZ"
		}
		prev := marketBar(code, "20260101", 10, 10, 10, 10)
		cur := marketBar(code, "20260102", 10, 10.2, 8.9, 9.1)
		cur.DownLimit = 9.1
		if i < 3 {
			cur = marketBar(code, "20260102", 10, 11.2, 9.8, 11)
			cur.UpLimit = 11
		}
		codeMap[code] = []data.DailyBar{prev, cur}
	}

	ms := &MarketStatus{}
	ms.analyzeTradingStats(codeMap)

	if ms.RisingCount != 3 || ms.FallingCount != 17 {
		t.Fatalf("涨跌家数 = %d/%d, want 3/17", ms.RisingCount, ms.FallingCount)
	}
	if ms.LimitUpCount != 3 || ms.LimitDownCount != 17 {
		t.Fatalf("涨跌停家数 = %d/%d, want 3/17", ms.LimitUpCount, ms.LimitDownCount)
	}
	if ms.ProfitEffect != 15 {
		t.Fatalf("ProfitEffect = %.0f, want 15", ms.ProfitEffect)
	}
	if !contains(ms.RiskFlags, "亏钱效应") || !contains(ms.RiskFlags, "跌停扩散") {
		t.Fatalf("RiskFlags = %v, want 亏钱效应 and 跌停扩散", ms.RiskFlags)
	}
}

func marketBar(code, date string, open, high, low, close float64) data.DailyBar {
	return data.DailyBar{
		TsCode:    code,
		TradeDate: date,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Vol:       1000,
		Amount:    close * 1000,
		RawOpen:   open,
		RawHigh:   high,
		RawLow:    low,
		RawClose:  close,
		AdjFactor: 1,
	}
}
