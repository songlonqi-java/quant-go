package signalworkflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"quant/internal/config"
	"quant/internal/data"
	"quant/internal/dataset"
	"quant/internal/forward"
	"quant/internal/market"
	"quant/internal/news"
	"quant/internal/portfolio"
	"quant/internal/realtime"
	"quant/internal/sector"
	signals "quant/internal/signal"
	"quant/internal/strategy"
	"quant/internal/validation"
)

type Options struct {
	Config              *config.Config
	StrategyNames       []string
	TopN                int
	WatchN              int
	Realtime            bool
	MarketRealtime      bool
	MarketRefreshWindow time.Duration
	PortfolioPath       string
	PortfolioLedger     *portfolio.Ledger
	ForwardDir          string
	NewsAnalyzer        NewsAnalyzer
	RealtimeProvider    realtime.Provider
}

// NewsAnalyzer is the seam for the optional external news source. Production
// uses the package implementation; callers can provide a deterministic
// adapter when running the workflow in another environment or in tests.
type NewsAnalyzer func(context.Context, string, int) (*news.NewsSummary, error)

type Result struct {
	StrategyNames       []string
	Dataset             *dataset.Dataset
	MarketStatus        *market.MarketStatus
	IntradayMarket      *market.IntradayStatus
	NewsSummary         *news.NewsSummary
	PortfolioSummary    *portfolio.Summary
	PositionDecision    signals.PositionDecision
	SectorReport        *sector.Report
	Signals             []signals.SignalResult
	Watchlist           []signals.SignalResult
	RealtimeLoaded      int
	NewsErr             error
	RealtimeErr         error
	MarketRealtimeErr   error
	MarketRealtimeStats realtime.FetchStats
	ForwardErr          error
	SectorErr           error
	ValidationErr       error
	ValidationStore     *validation.Store
	PriceQuality        data.PriceDataQuality
}

