package backtest

import (
	"math"
	"testing"

	"quant/internal/data"
	"quant/internal/strategy"
)

func TestRunExecutesSignalsAtNextOpen(t *testing.T) {
	bars := []data.DailyBar{
		bar("20260101", 10, 10, 1000),
		bar("20260102", 11, 12, 1000),
		bar("20260103", 13, 14, 1000),
		bar("20260104", 15, 16, 1000),
	}
	signals := map[int]strategy.SignalType{
		0: strategy.Buy,
		2: strategy.Sell,
	}

	result := Run(bars, func(_ []data.DailyBar, idx int) strategy.SignalType {
		return signals[idx]
	}, Config{InitialCapital: 2000, LimitPct: 0.2})

	if result.TradeCount != 2 {
		t.Fatalf("TradeCount = %d, want 2", result.TradeCount)
	}
	if len(result.Trades) != 2 {
		t.Fatalf("len(Trades) = %d, want 2", len(result.Trades))
	}

	buy := result.Trades[0]
	if buy.Action != "BUY" || buy.SignalDate != "20260101" || buy.Date != "20260102" {
		t.Fatalf("buy trade = %+v, want BUY from 20260101 executed 20260102", buy)
	}
	if buy.Price != 11 {
		t.Fatalf("buy price = %.2f, want next open 11", buy.Price)
	}

	sell := result.Trades[1]
	if sell.Action != "SELL" || sell.SignalDate != "20260103" || sell.Date != "20260104" {
		t.Fatalf("sell trade = %+v, want SELL from 20260103 executed 20260104", sell)
	}
	if sell.Price != 15 {
		t.Fatalf("sell price = %.2f, want next open 15", sell.Price)
	}
	if sell.Shares == 0 || sell.Code != "000001.SZ" {
		t.Fatalf("sell trade = %+v, want sold shares and security code", sell)
	}
}

func TestCalculateMetricsMatchesTradesPerSecurity(t *testing.T) {
	result := &Result{FinalEquity: 100, TradeCount: 4, Trades: []Trade{
		{Code: "A", Action: "BUY", Price: 10, Shares: 100},
		{Code: "B", Action: "BUY", Price: 20, Shares: 100},
		{Code: "B", Action: "SELL", Price: 18, Shares: 100},
		{Code: "A", Action: "SELL", Price: 12, Shares: 100},
	}}
	metrics := CalculateMetrics(result, 100, 0, 252)
	if metrics.WinningTrades != 1 || metrics.LosingTrades != 1 || metrics.WinRate != 50 {
		t.Fatalf("metrics = %+v, want one win and one loss", metrics)
	}
}

func TestRunSkipsLimitUpBuyAtOpen(t *testing.T) {
	bars := []data.DailyBar{
		bar("20260101", 10, 10, 1000),
		bar("20260102", 11, 11, 1000),
	}

	result := Run(bars, func(_ []data.DailyBar, idx int) strategy.SignalType {
		if idx == 0 {
			return strategy.Buy
		}
		return strategy.Hold
	}, Config{InitialCapital: 1000, LimitPct: 0.095})

	if result.TradeCount != 0 {
		t.Fatalf("TradeCount = %d, want 0", result.TradeCount)
	}
	if result.SkippedSignals != 1 {
		t.Fatalf("SkippedSignals = %d, want 1", result.SkippedSignals)
	}
}

func TestRunSkipsExactLimitUpBuyAtOpen(t *testing.T) {
	bars := []data.DailyBar{
		bar("20260101", 10, 10, 1000),
		bar("20260102", 10.5, 10.4, 1000),
	}
	bars[1].UpLimit = 10.5
	bars[1].DownLimit = 9.5

	result := Run(bars, func(_ []data.DailyBar, idx int) strategy.SignalType {
		if idx == 0 {
			return strategy.Buy
		}
		return strategy.Hold
	}, Config{InitialCapital: 1000, LimitPct: 0.2})

	if result.TradeCount != 0 {
		t.Fatalf("TradeCount = %d, want 0", result.TradeCount)
	}
	if result.SkippedSignals != 1 {
		t.Fatalf("SkippedSignals = %d, want 1", result.SkippedSignals)
	}
}

