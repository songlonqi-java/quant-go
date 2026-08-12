package validation

import (
	"math"
	"path/filepath"
	"testing"

	"quant/internal/data"
	"quant/internal/execution"
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
			alwaysBuyStrategy{name: "bollinger"},
			alwaysBuyStrategy{name: "sar"},
			alwaysBuyStrategy{name: "volume_breakout"},
		},
		Workers: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.FeasibleTrades == 0 {
		t.Fatal("FeasibleTrades = 0, want feasible next-open outcomes")
	}
	if store.OverlappingSignals == 0 || store.EmbargoedSignals == 0 || store.PurgedSignals == 0 {
		t.Fatalf("sampling filters not exercised: overlap=%d embargo=%d purged=%d",
			store.OverlappingSignals, store.EmbargoedSignals, store.PurgedSignals)
	}
	if store.Sampling.ClusterUnit != "signal_date_equal_weight" || !store.Sampling.PurgeAtFoldEnd {
		t.Fatalf("sampling policy = %#v", store.Sampling)
	}
	if store.StrategyFingerprint == "" || store.DataFingerprint == "" || store.BuildFingerprint == "" {
		t.Fatalf("build fingerprints missing: %#v", store)
	}
	stats, ok := store.Stats["horizon|short"]
	if !ok || stats.Samples == 0 {
		t.Fatalf("short horizon stats missing: %#v", store.Stats)
	}
	if stats.Trades != 6 || stats.Samples != 3 || stats.Trades != store.FeasibleTrades {
		t.Fatalf("clustered samples/trades = %d/%d, feasible=%d, want 3/6",
			stats.Samples, stats.Trades, store.FeasibleTrades)
	}
	if stats.ExpectedReturnPct <= 0 || stats.PositiveFolds == 0 {
		t.Fatalf("stats = %#v, want positive out-of-sample return", stats)
	}
	if store.ExitReasonCounts[string(execution.ExitReasonTimeStop)] != store.FeasibleTrades || store.WorstNetReturnPct <= 0 {
		t.Fatalf("exit audit = reasons=%v worst=%.4f feasible=%d", store.ExitReasonCounts, store.WorstNetReturnPct, store.FeasibleTrades)
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
	if loaded.Stats["horizon|short"].Trades != stats.Trades || loaded.Sampling.ClusterUnit != store.Sampling.ClusterUnit {
		t.Fatalf("loaded sampling metadata = %#v / %#v", loaded.Stats["horizon|short"], loaded.Sampling)
	}
}

func TestLoadRejectsPreClusterEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := (&Store{Version: formatVersion - 1, Stats: map[string]Stats{}}).Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("pre-cluster evidence should require rebuild")
	}
}

func TestStoreCompatibilityRejectsStrategyChangesAndStaleData(t *testing.T) {
	strategies := []strategy.Strategy{alwaysBuyStrategy{name: "sar"}}
	codeMap := map[string][]data.DailyBar{"000001.SZ": risingBars("000001.SZ", 10)}
	fingerprint, err := StrategyFingerprint(strategies, 0.0003, 0.0001)
	if err != nil {
		t.Fatal(err)
	}
	dataFingerprint := fingerprintData(codeMap, nil, nil)
	store := &Store{
		Version:             formatVersion,
		StartDate:           codeMap["000001.SZ"][0].TradeDate,
		EndDate:             codeMap["000001.SZ"][9].TradeDate,
		StrategyFingerprint: fingerprint,
		DataFingerprint:     dataFingerprint,
		Sampling:            defaultSamplingPolicy(),
	}
	store.BuildFingerprint = fingerprintBuild(fingerprint, dataFingerprint, store.StartDate, store.EndDate, 0)
	if err := store.ValidateCompatibility(strategies, 0.0003, 0.0001, codeMap, nil, nil); err != nil {
		t.Fatalf("matching evidence rejected: %v", err)
	}
	if err := store.ValidateCompatibility([]strategy.Strategy{alwaysBuyStrategy{name: "macd"}}, 0.0003, 0.0001, codeMap, nil, nil); err == nil {
		t.Fatal("changed strategy should invalidate evidence")
	}
	changed := map[string][]data.DailyBar{"000001.SZ": append([]data.DailyBar(nil), codeMap["000001.SZ"]...)}
	changed["000001.SZ"][9].RawClose++
	if err := store.ValidateCompatibility(strategies, 0.0003, 0.0001, changed, nil, nil); err == nil {
		t.Fatal("changed market data should invalidate evidence")
	}
	store.BuildFingerprint = "tampered"
	if err := store.ValidateCompatibility(strategies, 0.0003, 0.0001, codeMap, nil, nil); err == nil {
		t.Fatal("changed build fingerprint should invalidate evidence")
	}
	store.BuildFingerprint = fingerprintBuild(fingerprint, dataFingerprint, store.StartDate, store.EndDate, 0)
	store.Sampling.ClusterUnit = "trade"
	if err := store.ValidateCompatibility(strategies, 0.0003, 0.0001, codeMap, nil, nil); err == nil {
		t.Fatal("changed sampling policy should invalidate evidence")
	}
}