func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("缺少配置")
	}
	cfg := opts.Config
	liquidityPolicy := cfg.Liquidity.Policy()
	if err := liquidityPolicy.Validate(); err != nil {
		return nil, fmt.Errorf("流动性配置无效: %w", err)
	}
	if opts.PortfolioPath == "" {
		opts.PortfolioPath = "portfolio.yaml"
	}
	if opts.ForwardDir == "" {
		opts.ForwardDir = cfg.Data.RawDir + "/../forward_test"
	}

	defaultNames := cfg.Signal.DefaultStrategies
	if len(opts.StrategyNames) == 0 {
		defaultNames = strategy.DailyStrategyNames(defaultNames)
	}
	selectedStrategies, strategyNames, err := selectStrategies(opts.StrategyNames, defaultNames)
	if err != nil {
		return nil, err
	}
	ds, err := dataset.Load(dataset.LoadOptions{
		RawDir:           cfg.Data.RawDir,
		LatestOnly:       true,
		FilterST:         true,
		MinMarketCap:     cfg.Fetch.MinMarketCap,
		LoadFundamentals: usesFundamentals(selectedStrategies) || (liquidityPolicy.Enabled && liquidityPolicy.MinTurnoverRatePct > 0),
	})
	if err != nil {
		return nil, err
	}
	injectFundamentals(selectedStrategies, ds.Fundamentals)

	priceQuality := ds.PriceQuality(2)
	if !priceQuality.HasCompleteRawPrices() {
		selectedStrategies = filterStrategies(selectedStrategies, map[string]bool{"limit_up": true})
		strategyNames = strategyNamesFrom(selectedStrategies)
		if len(selectedStrategies) == 0 {
			return nil, fmt.Errorf("真实价字段缺失后没有可用策略")
		}
	}

	topN := opts.TopN
	if topN == 0 {
		topN = cfg.Signal.TopN
	}

	result := &Result{
		StrategyNames: strategyNames,
		Dataset:       ds,
		PriceQuality:  priceQuality,
	}
	result.MarketStatus = market.Analyze(ds.ActiveBars())
	result.NewsSummary, result.NewsErr = analyzeNews(ctx, cfg.Data.RawDir, opts.NewsAnalyzer)
	result.SectorReport, result.SectorErr = sector.LoadReport(cfg.Data.RawDir, ds.LatestDate)

	portfolioSummary, err := loadPortfolioSummary(opts.PortfolioPath, opts.PortfolioLedger, ds)
	if err != nil {
		return nil, err
	}
	result.PortfolioSummary = portfolioSummary

	var marketQuoteMap map[string]realtime.Quote
	if opts.MarketRealtime {
		quotes, stats, err := fetchMarketRealtime(opts.RealtimeProvider, ds.CodeMap, opts.MarketRefreshWindow)
		result.MarketRealtimeStats = stats
		result.IntradayMarket = market.AnalyzeIntraday(quotes, len(ds.CodeMap), ds.StkLimits)
		if err != nil {
			result.MarketRealtimeErr = err
		} else if result.IntradayMarket.AsOf != realtime.ChinaTradeDate(time.Now()) {
			result.MarketRealtimeErr = fmt.Errorf("实时行情日期%s与当前交易日%s不一致", result.IntradayMarket.AsOf, realtime.ChinaTradeDate(time.Now()))
		} else if !result.IntradayMarket.Complete {
			result.MarketRealtimeErr = fmt.Errorf("实时行情覆盖率%.1f%%不足，未用于候选盘中校验", result.IntradayMarket.CoveragePct)
		} else {
			marketQuoteMap = realtime.MapByCode(quotes)
		}
	}

	allSignals := signals.GenerateWithContextAndMoneyflow(ds.CodeMap, selectedStrategies, 0, ds.StockNames, result.MarketStatus, ds.Moneyflows)
	signals.ApplyLiquidityPolicy(allSignals, ds.CodeMap, signals.LiquidityContext{
		Policy: liquidityPolicy, StockInfos: ds.StockInfos, Fundamentals: ds.Fundamentals,
		ReferenceEquity: cfg.Portfolio.Normalized(cfg.Backtest.InitialCapital).ReferenceEquity,
		ApplyCurrentST:  true,
	})
	memberships, membershipErr := sector.LoadIndustryMemberships(cfg.Data.RawDir)
	if membershipErr == nil {
		signals.ApplySectorContext(allSignals, result.SectorReport, memberships)
	} else if result.SectorErr == nil {
		result.SectorErr = membershipErr
	}
	candidatePool := signals.SelectCandidatePool(allSignals, topN)
	watchPool := signals.SelectWatchlist(allSignals, candidatePool, opts.WatchN*3)
	if cfg.Validation.Enabled {
		path := validation.DefaultPath(cfg.Data.RawDir, cfg.Validation.Path)
		store, err := validation.Load(path)
		if err != nil {
			if os.IsNotExist(err) {
				result.ValidationErr = fmt.Errorf("尚未构建历史验证证据（运行 go-quant validate build）")
			} else {
				result.ValidationErr = err
			}
		} else {
			if err := store.ValidateCompatibilityWithExecution(
				selectedStrategies, cfg.Backtest.Commission, cfg.Backtest.Slippage,
				liquidityPolicy, cfg.Portfolio.Normalized(cfg.Backtest.InitialCapital).ReferenceEquity,
				ds.AllCodeMap, ds.StockInfos, ds.Fundamentals, ds.Moneyflows,
			); err != nil {
				result.ValidationErr = err
			} else {
				policy := validation.Policy{
					MinSamples:           cfg.Validation.MinSamples,
					MinPositiveFolds:     cfg.Validation.MinPositiveFolds,
					MinExpectedReturnPct: cfg.Validation.MinExpectedReturn,
					PriorSamples:         cfg.Validation.PriorSamples,
				}
				candidatePool = validation.Annotate(candidatePool, store, policy, true)
				watchPool = validation.Annotate(watchPool, store, policy, false)
				result.ValidationStore = store
			}
		}
		if result.ValidationErr != nil {
			candidatePool = validation.MarkUnavailable(candidatePool, result.ValidationErr.Error(), true)
			watchPool = validation.MarkUnavailable(watchPool, result.ValidationErr.Error(), false)
		}
	}

	if opts.Realtime {
		quoteMap := marketQuoteMap
		var err error
		if len(quoteMap) == 0 {
			quoteMap, err = fetchRealtimeQuotes(opts.RealtimeProvider, realtimeTargets(candidatePool, watchPool), result.PortfolioSummary)
		}
		if err != nil {
			result.RealtimeErr = err
		} else if len(quoteMap) > 0 {
			signals.ApplyRealtimeQuotes(candidatePool, quoteMap, ds.StkLimits)
			signals.ApplyRealtimeQuotes(watchPool, quoteMap, ds.StkLimits)
			portfolio.ApplyRealtimeQuotes(result.PortfolioSummary, quoteMap)
			result.RealtimeLoaded = len(quoteMap)
		}
	}

	result.PositionDecision = signals.ApplyPositionPolicy(candidatePool, result.MarketStatus)
	candidatePool = validation.Allocate(candidatePool)
	portfolioCfg := cfg.Portfolio.Normalized(cfg.Backtest.InitialCapital)
	portfolioBudget := buildPortfolioBudget(portfolioCfg, result.PortfolioSummary, memberships, ds.LatestDate, result.PositionDecision)
	portfolioBudget.MaxBuysPerHorizon = topN
	allocation := signals.ApplyPortfolioBudget(candidatePool, portfolioBudget)
	signals.ReconcilePositionDecision(&result.PositionDecision, allocation)
	result.Signals = signals.LimitByRecommendation(candidatePool, topN)
	watchSource := append(candidatePool, watchPool...)
	result.Watchlist = signals.SelectWatchlist(watchSource, result.Signals, opts.WatchN)

	forwardSignals := result.Signals
	if len(forwardSignals) == 0 && shouldRecordCash(result.PositionDecision) {
		forwardSignals = candidatePool
	}
	if len(forwardSignals) > 0 {
		result.ForwardErr = forward.RecordWithDecision(opts.ForwardDir, forwardSignals, result.MarketStatus, 5, ds.TradingDates, result.PositionDecision)
	}
	return result, nil
}

