// Package validation builds and queries historical, out-of-sample evidence for
// the complete signal decision chain. Its interface deliberately exposes only
// build/load/query operations; replay details, chronological folds, feasible
// entry checks, and shrinkage stay inside the module.
package validation

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"quant/internal/data"
	"quant/internal/execution"
	"quant/internal/market"
	"quant/internal/signal"
	"quant/internal/strategy"
)

const formatVersion = 5

type Policy struct {
	MinSamples           int
	MinPositiveFolds     int
	MinExpectedReturnPct float64
	PriorSamples         float64
}

func DefaultPolicy() Policy {
	return Policy{MinSamples: 30, MinPositiveFolds: 2, PriorSamples: 20}
}

type BuildOptions struct {
	CodeMap      map[string][]data.DailyBar
	StockNames   map[string]string
	StockInfos   map[string]data.StockInfo
	Strategies   []strategy.Strategy
	Fundamentals *data.FundamentalStore
	Moneyflows   *data.MoneyflowStore
	Commission   float64
	Slippage     float64
	Liquidity    execution.LiquidityPolicy
	// ReferenceEquity translates a signal's target position percentage into
	// an order value for participation and market-impact estimates.
	ReferenceEquity float64
	Workers         int
	StartDate       string
	EndDate         string
	FoldCount       int
}