func TestStoreCompatibilityIncludesLiquidityAndStockMetadata(t *testing.T) {
	strategies := []strategy.Strategy{alwaysBuyStrategy{name: "sar"}}
	codeMap := map[string][]data.DailyBar{"000001.SZ": risingBars("000001.SZ", 10)}
	stockInfos := map[string]data.StockInfo{
		"000001.SZ": {TsCode: "000001.SZ", Name: "平安银行", ListDate: "19910403"},
	}
	policy := execution.DefaultLiquidityPolicy()
	fingerprint, err := StrategyFingerprintWithExecution(strategies, 0.0003, 0.0001, policy, 100_000)
	if err != nil {
		t.Fatal(err)
	}
	dataFingerprint := fingerprintDataWithStocks(codeMap, stockInfos, nil, nil)
	store := &Store{
		Version: formatVersion, StartDate: codeMap["000001.SZ"][0].TradeDate,
		EndDate: codeMap["000001.SZ"][9].TradeDate, StrategyFingerprint: fingerprint,
		DataFingerprint: dataFingerprint, Sampling: defaultSamplingPolicy(),
	}
	store.BuildFingerprint = fingerprintBuild(fingerprint, dataFingerprint, store.StartDate, store.EndDate, 0)
	if err := store.ValidateCompatibilityWithExecution(strategies, 0.0003, 0.0001, policy, 100_000, codeMap, stockInfos, nil, nil); err != nil {
		t.Fatalf("matching execution evidence rejected: %v", err)
	}
	changedPolicy := policy
	changedPolicy.MaxParticipationPct = 3
	if err := store.ValidateCompatibilityWithExecution(strategies, 0.0003, 0.0001, changedPolicy, 100_000, codeMap, stockInfos, nil, nil); err == nil {
		t.Fatal("changed liquidity policy should invalidate evidence")
	}
	changedStocks := map[string]data.StockInfo{"000001.SZ": stockInfos["000001.SZ"]}
	info := changedStocks["000001.SZ"]
	info.ListDate = "19920403"
	changedStocks["000001.SZ"] = info
	if err := store.ValidateCompatibilityWithExecution(strategies, 0.0003, 0.0001, policy, 100_000, codeMap, changedStocks, nil, nil); err == nil {
		t.Fatal("changed listing metadata should invalidate evidence")
	}
}