func buildPortfolioBudget(cfg config.PortfolioConfig, summary *portfolio.Summary, memberships sector.MembershipStore, date string, decision signals.PositionDecision) signals.PortfolioBudget {
	budget := signals.PortfolioBudget{
		MaxTotalPct:       signals.DeployablePositionCap(decision, cfg.MaxTotalPositionPct),
		MaxSinglePct:      cfg.MaxSinglePositionPct,
		MaxSectorPct:      cfg.MaxSectorPositionPct,
		ExistingCodePct:   make(map[string]float64),
		ExistingSectorPct: make(map[string]float64),
	}
	if summary == nil || cfg.ReferenceEquity <= 0 {
		return budget
	}
	for _, holding := range summary.Holdings {
		if holding.MarketVal <= 0 {
			continue
		}
		pct := holding.MarketVal / cfg.ReferenceEquity * 100
		budget.ExistingTotalPct += pct
		budget.ExistingCodePct[holding.Code] += pct
		if membership, ok := memberships.PrimaryIndustry(holding.Code, date); ok {
			budget.ExistingSectorPct[membership.SectorName] += pct
		}
	}
	return budget
}

func usesFundamentals(strategiesList []strategy.Strategy) bool {
	for _, current := range strategiesList {
		if _, ok := current.(strategy.FundStoreUser); ok {
			return true
		}
	}
	return false
}

