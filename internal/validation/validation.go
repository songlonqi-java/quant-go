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
	"quant/internal/market"
	"quant/internal/signal"
	"quant/internal/strategy"
)

const formatVersion = 1

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
	Strategies   []strategy.Strategy
	Fundamentals *data.FundamentalStore
	Moneyflows   *data.MoneyflowStore
	Commission   float64
	Slippage     float64
	Workers      int
	StartDate    string
	EndDate      string
	FoldCount    int
}

type Fold struct {
	Number    int    `json:"number"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

type Stats struct {
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

type Store struct {
	Version        int              `json:"version"`
	BuiltAt        string           `json:"built_at"`
	StartDate      string           `json:"start_date"`
	EndDate        string           `json:"end_date"`
	Folds          []Fold           `json:"folds"`
	Stats          map[string]Stats `json:"stats"`
	ScannedSignals int              `json:"scanned_signals"`
	FeasibleTrades int              `json:"feasible_trades"`
	SkippedTrades  int              `json:"skipped_trades"`
	Limitations    []string         `json:"limitations"`
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
	if opts.Commission < 0 || opts.Slippage < 0 {
		return nil, fmt.Errorf("手续费和滑点不能为负")
	}

	sortBarsByDate(opts.CodeMap)
	prepareStrategies(opts.Strategies, opts.CodeMap, opts.Fundamentals)
	statuses := market.BuildHistoricalStatus(opts.CodeMap)
	dates := replayDates(opts.CodeMap, opts.StartDate, opts.EndDate)
	if len(dates) < 20 {
		return nil, fmt.Errorf("可回放交易日不足: %d", len(dates))
	}
	folds := buildFolds(dates, opts.FoldCount)
	foldByDate := make(map[string]int)
	for _, fold := range folds {
		for _, date := range dates {
			if date >= fold.StartDate && date <= fold.EndDate {
				foldByDate[date] = fold.Number
			}
		}
	}

	accumulators := make(map[string]*accumulator)
	store := &Store{
		Version:   formatVersion,
		BuiltAt:   time.Now().Format(time.RFC3339),
		StartDate: dates[0],
		EndDate:   dates[len(dates)-1],
		Folds:     folds,
		Stats:     make(map[string]Stats),
		Limitations: []string{
			"历史回放按信号日后首个可交易开盘价入场，使用真实价、手续费和滑点。",
			"样本外统计采用后半段历史的时间顺序分段；策略参数在回放前固定。",
			"历史股票池来自本地已保存日线，若缺少退市证券或历史成分股，仍可能存在幸存者偏差。",
			"盘中实时行情无法历史重建；回放改用次日涨停、高开和跌破前低的可成交规则。",
			"最大回撤以各交易日信号等权收益构造，单笔胜率和期望收益仍按全部可成交信号统计。",
		},
	}

	evaluator := signal.NewHistoricalEvaluator(opts.Strategies)
	for _, date := range dates {
		fold, outOfSample := foldByDate[date]
		if !outOfSample {
			continue
		}
		rows := replayDate(opts.CodeMap, opts.StockNames, evaluator, statuses[date], opts.Moneyflows, opts.Workers, date)
		results := make([]signal.SignalResult, len(rows))
		for i := range rows {
			results[i] = rows[i].result
		}
		signal.ApplyPositionPolicy(results, statuses[date])
		for i := range results {
			store.ScannedSignals++
			if results[i].Recommendation() != "买入" {
				continue
			}
			ret, ok := feasibleReturn(opts.CodeMap[rows[i].code], rows[i].idx, results[i].Horizon, opts.Commission, opts.Slippage, opts.EndDate)
			if !ok {
				store.SkippedTrades++
				continue
			}
			store.FeasibleTrades++
			for _, key := range statKeys(results[i].Horizon, statuses[date], rows[i].signature) {
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
	return store, nil
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
					if candidate.BuyCount <= candidate.SellCount || candidate.TotalScore <= 0 {
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
	entryIdx := signalIdx + 1
	if entryIdx >= len(bars) {
		return 0, false
	}
	offset := horizonDays(horizon) - 1
	exitIdx := entryIdx + offset
	if exitIdx >= len(bars) {
		return 0, false
	}
	if endDate != "" && bars[exitIdx].TradeDate > endDate {
		return 0, false
	}
	signalBar := bars[signalIdx]
	entryBar := bars[entryIdx]
	entry := entryBar.TradeOpen()
	if entry <= 0 || entryBar.Vol <= 0 || entryBar.IsLimitUpOpen() {
		return 0, false
	}
	if prevClose := signalBar.TradeClose(); prevClose > 0 && entry > prevClose*1.03 {
		return 0, false
	}
	if prevLow := signalBar.TradeLow(); prevLow > 0 && entryBar.TradeLow() < prevLow {
		return 0, false
	}
	exit := bars[exitIdx].TradeClose()
	if exit <= 0 {
		return 0, false
	}
	entryCost := entry * (1 + slippage) * (1 + commission)
	exitValue := exit * (1 - slippage) * (1 - commission)
	if entryCost <= 0 {
		return 0, false
	}
	return (exitValue/entryCost - 1) * 100, true
}

func horizonDays(h strategy.Horizon) int {
	switch h {
	case strategy.HorizonMid:
		return 20
	case strategy.HorizonLong:
		return 120
	default:
		return 5
	}
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
	stats := Stats{Samples: len(points)}
	var sum, wins, losses float64
	foldSums := make(map[int]float64)
	foldCounts := make(map[int]int)
	for _, point := range points {
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
	for _, point := range points {
		delta := point.ret - stats.ExpectedReturnPct
		squared += delta * delta
	}
	if stats.Samples > 1 {
		stats.VolatilityPct = math.Sqrt(squared / float64(stats.Samples-1))
	}
	dailyReturns := make(map[string][]float64)
	for _, point := range points {
		dailyReturns[point.date] = append(dailyReturns[point.date], point.ret)
	}
	days := make([]string, 0, len(dailyReturns))
	for date := range dailyReturns {
		days = append(days, date)
	}
	sort.Strings(days)
	equity, peak := 1.0, 1.0
	for _, date := range days {
		returns := dailyReturns[date]
		var dailyReturn float64
		for _, ret := range returns {
			dailyReturn += ret
		}
		dailyReturn /= float64(len(returns))
		equity *= 1 + dailyReturn/100
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

// Annotate attaches the strongest available evidence to each candidate. It
// falls back from an exact strategy+regime match to a broader horizon match and
// makes that fallback explicit in Basis.
func Annotate(results []signal.SignalResult, store *Store, policy Policy, enforce bool) []signal.SignalResult {
	if store == nil {
		return results
	}
	policy = normalizedPolicy(policy)
	for i := range results {
		if results[i].BuyCount <= results[i].SellCount || results[i].TotalScore <= 0 {
			continue
		}
		stats, basis, ok := store.lookup(results[i])
		evidence := &signal.HistoricalEvidence{Available: ok, Enforced: enforce}
		if !ok {
			evidence.Status = "无历史样本"
			results[i].HistoricalEvidence = evidence
			continue
		}
		evidence.Basis = basis
		evidence.Samples = stats.Samples
		evidence.Wins = stats.Wins
		evidence.WinRatePct = shrinkWinRate(stats, policy.PriorSamples)
		evidence.ExpectedReturnPct = shrinkExpectedReturn(stats, policy.PriorSamples)
		evidence.AverageWinPct = stats.AverageWinPct
		evidence.AverageLossPct = stats.AverageLossPct
		evidence.VolatilityPct = stats.VolatilityPct
		evidence.MaxDrawdownPct = stats.MaxDrawdownPct
		evidence.PositiveFolds = stats.PositiveFolds
		evidence.FoldCount = stats.FoldCount
		evidence.Eligible = stats.Samples >= policy.MinSamples &&
			stats.PositiveFolds >= policy.MinPositiveFolds &&
			evidence.ExpectedReturnPct > policy.MinExpectedReturnPct
		if evidence.Eligible {
			evidence.Status = "历史验证通过"
		} else {
			evidence.Status = "历史验证不足"
		}
		results[i].HistoricalEvidence = evidence
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

func (s *Store) lookup(r signal.SignalResult) (Stats, string, bool) {
	sentiment := r.MarketSentiment
	if sentiment == "" {
		sentiment = "未知"
	}
	for _, candidate := range []struct {
		key   string
		basis string
	}{
		{"exact|" + string(r.Horizon) + "|" + sentiment + "|" + buySignature(r), "同策略组合 + 同市场状态"},
		{"signature|" + string(r.Horizon) + "|" + buySignature(r), "同策略组合"},
		{"regime|" + string(r.Horizon) + "|" + sentiment, "同周期 + 同市场状态"},
		{"horizon|" + string(r.Horizon), "同周期"},
	} {
		if stats, ok := s.Stats[candidate.key]; ok {
			return stats, candidate.basis, true
		}
	}
	return Stats{}, "", false
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

func shrinkWinRate(stats Stats, prior float64) float64 {
	return (float64(stats.Wins) + prior*0.5) / (float64(stats.Samples) + prior) * 100
}

func shrinkExpectedReturn(stats Stats, prior float64) float64 {
	return stats.ExpectedReturnPct * float64(stats.Samples) / (float64(stats.Samples) + prior)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