func TestBuildAppliesLiquidityImpact(t *testing.T) {
	codeMap := map[string][]data.DailyBar{"000001.SZ": risingBars("000001.SZ", 80)}
	for i := range codeMap["000001.SZ"] {
		codeMap["000001.SZ"][i].Amount = 100_000 // thousand CNY
	}
	policy := execution.DefaultLiquidityPolicy()
	policy.MinListingDays = 0
	policy.MinTurnoverRatePct = 0
	store, err := Build(BuildOptions{
		CodeMap: codeMap,
		Strategies: []strategy.Strategy{
			alwaysBuyStrategy{name: "bollinger"},
			alwaysBuyStrategy{name: "sar"},
			alwaysBuyStrategy{name: "volume_breakout"},
		},
		Liquidity: policy, ReferenceEquity: 100_000, Workers: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.FeasibleTrades == 0 || store.ImpactedTrades != store.FeasibleTrades {
		t.Fatalf("liquidity audit = feasible %d impacted %d", store.FeasibleTrades, store.ImpactedTrades)
	}
	if store.AverageEntryImpactPct <= 0 || store.AverageExitImpactPct <= 0 || store.MaxParticipationPct <= 0 {
		t.Fatalf("impact metrics missing: %#v", store)
	}
}

func TestFeasibleReturnKeepsOpenEntryWhenTargetDayBreaksPreviousLow(t *testing.T) {
	bars := risingBars("000001.SZ", 8)
	bars[2].Low = bars[1].Low * 0.9
	bars[2].RawLow = bars[2].Low
	if _, ok := feasibleReturn(bars, 1, strategy.HorizonShort, 0, 0, ""); !ok {
		t.Fatal("intraday break after an open entry must not erase the trade")
	}
}

func TestFeasibleReturnUsesSharedRoundTripCostModel(t *testing.T) {
	bars := risingBars("000001.SZ", 8)
	commission := 0.001
	slippage := 0.002
	got, ok := feasibleReturn(bars, 1, strategy.HorizonShort, commission, slippage, "")
	if !ok {
		t.Fatal("feasibleReturn() = not ok")
	}
	// Five closes are observed after entry; the time stop is confirmed on
	// bars[6] and executed at the following open.
	want, ok := execution.RoundTripReturn(bars[2].TradeOpen(), bars[7].TradeOpen(), execution.CostModel{
		Commission: commission,
		Slippage:   slippage,
	})
	if !ok || math.Abs(got-want.NetReturnPct) > 1e-9 {
		t.Fatalf("feasibleReturn() = %.9f, shared model = %+v", got, want)
	}
}

func TestAnnotateAndAllocateUseEvidenceEligibility(t *testing.T) {
	store := &Store{Version: formatVersion, Stats: map[string]Stats{
		"exact|short|偏多|bollinger+sar+volume_breakout": {
			Trades: 80, Samples: 40, Wins: 24, ExpectedReturnPct: 2, ProxyExpectedReturnPct: 1,
			VolatilityPct: 3,
			AverageWinPct: 4, AverageLossPct: -2, MaxDrawdownPct: -8,
			PositiveFolds: 3, PositiveAlphaFolds: 3, FoldCount: 3,
		},
	}}
	results := []signal.SignalResult{{
		Horizon: strategy.HorizonShort, MarketSentiment: "偏多", BuyCount: 3,
		TotalScore: 2, Confidence: 80, PositionPct: 10,
		Strategies: map[string]signal.SignalDetail{
			"bollinger":       {Signal: strategy.Buy},
			"sar":             {Signal: strategy.Buy},
			"volume_breakout": {Signal: strategy.Buy},
		},
	}}
	results = Annotate(results, store, Policy{MinSamples: 30, MinPositiveFolds: 2, PriorSamples: 20}, true)
	if results[0].HistoricalEvidence == nil || !results[0].HistoricalEvidence.Eligible {
		t.Fatalf("evidence = %#v, want eligible", results[0].HistoricalEvidence)
	}
	if !results[0].HistoricalEvidence.StrategySpecific {
		t.Fatalf("evidence must be strategy-specific: %#v", results[0].HistoricalEvidence)
	}
	if results[0].HistoricalEvidence.PriorBasis != "" || results[0].HistoricalEvidence.PriorWeight != 20 {
		t.Fatalf("neutral prior metadata = %#v", results[0].HistoricalEvidence)
	}
	if results[0].HistoricalEvidence.Trades != 80 || results[0].HistoricalEvidence.Samples != 40 {
		t.Fatalf("evidence counts = %#v", results[0].HistoricalEvidence)
	}
	results = Allocate(results)
	if got := results[0].HistoricalEvidence.SuggestedWeightPct; got <= 0 || got > 100 {
		t.Fatalf("SuggestedWeightPct = %.2f, want (0,100]", got)
	}
	if results[0].PositionPct > 10 {
		t.Fatalf("PositionPct = %.2f, must not exceed strategy cap", results[0].PositionPct)
	}
}

func TestAnnotateNeverPromotesPeriodOnlyPrior(t *testing.T) {
	store := &Store{Version: formatVersion, Stats: map[string]Stats{
		"regime|short|偏多": {
			Trades: 200, Samples: 100, Wins: 80, ExpectedReturnPct: 5,
			PositiveFolds: 3, FoldCount: 3,
		},
		"horizon|short": {
			Trades: 300, Samples: 150, Wins: 120, ExpectedReturnPct: 4,
			PositiveFolds: 3, FoldCount: 3,
		},
	}}
	results := Annotate([]signal.SignalResult{validationCandidate()}, store, DefaultPolicy(), true)
	evidence := results[0].HistoricalEvidence
	if evidence == nil || !evidence.Available || evidence.StrategySpecific || evidence.Eligible {
		t.Fatalf("period prior must be visible but ineligible: %#v", evidence)
	}
	if evidence.Status != "仅有周期先验，不能用于正式资格" || evidence.Basis != "同周期 + 同市场状态（仅先验）" {
		t.Fatalf("period prior status = %#v", evidence)
	}
}

func TestAnnotateBroadPriorCannotRescueLosingExactEvidence(t *testing.T) {
	store := &Store{Version: formatVersion, Stats: map[string]Stats{
		"exact|short|偏多|bollinger+sar+volume_breakout": {
			Trades: 80, Samples: 40, Wins: 20, ExpectedReturnPct: -1,
			PositiveFolds: 2, FoldCount: 3,
		},
		"regime|short|偏多": {
			Trades: 200, Samples: 100, Wins: 80, ExpectedReturnPct: 5,
			PositiveFolds: 3, FoldCount: 3,
		},
	}}
	results := Annotate([]signal.SignalResult{validationCandidate()}, store, DefaultPolicy(), true)
	evidence := results[0].HistoricalEvidence
	if evidence == nil || !evidence.StrategySpecific || evidence.Eligible {
		t.Fatalf("losing exact evidence must remain ineligible: %#v", evidence)
	}
	if evidence.ExpectedReturnPct <= 0 || evidence.PriorBasis != "同周期 + 同市场状态" || evidence.PriorSamples != 100 {
		t.Fatalf("prior audit fields or rescue guard missing: %#v", evidence)
	}
}

func TestAnnotateDoesNotBypassSparseExactRegimeWithSignature(t *testing.T) {
	store := &Store{Version: formatVersion, Stats: map[string]Stats{
		"exact|short|偏多|bollinger+sar+volume_breakout": {
			Trades: 4, Samples: 2, Wins: 2, ExpectedReturnPct: 3,
			PositiveFolds: 1, FoldCount: 1,
		},
		"signature|short|bollinger+sar+volume_breakout": {
			Trades: 100, Samples: 50, Wins: 35, ExpectedReturnPct: 2,
			PositiveFolds: 3, FoldCount: 3,
		},
	}}
	results := Annotate([]signal.SignalResult{validationCandidate()}, store, DefaultPolicy(), true)
	evidence := results[0].HistoricalEvidence
	if evidence == nil || evidence.Basis != "同策略组合 + 同市场状态" || evidence.Samples != 2 || evidence.Eligible {
		t.Fatalf("sparse current-regime evidence must not be bypassed: %#v", evidence)
	}
}

func TestAnnotateUsesSignatureWhenNoExactRegimeEvidenceExists(t *testing.T) {
	store := &Store{Version: formatVersion, Stats: map[string]Stats{
		"signature|short|bollinger+sar+volume_breakout": {
			Trades: 100, Samples: 50, Wins: 35, ExpectedReturnPct: 2, ProxyExpectedReturnPct: 0.5,
			PositiveFolds: 3, PositiveAlphaFolds: 3, FoldCount: 3,
		},
		"horizon|short": {
			Trades: 200, Samples: 100, Wins: 60, ExpectedReturnPct: 1,
			PositiveFolds: 3, FoldCount: 3,
		},
	}}
	results := Annotate([]signal.SignalResult{validationCandidate()}, store, DefaultPolicy(), true)
	evidence := results[0].HistoricalEvidence
	if evidence == nil || evidence.Basis != "同策略组合" || !evidence.StrategySpecific || !evidence.Eligible {
		t.Fatalf("signature evidence = %#v, want eligible", evidence)
	}
}

func TestEffectivePriorWeightNeverExceedsAvailableDates(t *testing.T) {
	if got := effectivePriorWeight(20, Stats{Samples: 5}, true); got != 5 {
		t.Fatalf("prior weight = %.0f, want 5", got)
	}
	if got := effectivePriorWeight(20, Stats{}, false); got != 20 {
		t.Fatalf("neutral prior weight = %.0f, want 20", got)
	}
}

func validationCandidate() signal.SignalResult {
	return signal.SignalResult{
		Horizon: strategy.HorizonShort, MarketSentiment: "偏多", BuyCount: 3,
		TotalScore: 2, Confidence: 80, PositionPct: 10,
		Strategies: map[string]signal.SignalDetail{
			"bollinger":       {Signal: strategy.Buy},
			"sar":             {Signal: strategy.Buy},
			"volume_breakout": {Signal: strategy.Buy},
		},
	}
}

func TestSummarizeUsesEqualWeightSignalDateClusters(t *testing.T) {
	stats := summarize([]observation{
		{date: "20250102", code: "000001.SZ", ret: 2, fold: 1},
		{date: "20250102", code: "000002.SZ", ret: 4, fold: 1},
		{date: "20250103", code: "000001.SZ", ret: -1, fold: 2},
		{date: "20250103", code: "000002.SZ", ret: -3, fold: 2},
	}, 2)
	if stats.Trades != 4 || stats.Samples != 2 || stats.Wins != 1 {
		t.Fatalf("clustered counts = %#v", stats)
	}
	if stats.ExpectedReturnPct != 0.5 || stats.WinRatePct != 50 {
		t.Fatalf("clustered returns = %#v", stats)
	}
	if stats.PositiveFolds != 1 || stats.FoldCount != 2 {
		t.Fatalf("clustered folds = %#v", stats)
	}
}

func TestSamplingPolicyMatchesHoldingWindows(t *testing.T) {
	policy := defaultSamplingPolicy()
	if policy.IndependenceKey != "stock+horizon" || len(policy.HorizonRules) != 3 {
		t.Fatalf("policy = %#v", policy)
	}
	for _, rule := range policy.HorizonRules {
		if rule.HoldingTradingDays != horizonDays(rule.Horizon) ||
			rule.CooldownTradingDays != rule.HoldingTradingDays ||
			rule.EmbargoTradingDays != rule.HoldingTradingDays {
			t.Fatalf("rule = %#v", rule)
		}
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