type Fold struct {
	Number    int    `json:"number"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type Stats struct {
	Trades            int     `json:"trades"`
	Samples           int     `json:"samples"`
	Wins              int     `json:"wins"`
	WinRatePct        float64 `json:"win_rate_pct"`
	ExpectedReturnPct float64 `json:"expected_return_pct"`
	AverageWinPct     float64 `json:"average_win_pct"`
	AverageLossPct    float64 `json:"average_loss_pct"`
	VolatilityPct     float64 `json:"volatility_pct"`
	MaxDrawdownPct    float64 `json:"max_drawdown_pct"`
	PositiveFolds     int     `json:"positive_folds"`
	FoldCount         int     `json:"fold_count"`
}

type HorizonSamplingRule struct {
	Horizon             strategy.Horizon `json:"horizon"`
	HoldingTradingDays  int              `json:"holding_trading_days"`
	CooldownTradingDays int              `json:"cooldown_trading_days"`
	EmbargoTradingDays  int              `json:"embargo_trading_days"`
}

type SamplingPolicy struct {
	ClusterUnit     string                `json:"cluster_unit"`
	IndependenceKey string                `json:"independence_key"`
	PurgeAtFoldEnd  bool                  `json:"purge_at_fold_end"`
	HorizonRules    []HorizonSamplingRule `json:"horizon_rules"`
}

type Store struct {
	Version               int                       `json:"version"`
	BuiltAt               string                    `json:"built_at"`
	StartDate             string                    `json:"start_date"`
	EndDate               string                    `json:"end_date"`
	StrategyFingerprint   string                    `json:"strategy_fingerprint"`
	DataFingerprint       string                    `json:"data_fingerprint"`
	BuildFingerprint      string                    `json:"build_fingerprint"`
	Folds                 []Fold                    `json:"folds"`
	Sampling              SamplingPolicy            `json:"sampling"`
	Stats                 map[string]Stats          `json:"stats"`
	ScannedSignals        int                       `json:"scanned_signals"`
	FeasibleTrades        int                       `json:"feasible_trades"`
	SkippedTrades         int                       `json:"skipped_trades"`
	OverlappingSignals    int                       `json:"overlapping_signals"`
	EmbargoedSignals      int                       `json:"embargoed_signals"`
	PurgedSignals         int                       `json:"purged_signals"`
	UnresolvedExits       int                       `json:"unresolved_exits"`
	ExitReasonCounts      map[string]int            `json:"exit_reason_counts"`
	DelayedExitTrades     int                       `json:"delayed_exit_trades"`
	ExitDelayDays         int                       `json:"exit_delay_days"`
	MaxExitDelayDays      int                       `json:"max_exit_delay_days"`
	TailLossTrades        int                       `json:"tail_loss_trades"`
	WorstNetReturnPct     float64                   `json:"worst_net_return_pct"`
	LiquidityPolicy       execution.LiquidityPolicy `json:"liquidity_policy"`
	ReferenceEquityCNY    float64                   `json:"reference_equity_cny"`
	LiquidityFiltered     int                       `json:"liquidity_filtered_signals"`
	ImpactedTrades        int                       `json:"impacted_trades"`
	AverageEntryImpactPct float64                   `json:"average_entry_impact_pct"`
	AverageExitImpactPct  float64                   `json:"average_exit_impact_pct"`
	MaxImpactPct          float64                   `json:"max_impact_pct"`
	MaxParticipationPct   float64                   `json:"max_participation_pct"`
	Limitations           []string                  `json:"limitations"`
}

type replaySignal struct {
	result    signal.SignalResult
	code      string
	idx       int
	signature string
}

type observation struct {
	date string
	code string
	ret  float64
	fold int
}

type accumulator struct {
	points []observation
}

func DefaultPath(rawDir, configured string) string {
	if configured == "" {
		configured = "validation/evidence.json"
	}
	if filepath.IsAbs(configured) {
		return configured
	}
	return filepath.Join(rawDir, configured)
}

func Load(path string) (*Store, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var store Store
	if err := json.Unmarshal(b, &store); err != nil {
		return nil, fmt.Errorf("解析历史验证结果: %w", err)
	}
	if store.Version != formatVersion {
		return nil, fmt.Errorf("历史验证版本 %d 不兼容，需要重新执行 validate build", store.Version)
	}
	if err := validateSamplingPolicy(store.Sampling); err != nil {
		return nil, err
	}
	if store.Stats == nil {
		return nil, fmt.Errorf("历史验证结果没有统计数据")
	}
	return &store, nil
}

func (s *Store) Save(path string) error {
	if s == nil {
		return fmt.Errorf("历史验证结果为空")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}

// Build replays all configured strategies chronologically. Only outcomes in
// the latter half of the available dates are used, split into chronological
// folds. This keeps the evidence separate from the earlier history used to
// establish the strategy and avoids attributing future data to a signal.
func Build(opts BuildOptions) (*Store, error) {
	if len(opts.CodeMap) == 0 {
		return nil, fmt.Errorf("没有可回放的日线数据")
	}
	if len(opts.Strategies) == 0 {
		return nil, fmt.Errorf("没有可回放的策略")
	}
	if opts.FoldCount <= 0 {
		opts.FoldCount = 3
	}
	if opts.Workers <= 0 {
		opts.Workers = runtime.GOMAXPROCS(0)
	}
	if err := (execution.CostModel{Commission: opts.Commission, Slippage: opts.Slippage}).Validate(); err != nil {
		return nil, fmt.Errorf("历史验证交易成本配置无效: %w", err)
	}
	if err := opts.Liquidity.Validate(); err != nil {
		return nil, fmt.Errorf("历史验证流动性配置无效: %w", err)
	}
	if opts.Liquidity.Enabled && (opts.Liquidity.MaxParticipationPct > 0 || opts.Liquidity.ImpactCoefficient > 0) &&
		(opts.ReferenceEquity <= 0 || math.IsNaN(opts.ReferenceEquity) || math.IsInf(opts.ReferenceEquity, 0)) {
		return nil, fmt.Errorf("历史验证启用成交占比或冲击成本时，参考权益必须为正有限数")
	}

	sortBarsByDate(opts.CodeMap)
	prepareStrategies(opts.Strategies, opts.CodeMap, opts.Fundamentals)
	statuses := market.BuildHistoricalStatus(opts.CodeMap)
	dates := replayDates(opts.CodeMap, opts.StartDate, opts.EndDate)
	marketDates := replayDates(opts.CodeMap, "", "")
	if len(dates) < 20 {
		return nil, fmt.Errorf("可回放交易日不足: %d", len(dates))
	}
	folds := buildFolds(dates, opts.FoldCount)
	foldByDate := make(map[string]int)
	foldStartIndex := make(map[int]int)
	foldEndDate := make(map[int]string)
	dateIndex := make(map[string]int, len(dates))
	for i, date := range dates {
		dateIndex[date] = i
	}
	for _, fold := range folds {
		foldStartIndex[fold.Number] = dateIndex[fold.StartDate]
		foldEndDate[fold.Number] = fold.EndDate
		for _, date := range dates {
			if date >= fold.StartDate && date <= fold.EndDate {
				foldByDate[date] = fold.Number
			}
		}
	}

	strategyFingerprint, err := StrategyFingerprintWithExecution(opts.Strategies, opts.Commission, opts.Slippage, opts.Liquidity, opts.ReferenceEquity)
	if err != nil {
		return nil, err
	}
	dataFingerprint := fingerprintDataWithStocks(opts.CodeMap, opts.StockInfos, opts.Fundamentals, opts.Moneyflows)
	accumulators := make(map[string]*accumulator)
	store := &Store{
		Version:             formatVersion,
		BuiltAt:             time.Now().Format(time.RFC3339),
		StartDate:           dates[0],
		EndDate:             dates[len(dates)-1],
		StrategyFingerprint: strategyFingerprint,
		DataFingerprint:     dataFingerprint,
		BuildFingerprint:    fingerprintBuild(strategyFingerprint, dataFingerprint, dates[0], dates[len(dates)-1], len(folds)),
		Folds:               folds,
		Sampling:            defaultSamplingPolicy(),
		Stats:               make(map[string]Stats),
		ExitReasonCounts:    make(map[string]int),
		LiquidityPolicy:     opts.Liquidity,
		ReferenceEquityCNY:  opts.ReferenceEquity,
		Limitations: []string{
			"历史回放仅在信号日后的下一市场交易日尝试开盘入场，涨停、停牌、高开超过3%或成交占比超过上限即放弃，使用真实价、手续费、滑点和流动性冲击成本。",
			"退出由收盘确认的初始止损、ATR移动止损和分周期时间止损触发，下一市场交易日开盘执行；跌停或停牌会持续保留卖单并记录延迟。",
			"上市日期和日均成交额使用信号日可知数据；当前股票名称不会倒推用于历史 ST 过滤，完整历史 ST 过滤仍需补充名称变更数据。",
			"样本外统计采用后半段历史的时间顺序分段；每折开头按最长持有期设置 embargo，跨越折尾的完整交易样本会被清除。",
			"同一股票和周期只保留持有窗口不重叠的信号；同日不同股票收益先等权聚类，再计算样本数、胜率、期望、波动和正收益折。",
			"历史股票池来自本地已保存日线，若缺少退市证券或历史成分股，仍可能存在幸存者偏差。",
			"盘中实时行情无法历史重建；回放改用次日涨停、高开和跌破前低的可成交规则。",
			"日期聚类降低了同日市场共振造成的伪样本量，但不同日期的多日持有收益仍可能受共同市场因子影响。",
		},
	}

	evaluator := signal.NewHistoricalEvaluator(opts.Strategies)
	independentUntil := make(map[string]string)
	for currentDateIndex, date := range dates {
		fold, outOfSample := foldByDate[date]
		if !outOfSample {
			continue
		}
		rows := replayDate(opts.CodeMap, opts.StockNames, evaluator, statuses[date], opts.Moneyflows, opts.Workers, date)
		results := make([]signal.SignalResult, len(rows))
		for i := range rows {
			results[i] = rows[i].result
		}
		signal.ApplyLiquidityPolicy(results, opts.CodeMap, signal.LiquidityContext{
			Policy: opts.Liquidity, StockInfos: opts.StockInfos, Fundamentals: opts.Fundamentals,
			ReferenceEquity: opts.ReferenceEquity, ApplyCurrentST: false,
		})
		signal.ApplyPositionPolicy(results, statuses[date])
		for i := range results {
			store.ScannedSignals++
			if results[i].Recommendation() != "买入" {
				if results[i].LiquidityApplied && !results[i].LiquidityEligible {
					store.LiquidityFiltered++
				}
				continue
			}
			horizon := results[i].Horizon
			if currentDateIndex < foldStartIndex[fold]+embargoTradingDays(horizon) {
				store.EmbargoedSignals++
				continue
			}
			bars := opts.CodeMap[rows[i].code]
			outcome := execution.SimulateManagedExit(bars, marketDates, date, horizon, execution.SimulationOptions{
				Costs: execution.CostModel{Commission: opts.Commission, Slippage: opts.Slippage}, MaxEntryGapPct: 3,
				Liquidity: opts.Liquidity, OrderValueCNY: opts.ReferenceEquity * results[i].PositionPct / 100,
			})
			if !outcome.EntryFeasible {
				store.SkippedTrades++
				continue
			}
			if !outcome.Completed {
				store.PurgedSignals++
				if outcome.Triggered {
					store.UnresolvedExits++
				}
				continue
			}
			exitDate := outcome.ExitDate
			if exitDate > foldEndDate[fold] || (opts.EndDate != "" && exitDate > opts.EndDate) {
				store.PurgedSignals++
				continue
			}
			independenceKey := rows[i].code + "|" + string(horizon)
			if until := independentUntil[independenceKey]; until != "" && date < until {
				store.OverlappingSignals++
				continue
			}
			independentUntil[independenceKey] = exitDate
			store.FeasibleTrades++
			if outcome.EntryImpactRate > 0 || outcome.ExitImpactRate > 0 {
				store.ImpactedTrades++
			}
			store.AverageEntryImpactPct += outcome.EntryImpactRate * 100
			store.AverageExitImpactPct += outcome.ExitImpactRate * 100
			store.MaxImpactPct = math.Max(store.MaxImpactPct, math.Max(outcome.EntryImpactRate, outcome.ExitImpactRate)*100)
			store.MaxParticipationPct = math.Max(store.MaxParticipationPct, math.Max(outcome.EntryParticipationPct, outcome.ExitParticipationPct))
			store.ExitReasonCounts[string(outcome.Reason)]++
			if outcome.DelayDays > 0 {
				store.DelayedExitTrades++
				store.ExitDelayDays += outcome.DelayDays
				if outcome.DelayDays > store.MaxExitDelayDays {
					store.MaxExitDelayDays = outcome.DelayDays
				}
			}
			if outcome.TailLoss {
				store.TailLossTrades++
			}
			ret := outcome.Returns.NetReturnPct
			if store.FeasibleTrades == 1 || ret < store.WorstNetReturnPct {
				store.WorstNetReturnPct = ret
			}
			for _, key := range statKeys(horizon, statuses[date], rows[i].signature) {
				acc := accumulators[key]
				if acc == nil {
					acc = &accumulator{}
					accumulators[key] = acc
				}
				acc.points = append(acc.points, observation{date: date, code: results[i].Code, ret: ret, fold: fold})
			}
		}
	}
	for key, acc := range accumulators {
		store.Stats[key] = summarize(acc.points, len(folds))
	}
	if store.FeasibleTrades > 0 {
		store.AverageEntryImpactPct /= float64(store.FeasibleTrades)
		store.AverageExitImpactPct /= float64(store.FeasibleTrades)
	}
	return store, nil
}

func defaultSamplingPolicy() SamplingPolicy {
	rules := make([]HorizonSamplingRule, 0, len(strategy.HorizonOrder()))
	for _, horizon := range strategy.HorizonOrder() {
		holding := horizonDays(horizon)
		rules = append(rules, HorizonSamplingRule{
			Horizon:             horizon,
			HoldingTradingDays:  holding,
			CooldownTradingDays: holding,
			EmbargoTradingDays:  embargoTradingDays(horizon),
		})
	}
	return SamplingPolicy{
		ClusterUnit:     "signal_date_equal_weight",
		IndependenceKey: "stock+horizon",
		PurgeAtFoldEnd:  true,
		HorizonRules:    rules,
	}
}

func validateSamplingPolicy(got SamplingPolicy) error {
	want := defaultSamplingPolicy()
	if got.ClusterUnit != want.ClusterUnit || got.IndependenceKey != want.IndependenceKey ||
		got.PurgeAtFoldEnd != want.PurgeAtFoldEnd || len(got.HorizonRules) != len(want.HorizonRules) {
		return fmt.Errorf("历史验证的独立采样规则不兼容，需要重新执行 validate build")
	}
	for i := range want.HorizonRules {
		if got.HorizonRules[i] != want.HorizonRules[i] {
			return fmt.Errorf("历史验证的独立采样规则不兼容，需要重新执行 validate build")
		}
	}
	return nil
}

func embargoTradingDays(horizon strategy.Horizon) int {
	days := horizonDays(horizon)
	if days < 0 {
		return 0
	}
	return days
}

func sortBarsByDate(codeMap map[string][]data.DailyBar) {
	for _, bars := range codeMap {
		sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
	}
}

func prepareStrategies(strategies []strategy.Strategy, barsMap map[string][]data.DailyBar, fundamentals *data.FundamentalStore) {
	for _, s := range strategies {
		if u, ok := s.(strategy.HistoricalUniverseUser); ok {
			u.SetHistoricalUniverse(barsMap)
		} else if u, ok := s.(strategy.UniverseUser); ok {
			u.SetUniverse(barsMap)
		}
		if f, ok := s.(strategy.FundStoreUser); ok {
			f.SetFundStore(fundamentals)
		}
	}
}

func replayDates(codeMap map[string][]data.DailyBar, start, end string) []string {
	seen := make(map[string]bool)
	for _, bars := range codeMap {
		for _, bar := range bars {
			if bar.TradeDate != "" && (start == "" || bar.TradeDate >= start) && (end == "" || bar.TradeDate <= end) {
				seen[bar.TradeDate] = true
			}
		}
	}
	dates := make([]string, 0, len(seen))
	for date := range seen {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	return dates
}

func buildFolds(dates []string, count int) []Fold {
	if count < 1 {
		count = 1
	}
	start := len(dates) / 2
	remaining := len(dates) - start
	if remaining < count {
		count = 1
	}
	folds := make([]Fold, 0, count)
	for i := 0; i < count; i++ {
		from := start + i*remaining/count
		to := start + (i+1)*remaining/count - 1
		if i == count-1 {
			to = len(dates) - 1
		}
		if from <= to {
			folds = append(folds, Fold{Number: i + 1, StartDate: dates[from], EndDate: dates[to]})
		}
	}
	return folds
}

// replayDate keeps the live-sized candidate set for one trading day only. The
// previous all-history materialization retained millions of detailed strategy
// maps and made a full replay memory-bound on otherwise capable machines.
func replayDate(codeMap map[string][]data.DailyBar, names map[string]string, evaluator *signal.HistoricalEvaluator, marketStatus *market.MarketStatus, moneyflows *data.MoneyflowStore, workers int, date string) []replaySignal {
	type job struct {
		code string
		bars []data.DailyBar
	}
	type result struct {
		rows []replaySignal
	}
	jobs := make(chan job)
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]replaySignal, 0)
			for item := range jobs {
				idx := sort.Search(len(item.bars), func(i int) bool { return item.bars[i].TradeDate >= date })
				if idx >= len(item.bars) || item.bars[idx].TradeDate != date {
					continue
				}
				name := item.code
				if names[item.code] != "" {
					name = names[item.code]
				}
				for _, candidate := range evaluator.Evaluate(item.bars, idx, name, marketStatus, moneyflows) {
					if candidate.Recommendation() != "买入" {
						continue
					}
					signature := buySignature(candidate)
					candidate.Strategies = nil
					candidate.GroupScores = nil
					candidate.Reasons = nil
					local = append(local, replaySignal{result: candidate, code: item.code, idx: idx, signature: signature})
				}
			}
			results <- result{rows: local}
		}()
	}
	go func() {
		for code, bars := range codeMap {
			jobs <- job{code: code, bars: bars}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	merged := make([]replaySignal, 0)
	for item := range results {
		merged = append(merged, item.rows...)
	}
	return merged
}

func feasibleReturn(bars []data.DailyBar, signalIdx int, horizon strategy.Horizon, commission, slippage float64, endDate string) (float64, bool) {
	if signalIdx < 0 || signalIdx >= len(bars) {
		return 0, false
	}
	marketDates := make([]string, 0, len(bars))
	for _, bar := range bars {
		marketDates = append(marketDates, bar.TradeDate)
	}
	outcome := execution.SimulateManagedExit(bars, marketDates, bars[signalIdx].TradeDate, horizon, execution.SimulationOptions{
		Costs: execution.CostModel{Commission: commission, Slippage: slippage}, MaxEntryGapPct: 3,
	})
	if !outcome.Completed || (endDate != "" && outcome.ExitDate > endDate) {
		return 0, false
	}
	return outcome.Returns.NetReturnPct, true
}

func horizonDays(h strategy.Horizon) int {
	return execution.DefaultExitPolicy(h).MaxHoldingDays
}

func statKeys(h strategy.Horizon, status *market.MarketStatus, signature string) []string {
	sentiment := "未知"
	if status != nil && status.Sentiment != "" {
		sentiment = status.Sentiment
	}
	return []string{
		"exact|" + string(h) + "|" + sentiment + "|" + signature,
		"signature|" + string(h) + "|" + signature,
		"regime|" + string(h) + "|" + sentiment,
		"horizon|" + string(h),
	}
}

func buySignature(r signal.SignalResult) string {
	names := make([]string, 0, r.BuyCount)
	for name, detail := range r.Strategies {
		if detail.Signal == strategy.Buy {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, "+")
}

func summarize(points []observation, possibleFolds int) Stats {
	if len(points) == 0 {
		return Stats{}
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].date == points[j].date {
			return points[i].code < points[j].code
		}
		return points[i].date < points[j].date
	})
	clustered := clusterBySignalDate(points)
	stats := Stats{Trades: len(points), Samples: len(clustered)}
	var sum, wins, losses float64
	foldSums := make(map[int]float64)
	foldCounts := make(map[int]int)
	for _, point := range clustered {
		sum += point.ret
		foldSums[point.fold] += point.ret
		foldCounts[point.fold]++
		if point.ret > 0 {
			stats.Wins++
			wins += point.ret
		} else if point.ret < 0 {
			losses += point.ret
		}
	}
	stats.WinRatePct = float64(stats.Wins) / float64(stats.Samples) * 100
	stats.ExpectedReturnPct = sum / float64(stats.Samples)
	if stats.Wins > 0 {
		stats.AverageWinPct = wins / float64(stats.Wins)
	}
	lossCount := stats.Samples - stats.Wins
	if lossCount > 0 {
		stats.AverageLossPct = losses / float64(lossCount)
	}
	var squared float64
	for _, point := range clustered {
		delta := point.ret - stats.ExpectedReturnPct
		squared += delta * delta
	}
	if stats.Samples > 1 {
		stats.VolatilityPct = math.Sqrt(squared / float64(stats.Samples-1))
	}
	equity, peak := 1.0, 1.0
	for _, point := range clustered {
		equity *= 1 + point.ret/100
		if equity > peak {
			peak = equity
		}
		if peak > 0 {
			drawdown := (equity/peak - 1) * 100
			if drawdown < stats.MaxDrawdownPct {
				stats.MaxDrawdownPct = drawdown
			}
		}
	}
	for fold, count := range foldCounts {
		if count == 0 {
			continue
		}
		stats.FoldCount++
		if foldSums[fold]/float64(count) > 0 {
			stats.PositiveFolds++
		}
	}
	if stats.FoldCount > possibleFolds {
		stats.FoldCount = possibleFolds
	}
	return stats
}

func clusterBySignalDate(points []observation) []observation {
	if len(points) == 0 {
		return nil
	}
	clustered := make([]observation, 0, len(points))
	for i := 0; i < len(points); {
		date := points[i].date
		fold := points[i].fold
		sum := 0.0
		count := 0
		for i < len(points) && points[i].date == date {
			sum += points[i].ret
			count++
			i++
		}
		clustered = append(clustered, observation{date: date, ret: sum / float64(count), fold: fold})
	}
	return clustered
}

type evidenceMatch struct {
	stats            Stats
	basis            string
	strategySpecific bool
	prior            Stats
	priorBasis       string
	hasPrior         bool
}

// Annotate attaches strategy-specific evidence to each candidate. Regime and
// horizon aggregates may shrink that evidence, but can never independently
// qualify a formal recommendation.
func Annotate(results []signal.SignalResult, store *Store, policy Policy, enforce bool) []signal.SignalResult {
	if store == nil {
		return results
	}
	policy = normalizedPolicy(policy)
	for i := range results {
		if results[i].Recommendation() != "买入" {
			continue
		}
		match, ok := store.lookup(results[i])
		evidence := &signal.HistoricalEvidence{Available: ok, Enforced: enforce}
		if !ok {
			evidence.Status = "无历史样本"
			results[i].HistoricalEvidence = evidence
			continue
		}
		stats := match.stats
		priorWeight := effectivePriorWeight(policy.PriorSamples, match.prior, match.hasPrior)
		evidence.Basis = match.basis
		evidence.StrategySpecific = match.strategySpecific
		evidence.Trades = stats.Trades
		evidence.Samples = stats.Samples
		evidence.Wins = stats.Wins
		if match.hasPrior {
			evidence.PriorBasis = match.priorBasis
			evidence.PriorSamples = match.prior.Samples
		}
		evidence.PriorWeight = priorWeight
		evidence.WinRatePct = shrinkWinRate(stats, priorWeight, match.prior, match.hasPrior)
		evidence.ExpectedReturnPct = shrinkExpectedReturn(stats, priorWeight, match.prior, match.hasPrior)
		evidence.AverageWinPct = stats.AverageWinPct
		evidence.AverageLossPct = stats.AverageLossPct
		evidence.VolatilityPct = stats.VolatilityPct
		evidence.MaxDrawdownPct = stats.MaxDrawdownPct
		evidence.PositiveFolds = stats.PositiveFolds
		evidence.FoldCount = stats.FoldCount
		evidence.Eligible = match.strategySpecific &&
			stats.Samples >= policy.MinSamples &&
			stats.PositiveFolds >= policy.MinPositiveFolds &&
			stats.ExpectedReturnPct > policy.MinExpectedReturnPct &&
			evidence.ExpectedReturnPct > policy.MinExpectedReturnPct
		if evidence.Eligible {
			evidence.Status = "历史验证通过"
		} else if !match.strategySpecific {
			evidence.Status = "仅有周期先验，不能用于正式资格"
		} else {
			evidence.Status = "历史验证不足"
		}
		results[i].HistoricalEvidence = evidence
	}
	return results
}

// MarkUnavailable attaches an explicit failed evidence result. When enforcement
// is enabled this keeps formal recommendations fail-closed if the evidence file
// is missing, stale, or incompatible with the running strategy configuration.
func MarkUnavailable(results []signal.SignalResult, status string, enforce bool) []signal.SignalResult {
	if status == "" {
		status = "历史验证不可用"
	}
	for i := range results {
		if results[i].Recommendation() != "买入" {
			continue
		}
		results[i].HistoricalEvidence = &signal.HistoricalEvidence{
			Available: false,
			Eligible:  false,
			Enforced:  enforce,
			Status:    status,
		}
	}
	return results
}

// Allocate applies evidence-based risk-budget weights to formal buys. It
// never raises a strategy's existing position cap and therefore never forces a
// fully invested portfolio.
func Allocate(results []signal.SignalResult) []signal.SignalResult {
	type weighted struct {
		idx   int
		score float64
	}
	items := make([]weighted, 0)
	var total float64
	for i := range results {
		e := results[i].HistoricalEvidence
		if results[i].Recommendation() != "买入" || e == nil || !e.Eligible {
			continue
		}
		denom := e.VolatilityPct
		if denom < 0.1 {
			denom = 0.1
		}
		score := math.Max(0, e.ExpectedReturnPct) * math.Sqrt(float64(e.Samples)) * float64(maxInt(1, e.PositiveFolds)) / denom
		if score <= 0 {
			continue
		}
		items = append(items, weighted{idx: i, score: score})
		total += score
	}
	if total == 0 {
		return results
	}
	for _, item := range items {
		weight := item.score / total * 100
		results[item.idx].HistoricalEvidence.SuggestedWeightPct = weight
		if results[item.idx].PositionPct <= 0 || weight < results[item.idx].PositionPct {
			results[item.idx].PositionPct = weight
		}
	}
	return results
}

func (s *Store) lookup(r signal.SignalResult) (evidenceMatch, bool) {
	sentiment := r.MarketSentiment
	if sentiment == "" {
		sentiment = "未知"
	}
	horizon := string(r.Horizon)
	signature := buySignature(r)
	exact, hasExact := s.Stats["exact|"+horizon+"|"+sentiment+"|"+signature]
	signatureStats, hasSignature := s.Stats["signature|"+horizon+"|"+signature]
	regime, hasRegime := s.Stats["regime|"+horizon+"|"+sentiment]
	horizonStats, hasHorizon := s.Stats["horizon|"+horizon]

	withPrior := func(match evidenceMatch) evidenceMatch {
		if hasRegime {
			match.prior, match.priorBasis, match.hasPrior = regime, "同周期 + 同市场状态", true
		} else if hasHorizon {
			match.prior, match.priorBasis, match.hasPrior = horizonStats, "同周期", true
		}
		return match
	}
	switch {
	case hasExact:
		return withPrior(evidenceMatch{stats: exact, basis: "同策略组合 + 同市场状态", strategySpecific: true}), true
	case hasSignature:
		return withPrior(evidenceMatch{stats: signatureStats, basis: "同策略组合", strategySpecific: true}), true
	case hasRegime:
		match := evidenceMatch{stats: regime, basis: "同周期 + 同市场状态（仅先验）"}
		if hasHorizon {
			match.prior, match.priorBasis, match.hasPrior = horizonStats, "同周期", true
		}
		return match, true
	case hasHorizon:
		return evidenceMatch{stats: horizonStats, basis: "同周期（仅先验）"}, true
	default:
		return evidenceMatch{}, false
	}
}

func normalizedPolicy(policy Policy) Policy {
	defaults := DefaultPolicy()
	if policy.MinSamples <= 0 {
		policy.MinSamples = defaults.MinSamples
	}
	if policy.MinPositiveFolds <= 0 {
		policy.MinPositiveFolds = defaults.MinPositiveFolds
	}
	if policy.PriorSamples <= 0 {
		policy.PriorSamples = defaults.PriorSamples
	}
	return policy
}

func shrinkWinRate(stats Stats, priorWeight float64, prior Stats, hasPrior bool) float64 {
	priorRate := 0.5
	if hasPrior && prior.Samples > 0 {
		priorRate = float64(prior.Wins) / float64(prior.Samples)
	}
	return (float64(stats.Wins) + priorWeight*priorRate) / (float64(stats.Samples) + priorWeight) * 100
}

func effectivePriorWeight(configured float64, prior Stats, hasPrior bool) float64 {
	if !hasPrior {
		return configured
	}
	available := float64(prior.Samples)
	if available < configured {
		return available
	}
	return configured
}

func shrinkExpectedReturn(stats Stats, priorWeight float64, prior Stats, hasPrior bool) float64 {
	priorReturn := 0.0
	if hasPrior {
		priorReturn = prior.ExpectedReturnPct
	}
	return (stats.ExpectedReturnPct*float64(stats.Samples) + priorReturn*priorWeight) /
		(float64(stats.Samples) + priorWeight)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
