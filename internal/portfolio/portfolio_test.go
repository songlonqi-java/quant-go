package portfolio

import (
	"testing"

	"quant/internal/data"
	"quant/internal/realtime"
)

func TestAnalyzeUsesTradeCloseForCurrentHoldings(t *testing.T) {
	ledger := &Ledger{
		Transactions: []Transaction{
			{Date: "20260723", Code: "000001.SZ", Action: "buy", Shares: 100, Price: 10.00},
		},
	}
	barsMap := map[string][]data.DailyBar{
		"000001.SZ": {
			{TsCode: "000001.SZ", TradeDate: "20260723", Close: 133.10, RawClose: 10.00, AdjFactor: 13.31},
		},
	}

	summary := Analyze(ledger, barsMap, nil)

	if len(summary.Holdings) != 1 {
		t.Fatalf("len(Holdings) = %d, want 1", len(summary.Holdings))
	}
	if summary.Holdings[0].LastPrice != 10.00 {
		t.Fatalf("LastPrice = %.2f, want raw trade close 10.00", summary.Holdings[0].LastPrice)
	}
}

func TestApplyRealtimeQuotesUpdatesCurrentHoldings(t *testing.T) {
	summary := &Summary{
		Holdings: []PositionStatus{
			{Code: "000001.SZ", Shares: 100, Cost: 10.00, LastPrice: 10.00},
		},
	}
	quotes := map[string]realtime.Quote{
		"000001.SZ": {Code: "000001.SZ", Current: 10.50},
	}

	ApplyRealtimeQuotes(summary, quotes)

	holding := summary.Holdings[0]
	if holding.LastPrice != 10.50 {
		t.Fatalf("LastPrice = %.2f, want realtime 10.50", holding.LastPrice)
	}
	if holding.MarketVal != 1050 {
		t.Fatalf("MarketVal = %.2f, want 1050", holding.MarketVal)
	}
	if holding.PnL != 50 {
		t.Fatalf("PnL = %.2f, want 50", holding.PnL)
	}
}
