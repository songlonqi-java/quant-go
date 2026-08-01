package strategy

import (
	"math"
	"testing"
	"time"

	"quant/internal/data"
)

func TestMarketNeutralMomentumPrefersResidualAlphaOverHighBeta(t *testing.T) {
	universe := neutralMomentumUniverse(150)
	strategy := NewMarketNeutralMomentum(20, 60, 120, 60, 40, 25)
	strategy.SetHistoricalUniverse(universe)

	alphaBars := universe["000002.SZ"]
	highBetaBars := universe["000001.SZ"]
	last := len(alphaBars) - 1
	alphaScore := strategy.Score(alphaBars, last)
	highBetaScore := strategy.Score(highBetaBars, last)
	if alphaScore <= highBetaScore || alphaScore <= 0 {
		t.Fatalf("scores alpha/high-beta = %.4f/%.4f, want positive residual alpha to rank first", alphaScore, highBetaScore)
	}
	if got := strategy.Signal(alphaBars, last); got != Buy {
		t.Fatalf("alpha Signal() = %s, want BUY", got)
	}
	if got := strategy.Signal(highBetaBars, last); got == Buy {
		t.Fatalf("market-driven high-beta stock unexpectedly selected with score %.4f", highBetaScore)
	}
}

func TestMarketNeutralMomentumHistoricalScoreDoesNotUseFutureReturns(t *testing.T) {
	universe := neutralMomentumUniverse(150)
	target := 130
	beforeStrategy := NewMarketNeutralMomentum(20, 60, 120, 60, 40, 25)
	beforeStrategy.SetHistoricalUniverse(universe)
	before := beforeStrategy.Score(universe["000002.SZ"], target)

	changed := cloneNeutralUniverse(universe)
	last := len(changed["000004.SZ"]) - 1
	changed["000004.SZ"][last].Close *= 8
	changed["000004.SZ"][last].RawClose = changed["000004.SZ"][last].Close
	afterStrategy := NewMarketNeutralMomentum(20, 60, 120, 60, 40, 25)
	afterStrategy.SetHistoricalUniverse(changed)
	after := afterStrategy.Score(changed["000002.SZ"], target)
	if math.Abs(before-after) > 1e-12 {
		t.Fatalf("historical score changed after future shock: before %.12f after %.12f", before, after)
	}
}

func TestMarketNeutralMomentumIgnoresRawExecutionPriceGaps(t *testing.T) {
	universe := neutralMomentumUniverse(150)
	baseline := NewMarketNeutralMomentum(20, 60, 120, 60, 40, 25)
	baseline.SetHistoricalUniverse(universe)
	target := universe["000002.SZ"]
	last := len(target) - 1
	want := baseline.Score(target, last)

	changed := cloneNeutralUniverse(universe)
	// Simulate an unadjusted ex-rights gap. Adjusted Close is unchanged, so
	// neither the market proxy nor a stock's factor score may move.
	changed["000004.SZ"][last].RawClose *= 0.1
	changed["000002.SZ"][last].RawClose *= 0.2
	actual := NewMarketNeutralMomentum(20, 60, 120, 60, 40, 25)
	actual.SetHistoricalUniverse(changed)
	if got := actual.Score(changed["000002.SZ"], last); math.Abs(got-want) > 1e-12 {
		t.Fatalf("score changed with raw-only execution gap: got %.12f want %.12f", got, want)
	}
}

func TestMarketNeutralMomentumFailsClosedWithoutPreparedUniverse(t *testing.T) {
	bars := neutralMomentumUniverse(150)["000002.SZ"]
	strategy := NewMarketNeutralMomentum(20, 60, 120, 60, 40, 25)
	if got := strategy.Signal(bars, len(bars)-1); got != Hold {
		t.Fatalf("unprepared Signal() = %s, want HOLD", got)
	}
}

func TestMarketNeutralMomentumLatestAndHistoricalPreparationAgree(t *testing.T) {
	universe := neutralMomentumUniverse(150)
	live := NewMarketNeutralMomentum(20, 60, 120, 60, 40, 25)
	live.SetUniverse(universe)
	historical := NewMarketNeutralMomentum(20, 60, 120, 60, 40, 25)
	historical.SetHistoricalUniverse(universe)
	bars := universe["000002.SZ"]
	last := len(bars) - 1
	if live.Signal(bars, last) != historical.Signal(bars, last) || math.Abs(live.Score(bars, last)-historical.Score(bars, last)) > 1e-12 {
		t.Fatalf("latest/historical mismatch: live %s %.12f historical %s %.12f",
			live.Signal(bars, last), live.Score(bars, last), historical.Signal(bars, last), historical.Score(bars, last))
	}
}

func neutralMomentumUniverse(count int) map[string][]data.DailyBar {
	type model struct {
		code  string
		beta  float64
		alpha float64
	}
	models := []model{
		{code: "000001.SZ", beta: 1.8},
		{code: "000002.SZ", beta: 0.7, alpha: 0.0015},
		{code: "000003.SZ", beta: 0.9, alpha: -0.0004},
		{code: "000004.SZ", beta: 1.1, alpha: -0.0002},
	}
	result := make(map[string][]data.DailyBar, len(models))
	prices := make(map[string]float64, len(models))
	for _, model := range models {
		prices[model.code] = 10
	}
	start := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	for day := 0; day < count; day++ {
		date := start.AddDate(0, 0, day).Format("20060102")
		marketReturn := 0.001 + 0.006*math.Sin(float64(day)*0.31)
		for _, model := range models {
			if day > 0 {
				prices[model.code] *= 1 + model.beta*marketReturn + model.alpha
			}
			closePrice := prices[model.code]
			result[model.code] = append(result[model.code], data.DailyBar{
				TsCode: model.code, TradeDate: date,
				Open: closePrice, High: closePrice * 1.01, Low: closePrice * 0.99, Close: closePrice,
				RawOpen: closePrice, RawHigh: closePrice * 1.01, RawLow: closePrice * 0.99, RawClose: closePrice,
				AdjFactor: 1, Vol: 1000, Amount: 100_000,
			})
		}
	}
	return result
}

func cloneNeutralUniverse(source map[string][]data.DailyBar) map[string][]data.DailyBar {
	cloned := make(map[string][]data.DailyBar, len(source))
	for code, bars := range source {
		cloned[code] = append([]data.DailyBar(nil), bars...)
	}
	return cloned
}
