package strategy

import (
	"math"
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

func TestDividendDeviationRequiresDividendData(t *testing.T) {
	s := NewDividendDeviation(3, 0.9, 1.2)
	if got := s.Signal(dividendDeviationBars(), 3); got != Hold {
		t.Fatalf("Signal() = %s, want HOLD when dividend data is unavailable", got)
	}
}

func TestWilliamsRScoreRewardsOversoldState(t *testing.T) {
	bars := []data.DailyBar{
		strategyBar("20260101", 10, 10.2, 9.8, 10),
		strategyBar("20260102", 10, 10.1, 9.5, 9.7),
		strategyBar("20260103", 9.7, 9.8, 9.1, 9.2),
		strategyBar("20260104", 9.2, 9.3, 8.9, 9.0),
	}
	s := NewWilliamsR(3, -80, -20)
	if score := s.Score(bars, 3); score <= 0 {
		t.Fatalf("Score() = %.2f, want positive score for oversold %%R", score)
	}
}

func TestLimitUpDoesNotSellUnrelatedDownDay(t *testing.T) {
	bars := make([]data.DailyBar, 12)
	for i := range bars {
		bars[i] = strategyBar(strategyDate(i), 10, 10.1, 9.9, 10)
	}
	bars[11] = strategyBar(strategyDate(11), 10, 10.1, 9.7, 9.8)

	if got := NewLimitUp(9.5, 1.2).Signal(bars, 11); got != Hold {
		t.Fatalf("Signal() = %s, want HOLD for unrelated down day", got)
	}
}

func TestLimitUpSellsOnlyAfterConfirmedEntry(t *testing.T) {
	bars := make([]data.DailyBar, 13)
	for i := range bars {
		bars[i] = strategyBar(strategyDate(i), 10, 10.1, 9.9, 10)
	}
	bars[10] = strategyBar(strategyDate(10), 10, 11, 10, 11)
	bars[11] = strategyBar(strategyDate(11), 11.1, 11.3, 11.0, 11.2)
	bars[11].Vol = 2000
	bars[12] = strategyBar(strategyDate(12), 11.1, 11.2, 10.8, 11.0)
	s := NewLimitUp(9.5, 1.2)

	if got := s.Signal(bars, 11); got != Buy {
		t.Fatalf("Signal() = %s, want BUY after confirmed limit-up entry", got)
	}
	if got := s.Signal(bars, 12); got != Sell {
		t.Fatalf("Signal() = %s, want SELL only after previous entry", got)
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

func TestBollingerSellComparesPreviousCloseToPreviousUpperBand(t *testing.T) {
	bars := []data.DailyBar{
		strategyBar("20260101", 10, 10, 10, 10),
		strategyBar("20260102", 10, 10, 10, 10),
		strategyBar("20260103", 20, 20, 20, 20),
		strategyBar("20260104", 19, 19, 19, 19),
	}
	s := NewBollinger(3, 1.0)

	if got := s.Signal(bars, 3); got != Sell {
		t.Fatalf("Signal() = %s, want SELL", got)
	}
}

func TestMAStickyPreviousConvergenceUsesPreviousMA60(t *testing.T) {
	bars := make([]data.DailyBar, 0, 61)
	bars = append(bars, strategyBar(strategyDate(0), 200, 200, 200, 200))
	middleClose := (6000.0 - 200.0 - 20.0*100.0) / 39.0
	for i := 1; i <= 39; i++ {
		bars = append(bars, strategyBar(strategyDate(i), middleClose, middleClose, middleClose, middleClose))
	}
	for i := 40; i <= 59; i++ {
		bars = append(bars, strategyBar(strategyDate(i), 100, 100, 100, 100))
	}
	bars = append(bars, strategyBar(strategyDate(60), 10, 10, 10, 10))

	s := NewMASticky(2.0, 1.5)
	if got := s.Signal(bars, 60); got != Sell {
		t.Fatalf("Signal() = %s, want SELL", got)
	}
}

func TestStrategyScoresReturnFiniteWithZeroDenominators(t *testing.T) {
	volumeBars := []data.DailyBar{
		strategyBar("20260101", 10, 10, 10, 10),
		strategyBar("20260102", 10, 10, 10, 10),
		strategyBar("20260103", 0, 0, 0, 0),
		strategyBar("20260104", 10, 10, 10, 10),
	}
	assertFiniteScore(t, "volume_breakout", NewVolumeBreakout(2, 1.5).Score(volumeBars, 3))

	macdBars := []data.DailyBar{
		strategyBar("20260101", 10, 10, 10, 10),
		strategyBar("20260102", 12, 12, 12, 12),
		strategyBar("20260103", 11, 11, 11, 11),
		strategyBar("20260104", 13, 13, 13, 13),
		strategyBar("20260105", 0, 0, 0, 0),
	}
	assertFiniteScore(t, "macd", NewMACD(2, 3, 1).Score(macdBars, 4))

	atrBars := make([]data.DailyBar, 62)
	for i := range atrBars {
		atrBars[i] = data.DailyBar{
			TsCode:    "000001.SZ",
			TradeDate: strategyDate(i),
			Open:      1,
			High:      0,
			Low:       -1,
			Close:     1,
			Vol:       1000,
		}
	}
	assertFiniteScore(t, "atr_breakout", NewATRBreakout(2, 2, 2, 5, 1.5).Score(atrBars, 61))

	stickyBars := make([]data.DailyBar, 61)
	for i := range stickyBars {
		stickyBars[i] = strategyBar(strategyDate(i), 100, 100, 100, 100)
	}
	assertFiniteScore(t, "ma_sticky", NewMASticky(2.0, 1.5).Score(stickyBars, 60))
}

func TestBottomReversalSignalHandlesZeroLow(t *testing.T) {
	bars := longTrendBars(22)
	bars[21].Low = 0

	if got := NewBottomReversal(20, -15, 1.5, 100, 0.5).Signal(bars, 21); got != Hold {
		t.Fatalf("Signal() = %s, want HOLD for invalid zero low", got)
	}
}

func TestKDJValuesRemainStable(t *testing.T) {
	bars := []data.DailyBar{
		strategyBar("20260101", 10, 11, 9, 10),
		strategyBar("20260102", 11, 12, 10, 11),
		strategyBar("20260103", 12, 13, 10, 12),
		strategyBar("20260104", 11, 12, 9, 10),
		strategyBar("20260105", 10, 11, 8, 9),
		strategyBar("20260106", 9, 10, 7, 8),
	}
	s := NewKDJ(3, 20, 80)

	kValue, dValue := s.kdValue(bars, 5)

	assertNear(t, kValue, 29.629630, 0.000001)
	assertNear(t, dValue, 38.518519, 0.000001)
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

func TestRegistryAndMetadataContainTheSameStrategies(t *testing.T) {
	registry := DefaultRegistry()
	if registry.Count() != len(strategyMetadata) {
		t.Fatalf("registry count = %d, metadata count = %d", registry.Count(), len(strategyMetadata))
	}
	for _, name := range registry.List() {
		meta := MetadataForStrategy(name)
		if meta.Name != name || meta.Group == GroupOther {
			t.Fatalf("metadata for %s = %+v, want named non-default metadata", name, meta)
		}
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

func assertFiniteScore(t *testing.T, name string, score float64) {
	t.Helper()
	if math.IsNaN(score) || math.IsInf(score, 0) {
		t.Fatalf("%s score = %v, want finite", name, score)
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
