package strategy

import (
	"testing"

	"quant/internal/data"
)

func TestDonchianBreaksPreviousChannel(t *testing.T) {
	bars := []data.DailyBar{
		strategyBar("20260101", 10, 11, 9, 10),
		strategyBar("20260102", 10, 12, 9.5, 11),
		strategyBar("20260103", 11, 12.5, 10.5, 12),
		strategyBar("20260104", 12, 13, 11.5, 12.6),
	}
	s := NewDonchian(3)

	if got := s.Signal(bars, 3); got != Buy {
		t.Fatalf("Signal() = %s, want BUY", got)
	}
}

func TestDonchianBreaksPreviousLowerChannel(t *testing.T) {
	bars := []data.DailyBar{
		strategyBar("20260101", 10, 11, 9, 10),
		strategyBar("20260102", 10, 12, 9.5, 11),
		strategyBar("20260103", 11, 12.5, 10.5, 10),
		strategyBar("20260104", 10, 10.3, 8.8, 8.9),
	}
	s := NewDonchian(3)

	if got := s.Signal(bars, 3); got != Sell {
		t.Fatalf("Signal() = %s, want SELL", got)
	}
}

func TestDividendDeviationRequiresSensibleHighDividend(t *testing.T) {
	bars := dividendDeviationBars()
	s := NewDividendDeviation(3, 0.9, 1.2)
	store := data.NewFundamentalStore()
	store.LoadDailyBasics([]data.DailyBasic{
		{TsCode: "000001.SZ", TradeDate: "20260104", DvRatio: 4.2},
	})
	s.SetFundStore(store)

	if got := s.Signal(bars, 3); got != Buy {
		t.Fatalf("Signal() = %s, want BUY", got)
	}
}

func TestDividendDeviationFiltersLowDividend(t *testing.T) {
	bars := dividendDeviationBars()
	s := NewDividendDeviation(3, 0.9, 1.2)
	store := data.NewFundamentalStore()
	store.LoadDailyBasics([]data.DailyBasic{
		{TsCode: "000001.SZ", TradeDate: "20260104", DvRatio: 1.5},
	})
	s.SetFundStore(store)

	if got := s.Signal(bars, 3); got != Hold {
		t.Fatalf("Signal() = %s, want HOLD", got)
	}
}

func TestMACDUsesRecursiveEMA(t *testing.T) {
	bars := []data.DailyBar{
		strategyBar("20260101", 10, 10, 10, 10),
		strategyBar("20260102", 11, 11, 11, 11),
		strategyBar("20260103", 12, 12, 12, 12),
		strategyBar("20260104", 11, 11, 11, 11),
		strategyBar("20260105", 13, 13, 13, 13),
		strategyBar("20260106", 14, 14, 14, 14),
	}
	s := NewMACD(3, 5, 2)

	dif, dea, hist := s.macdAt(bars, 5)

	assertNear(t, dif, 0.619727, 0.00001)
	assertNear(t, dea, 0.537123, 0.00001)
	assertNear(t, hist, 0.165208, 0.00001)
}

func TestRSIUsesWilderSmoothing(t *testing.T) {
	bars := []data.DailyBar{
		strategyBar("20260101", 10, 10, 10, 10),
		strategyBar("20260102", 11, 11, 11, 11),
		strategyBar("20260103", 12, 12, 12, 12),
		strategyBar("20260104", 11, 11, 11, 11),
		strategyBar("20260105", 13, 13, 13, 13),
		strategyBar("20260106", 12, 12, 12, 12),
	}

	assertNear(t, rsiValue(bars, 3, 3), 66.666667, 0.000001)
	assertNear(t, rsiValue(bars, 4, 3), 83.333333, 0.000001)
	assertNear(t, rsiValue(bars, 5, 3), 60.606061, 0.000001)
}

func TestQualityValueBuySignal(t *testing.T) {
	bars := longTrendBars(260)
	s := NewQualityValue(120, 25, 3, 12, 1.5)
	store := data.NewFundamentalStore()
	store.LoadDailyBasics([]data.DailyBasic{
		{TsCode: "000001.SZ", TradeDate: bars[len(bars)-1].TradeDate, PeTTM: 15, Pb: 1.6, DvTTM: 2.5},
	})
	store.LoadFinaIndicators([]data.FinaIndicator{
		{TsCode: "000001.SZ", AnnDate: "20260901", EndDate: "20260630", Roe: 16},
	})
	s.SetFundStore(store)

	if got := s.Signal(bars, len(bars)-1); got != Buy {
		t.Fatalf("Signal() = %s, want BUY", got)
	}
	if HorizonForStrategy(s.Name()) != HorizonLong {
		t.Fatalf("HorizonForStrategy(%s) = %s, want long", s.Name(), HorizonForStrategy(s.Name()))
	}
}

func TestEarningsGrowthBuySignal(t *testing.T) {
	bars := longTrendBars(140)
	s := NewEarningsGrowth(60, 120, 60, 10, 5, 10)
	store := data.NewFundamentalStore()
	store.LoadDailyBasics([]data.DailyBasic{
		{TsCode: "000001.SZ", TradeDate: bars[len(bars)-1].TradeDate, PeTTM: 28, Pb: 3.2},
	})
	store.LoadFinaIndicators([]data.FinaIndicator{
		{TsCode: "000001.SZ", AnnDate: "20260501", EndDate: "20260331", Roe: 14, NIncomeYoY: 25, RevenueYoY: 12},
	})
	s.SetFundStore(store)

	if got := s.Signal(bars, len(bars)-1); got != Buy {
		t.Fatalf("Signal() = %s, want BUY", got)
	}
	if HorizonForStrategy(s.Name()) != HorizonLong {
		t.Fatalf("HorizonForStrategy(%s) = %s, want long", s.Name(), HorizonForStrategy(s.Name()))
	}
	if GroupForStrategy(s.Name()) != GroupValue {
		t.Fatalf("GroupForStrategy(%s) = %s, want value", s.Name(), GroupForStrategy(s.Name()))
	}
}

func dividendDeviationBars() []data.DailyBar {
	return []data.DailyBar{
		strategyBar("20260101", 10, 10.2, 9.8, 10),
		strategyBar("20260102", 10, 10.2, 9.8, 10),
		strategyBar("20260103", 8.5, 8.8, 8.4, 8.7),
		strategyBar("20260104", 7.8, 8.1, 7.7, 8.0),
	}
}

func longTrendBars(days int) []data.DailyBar {
	bars := make([]data.DailyBar, 0, days)
	for i := 0; i < days; i++ {
		closePrice := 10 + float64(i)*0.02
		bars = append(bars, strategyBar(strategyDate(i), closePrice-0.05, closePrice+0.1, closePrice-0.1, closePrice))
	}
	return bars
}

func strategyDate(offset int) string {
	day := 1 + offset
	month := 1 + day/28
	dayInMonth := 1 + day%28
	return "2026" + twoDigit(month) + twoDigit(dayInMonth)
}

func assertNear(t *testing.T, got, want, tolerance float64) {
	t.Helper()
	if got < want-tolerance || got > want+tolerance {
		t.Fatalf("got %.6f, want %.6f", got, want)
	}
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func strategyBar(date string, open, high, low, close float64) data.DailyBar {
	return data.DailyBar{
		TsCode:    "000001.SZ",
		TradeDate: date,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Vol:       1000,
		RawOpen:   open,
		RawHigh:   high,
		RawLow:    low,
		RawClose:  close,
		AdjFactor: 1,
	}
}
