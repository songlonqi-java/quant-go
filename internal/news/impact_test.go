package news

import (
	"testing"

	"quant/internal/data"
)

func impactBars(code string) []data.DailyBar {
	bars := make([]data.DailyBar, 0, 12)
	price := 10.0
	for i := 0; i < 12; i++ {
		open := price * 1.002
		close := price * 1.01
		bars = append(bars, data.DailyBar{
			TsCode: code, TradeDate: "2026" + impactTwoDigits(i/30+1) + impactTwoDigits(i%30+1),
			Open: open, High: close * 1.01, Low: price * 0.995, Close: close,
			RawOpen: open, RawHigh: close * 1.01, RawLow: price * 0.995, RawClose: close,
			AdjFactor: 1, Vol: 10000,
		})
		price = close
	}
	return bars
}

func impactTwoDigits(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string([]byte{'0' + byte(value/10), '0' + byte(value%10)})
}

func TestBuildNewsImpactFillsReturnsFromNextSession(t *testing.T) {
	bars := impactBars("000001.SZ")
	bars2 := impactBars("000002.SZ")
	codeMap := map[string][]data.DailyBar{
		"000001.SZ": bars,
		"000002.SZ": bars2,
	}
	records := []NewsRecord{
		{
			CanonicalID: "a", Revision: 1, ID: "a1",
			Title: "测试股票一发布重大利好", Content: "测试股票一发布重大利好",
			PublishedAt: "2026-01-02T10:00:00+08:00", ReceivedAt: "2026-01-02T10:00:00+08:00",
			TimeConfidence: newsTimeObserved,
		},
	}
	report, err := BuildNewsImpact(records, codeMap, map[string]string{"000001.SZ": "测试股票一"}, 0.0003, 0.0001)
	if err != nil {
		t.Fatalf("BuildNewsImpact: %v", err)
	}
	if report.TotalArticles != 1 || report.EventDays != 1 {
		t.Fatalf("counts = %+v", report)
	}
	if report.ByMentions[0].Events != 1 {
		t.Fatalf("mention bucket events = %d, want 1", report.ByMentions[0].Events)
	}
	h1, ok := report.ByMentions[0].Horizons[1]
	if !ok || h1.Events != 1 {
		t.Fatalf("1-day horizon = %+v, want 1 event", h1)
	}
	if report.TopStocks[0].Code != "000001.SZ" || report.TopStocks[0].Mentions != 1 {
		t.Fatalf("top stock = %+v", report.TopStocks[0])
	}
}

func TestBuildNewsImpactSkipsLimitUpEntry(t *testing.T) {
	bars := impactBars("000001.SZ")
	// News arrives 2026-01-01, so entry happens on 2026-01-02 (bars[1]).
	// Make that open a limit-up so entry must be rejected.
	bars[1].Open = bars[1].Close * 1.10
	bars[1].RawOpen = bars[1].Open
	bars[1].UpLimit = bars[1].Close * 1.10
	codeMap := map[string][]data.DailyBar{"000001.SZ": bars}
	records := []NewsRecord{{
		CanonicalID: "b", Revision: 1, ID: "b1",
		Title: "测试股票一", Content: "测试股票一",
		PublishedAt: "2026-01-01T10:00:00+08:00", ReceivedAt: "2026-01-01T10:00:00+08:00",
		TimeConfidence: newsTimeObserved,
	}}
	report, err := BuildNewsImpact(records, codeMap, map[string]string{"000001.SZ": "测试股票一"}, 0.0003, 0.0001)
	if err != nil {
		t.Fatalf("BuildNewsImpact: %v", err)
	}
	if report.UnbuyableEvents != 1 {
		t.Fatalf("unbuyable = %d, want 1", report.UnbuyableEvents)
	}
}
