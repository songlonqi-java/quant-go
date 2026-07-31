package validation

import (
	"path/filepath"
	"testing"

	"quant/internal/data"
	"quant/internal/signal"
	"quant/internal/strategy"
)

type alwaysBuyStrategy struct{ name string }

func (s alwaysBuyStrategy) Name() string { return s.name }
func (s alwaysBuyStrategy) Warmup() int  { return 1 }
func (s alwaysBuyStrategy) Signal(_ []data.DailyBar, _ int) strategy.SignalType {
	return strategy.Buy
}

func TestBuildProducesOutOfSampleStatsWithFeasibleNextOpenEntry(t *testing.T) {
	codeMap := map[string][]data.DailyBar{
		"000001.SZ": risingBars("000001.SZ", 80),
		"000002.SZ": risingBars("000002.SZ", 80),
	}
	store, err := Build(BuildOptions{
		CodeMap: codeMap,
		Strategies: []strategy.Strategy{
			alwaysBuyStrategy{name: "donchian"},
			alwaysBuyStrategy{name: "roc"},
			alwaysBuyStrategy{name: "sar"},
		},
		Workers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.FeasibleTrades == 0 {
		t.Fatal("FeasibleTrades = 0, want feasible next-open outcomes")
	}
	stats, ok := store.Stats["horizon|short"]
	if !ok || stats.Samples == 0 {
		t.Fatalf("short horizon stats missing: %#v", store.Stats)
	}
	if stats.ExpectedReturnPct <= 0 || stats.PositiveFolds == 0 {
		t.Fatalf("stats = %#v, want positive out-of-sample return", stats)
	}

	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := store.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Stats["horizon|short"].Samples != stats.Samples {
		t.Fatalf("loaded samples = %d, want %d", loaded.Stats["horizon|short"].Samples, stats.Samples)
	}
}

func TestAnnotateAndAllocateUseEvidenceEligibility(t *testing.T) {
	store := &Store{Version: formatVersion, Stats: map[string]Stats{
		"exact|short|偏多|donchian+roc+sar": {
			Samples: 40, Wins: 24, ExpectedReturnPct: 2, VolatilityPct: 3,
			AverageWinPct: 4, AverageLossPct: -2, MaxDrawdownPct: -8,
			PositiveFolds: 3, FoldCount: 3,
		},
	}}
	results := []signal.SignalResult{{
		Horizon: strategy.HorizonShort, MarketSentiment: "偏多", BuyCount: 3,
		TotalScore: 2, Confidence: 80, PositionPct: 10,
		Strategies: map[string]signal.SignalDetail{
			"donchian": {Signal: strategy.Buy},
			"roc":      {Signal: strategy.Buy},
			"sar":      {Signal: strategy.Buy},
		},
	}}
	results = Annotate(results, store, Policy{MinSamples: 30, MinPositiveFolds: 2, PriorSamples: 20}, true)
	if results[0].HistoricalEvidence == nil || !results[0].HistoricalEvidence.Eligible {
		t.Fatalf("evidence = %#v, want eligible", results[0].HistoricalEvidence)
	}
	results = Allocate(results)
	if got := results[0].HistoricalEvidence.SuggestedWeightPct; got <= 0 || got > 100 {
		t.Fatalf("SuggestedWeightPct = %.2f, want (0,100]", got)
	}
	if results[0].PositionPct > 10 {
		t.Fatalf("PositionPct = %.2f, must not exceed strategy cap", results[0].PositionPct)
	}
}

func TestAnnotateLeavesSellSignalsWithoutBuyEvidence(t *testing.T) {
	store := &Store{Version: formatVersion, Stats: map[string]Stats{
		"horizon|short": {Samples: 100, Wins: 80, ExpectedReturnPct: 2, PositiveFolds: 3, FoldCount: 3},
	}}
	results := []signal.SignalResult{{Horizon: strategy.HorizonShort, SellCount: 2, TotalScore: -1}}
	results = Annotate(results, store, DefaultPolicy(), true)
	if results[0].HistoricalEvidence != nil {
		t.Fatalf("sell evidence = %#v, want nil", results[0].HistoricalEvidence)
	}
}

func TestFeasibleReturnRejectsLimitUpAndGapEntries(t *testing.T) {
	bars := risingBars("000001.SZ", 8)
	bars[2].UpLimit = bars[2].Open
	if _, ok := feasibleReturn(bars, 1, strategy.HorizonShort, 0, 0, ""); ok {
		t.Fatal("limit-up open should be rejected")
	}
	bars[2].UpLimit = 0
	bars[2].Open = bars[1].TradeClose() * 1.04
	bars[2].RawOpen = bars[2].Open
	if _, ok := feasibleReturn(bars, 1, strategy.HorizonShort, 0, 0, ""); ok {
		t.Fatal("gap above 3% should be rejected")
	}
}

func TestFeasibleReturnDoesNotUseExitAfterBacktestEnd(t *testing.T) {
	bars := risingBars("000001.SZ", 8)
	if _, ok := feasibleReturn(bars, 1, strategy.HorizonShort, 0, 0, "20250105"); ok {
		t.Fatal("outcome beyond the requested backtest end should be rejected")
	}
}

func risingBars(code string, count int) []data.DailyBar {
	bars := make([]data.DailyBar, 0, count)
	price := 10.0
	for i := 0; i < count; i++ {
		open := price * 1.002
		close := price * 1.01
		bars = append(bars, data.DailyBar{
			TsCode: code, TradeDate: "2025" + twoDigits(i/30+1) + twoDigits(i%30+1),
			Open: open, High: close * 1.01, Low: price * 0.995, Close: close,
			RawOpen: open, RawHigh: close * 1.01, RawLow: price * 0.995, RawClose: close,
			AdjFactor: 1, Vol: 1000,
		})
		price = close
	}
	return bars
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string([]byte{'0' + byte(value/10), '0' + byte(value%10)})
}
