package backtest

import (
	"testing"

	"quant/internal/data"
	"quant/internal/strategy"
)

func TestRunPortfolioUsesSharedAccountAggregationAndNextOpen(t *testing.T) {
	bars := []data.DailyBar{
		portfolioBar("000001.SZ", "20260101", 10.0),
		portfolioBar("000001.SZ", "20260102", 10.1),
		portfolioBar("000001.SZ", "20260103", 10.2),
		portfolioBar("000001.SZ", "20260104", 10.4),
	}
	strategies := []strategy.Strategy{
		portfolioTestStrategy{name: "limit_up"},
		portfolioTestStrategy{name: "sar"},
		portfolioTestStrategy{name: "kdj"},
	}
	flows := data.NewMoneyflowStore([]data.Moneyflow{{
		TsCode: "000001.SZ", TradeDate: "20260101", NetMfAmount: 100,
		BuyLgAmount: 100, BuyElgAmount: 100,
	}})

	result, err := RunPortfolio(PortfolioOptions{
		CodeMap:     map[string][]data.DailyBar{"000001.SZ": bars},
		Strategies:  strategies,
		Moneyflows:  flows,
		TopN:        1,
		Config:      Config{InitialCapital: 100_000, LotSize: 100},
		MaxTotalPct: 70, MaxSinglePct: 15, MaxSectorPct: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 2 || result.Trades[0].Action != "BUY" || result.Trades[1].Action != "SELL" {
		t.Fatalf("trades = %+v, want next-open buy and sell", result.Trades)
	}
	if result.Trades[0].SignalDate != "20260101" || result.Trades[0].Date != "20260102" {
		t.Fatalf("buy = %+v", result.Trades[0])
	}
	if result.Trades[1].SignalDate != "20260103" || result.Trades[1].Date != "20260104" {
		t.Fatalf("sell = %+v", result.Trades[1])
	}
	if result.FinalEquity <= 100_000 {
		t.Fatalf("FinalEquity = %.2f, want profitable replay", result.FinalEquity)
	}
}

func TestRunPortfolioKeepsPreStartBarsForWarmup(t *testing.T) {
	bars := []data.DailyBar{
		portfolioBar("000001.SZ", "20260101", 10.0),
		portfolioBar("000001.SZ", "20260102", 10.1),
		portfolioBar("000001.SZ", "20260103", 10.2),
	}
	strategies := []strategy.Strategy{
		portfolioWarmupStrategy{name: "limit_up"},
		portfolioWarmupStrategy{name: "sar"},
		portfolioWarmupStrategy{name: "kdj"},
	}
	flows := data.NewMoneyflowStore([]data.Moneyflow{{
		TsCode: "000001.SZ", TradeDate: "20260102", NetMfAmount: 100, BuyLgAmount: 100,
	}})
	result, err := RunPortfolio(PortfolioOptions{
		CodeMap: map[string][]data.DailyBar{"000001.SZ": bars}, Strategies: strategies, Moneyflows: flows,
		StartDate: "20260102", TopN: 1, Config: Config{InitialCapital: 100_000, LotSize: 100},
		MaxTotalPct: 70, MaxSinglePct: 15, MaxSectorPct: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 1 || result.Trades[0].SignalDate != "20260102" || result.Trades[0].Date != "20260103" {
		t.Fatalf("trades = %+v, want signal to use pre-start warmup and buy next open", result.Trades)
	}
}

type portfolioTestStrategy struct{ name string }

func (s portfolioTestStrategy) Name() string { return s.name }
func (s portfolioTestStrategy) Warmup() int  { return 0 }
func (s portfolioTestStrategy) Signal(_ []data.DailyBar, idx int) strategy.SignalType {
	switch idx {
	case 0:
		return strategy.Buy
	case 2:
		return strategy.Sell
	default:
		return strategy.Hold
	}
}
func (s portfolioTestStrategy) Score(_ []data.DailyBar, _ int) float64 { return 20 }

type portfolioWarmupStrategy struct{ name string }

func (s portfolioWarmupStrategy) Name() string { return s.name }
func (s portfolioWarmupStrategy) Warmup() int  { return 1 }
func (s portfolioWarmupStrategy) Signal(_ []data.DailyBar, idx int) strategy.SignalType {
	if idx == 1 {
		return strategy.Buy
	}
	return strategy.Hold
}
func (s portfolioWarmupStrategy) Score(_ []data.DailyBar, _ int) float64 { return 20 }

func portfolioBar(code, date string, price float64) data.DailyBar {
	return data.DailyBar{
		TsCode: code, TradeDate: date, Open: price, High: price + .1, Low: price - .1, Close: price,
		RawOpen: price, RawHigh: price + .1, RawLow: price - .1, RawClose: price, AdjFactor: 1, Vol: 1000,
	}
}
