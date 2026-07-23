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
	}, Config{InitialCapital: 1000, LimitPct: 0.2})

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
	}, Config{InitialCapital: 1000, Commission: 0.001, LimitPct: 0.2})

	if len(result.Trades) != 1 {
		t.Fatalf("len(Trades) = %d, want 1", len(result.Trades))
	}
	if result.Trades[0].Cash < -1e-9 {
		t.Fatalf("cash = %.12f, want non-negative", result.Trades[0].Cash)
	}
	if math.Abs(result.Trades[0].Cash) > 1e-9 {
		t.Fatalf("cash = %.12f, want near zero", result.Trades[0].Cash)
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