func TestRunDoesNotApplyMainBoardFallbackWhenExactLimitIsAvailable(t *testing.T) {
	bars := []data.DailyBar{
		bar("20260101", 10, 10, 1000),
		bar("20260102", 11, 11, 1000),
	}
	bars[1].TsCode = "300001.SZ"
	bars[1].UpLimit = 12
	bars[1].DownLimit = 8

	result := Run(bars, func(_ []data.DailyBar, idx int) strategy.SignalType {
		if idx == 0 {
			return strategy.Buy
		}
		return strategy.Hold
	}, Config{InitialCapital: 2000})

	if result.TradeCount != 1 {
		t.Fatalf("TradeCount = %d, want tradable ChiNext open", result.TradeCount)
	}
}

func TestRunKeepsBuyCashNonNegativeWithCommission(t *testing.T) {
	bars := []data.DailyBar{
		bar("20260101", 10, 10, 1000),
		bar("20260102", 10, 10, 1000),
	}

	result := Run(bars, func(_ []data.DailyBar, idx int) strategy.SignalType {
		if idx == 0 {
			return strategy.Buy
		}
		return strategy.Hold
	}, Config{InitialCapital: 2000, Commission: 0.001, LimitPct: 0.2})

	if len(result.Trades) != 1 {
		t.Fatalf("len(Trades) = %d, want 1", len(result.Trades))
	}
	if result.Trades[0].Shares != 100 {
		t.Fatalf("shares = %.0f, want 100", result.Trades[0].Shares)
	}
	if result.Trades[0].Cash < -1e-9 {
		t.Fatalf("cash = %.12f, want non-negative", result.Trades[0].Cash)
	}
}

func TestRunSkipsBuyWhenCashCannotCoverHundredShareLot(t *testing.T) {
	bars := []data.DailyBar{
		bar("20260101", 33, 33, 1000),
		bar("20260102", 33, 33, 1000),
	}

	result := Run(bars, func(_ []data.DailyBar, idx int) strategy.SignalType {
		if idx == 0 {
			return strategy.Buy
		}
		return strategy.Hold
	}, Config{InitialCapital: 1000, LimitPct: 0.2})

	if len(result.Trades) != 0 {
		t.Fatalf("len(Trades) = %d, want 0", len(result.Trades))
	}
	if result.SkippedSignals != 1 {
		t.Fatalf("SkippedSignals = %d, want 1", result.SkippedSignals)
	}
}

func TestCalculateMetricsDefaultsInvalidTradingDays(t *testing.T) {
	result := &Result{
		FinalEquity: 105,
		TradeCount:  0,
		EquityCurve: []EquityPoint{
			{Date: "20260101", Value: 100},
			{Date: "20260102", Value: 110},
			{Date: "20260103", Value: 105},
		},
	}

	got := CalculateMetrics(result, 100, 0.03, -1)
	want := CalculateMetrics(result, 100, 0.03, 252)

	if !finiteMetric(got.AnnualizedReturn) || !finiteMetric(got.Volatility) || !finiteMetric(got.SharpeRatio) {
		t.Fatalf("metrics contain non-finite values: %+v", got)
	}
	if got.AnnualizedReturn != want.AnnualizedReturn || got.Volatility != want.Volatility || got.SharpeRatio != want.SharpeRatio {
		t.Fatalf("metrics = %+v, want tradingDays default equivalent %+v", got, want)
	}
}

func TestCalculateMetricsKeepsCalmarNegativeForLosingStrategy(t *testing.T) {
	result := &Result{
		FinalEquity: 80,
		EquityCurve: []EquityPoint{
			{Date: "20260101", Value: 100},
			{Date: "20260102", Value: 90},
			{Date: "20260103", Value: 80},
		},
	}

	metrics := CalculateMetrics(result, 100, 0.03, 252)
	if metrics.CalmarRatio >= 0 {
		t.Fatalf("CalmarRatio = %.2f, want negative for losing strategy", metrics.CalmarRatio)
	}
}

func bar(date string, open, close, vol float64) data.DailyBar {
	return data.DailyBar{
		TsCode:    "000001.SZ",
		TradeDate: date,
		Open:      open,
		High:      math.Max(open, close),
		Low:       math.Min(open, close),
		Close:     close,
		Vol:       vol,
		RawOpen:   open,
		RawHigh:   math.Max(open, close),
		RawLow:    math.Min(open, close),
		RawClose:  close,
		AdjFactor: 1,
	}
}

func finiteMetric(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