func fetchMarketRealtime(provider realtime.Provider, codeMap map[string][]data.DailyBar, window time.Duration) ([]realtime.Quote, realtime.FetchStats, error) {
	if provider == nil {
		provider = realtime.NewAutoProvider()
	}
	paced, ok := provider.(realtime.PacedProvider)
	if !ok {
		return nil, realtime.FetchStats{}, fmt.Errorf("实时行情提供方不支持全市场限速刷新")
	}
	if window <= 0 {
		window = time.Minute
	}
	return paced.FetchPaced(market.SortedQuoteCodes(codeMap), window)
}

func loadPortfolioSummary(path string, ledger *portfolio.Ledger, ds *dataset.Dataset) (*portfolio.Summary, error) {
	barsMap := ds.AllCodeMap
	if len(barsMap) == 0 {
		barsMap = ds.CodeMap
	}
	if ledger != nil {
		return portfolio.Analyze(ledger, barsMap, ds.StockNames), nil
	}
	ledger, err := portfolio.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("加载组合失败: %w", err)
	}
	return portfolio.Analyze(ledger, barsMap, ds.StockNames), nil
}

func realtimeTargets(signalsList []signals.SignalResult, watchlist []signals.SignalResult) []signals.SignalResult {
	targets := make([]signals.SignalResult, 0, len(signalsList)+len(watchlist))
	targets = append(targets, signalsList...)
	targets = append(targets, watchlist...)
	return targets
}

func analyzeNews(ctx context.Context, rawDir string, analyzer NewsAnalyzer) (*news.NewsSummary, error) {
	if analyzer != nil {
		return analyzer(ctx, rawDir, 8)
	}
	return news.Analyze(ctx, nil, rawDir, 8)
}

func shouldRecordCash(decision signals.PositionDecision) bool {
	return decision.Action == signals.PositionActionCash || decision.Action == signals.PositionActionWatch
}

func selectStrategies(requested, defaults []string) ([]strategy.Strategy, []string, error) {
	reg := strategy.DefaultRegistry()
	names := requested
	if len(names) == 0 {
		names = defaults
	}
	var selected []strategy.Strategy
	var selectedNames []string
	for _, name := range names {
		s, ok := reg.Get(name)
		if !ok {
			continue
		}
		selected = append(selected, s)
		selectedNames = append(selectedNames, name)
	}
	if len(selected) == 0 {
		return nil, nil, fmt.Errorf("没有可用的策略")
	}
	return selected, selectedNames, nil
}

func injectFundamentals(strategies []strategy.Strategy, store *data.FundamentalStore) {
	if store == nil {
		return
	}
	for _, s := range strategies {
		if u, ok := s.(strategy.FundStoreUser); ok {
			u.SetFundStore(store)
		}
	}
}

func filterStrategies(strategies []strategy.Strategy, skip map[string]bool) []strategy.Strategy {
	filtered := make([]strategy.Strategy, 0, len(strategies))
	for _, s := range strategies {
		if skip[s.Name()] {
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}

func strategyNamesFrom(strategies []strategy.Strategy) []string {
	names := make([]string, 0, len(strategies))
	for _, s := range strategies {
		names = append(names, s.Name())
	}
	return names
}

func fetchRealtimeQuotes(provider realtime.Provider, results []signals.SignalResult, portfolioSummary *portfolio.Summary) (map[string]realtime.Quote, error) {
	var codes []string
	for _, r := range results {
		codes = append(codes, r.Code)
	}
	if portfolioSummary != nil {
		for _, h := range portfolioSummary.Holdings {
			codes = append(codes, h.Code)
		}
	}
	codes = realtime.UniqueCodes(codes)
	if len(codes) == 0 {
		return nil, nil
	}
	if provider == nil {
		provider = realtime.NewAutoProvider()
	}
	quotes, err := provider.Fetch(codes)
	if err != nil {
		return nil, err
	}
	return realtime.MapByCode(quotes), nil
}
