package signalworkflow

import (
	"context"
	"fmt"

	"quant/internal/config"
	"quant/internal/data"
	"quant/internal/dataset"
	"quant/internal/forward"
	"quant/internal/market"
	"quant/internal/news"
	"quant/internal/portfolio"
	"quant/internal/realtime"
	signals "quant/internal/signal"
	"quant/internal/strategy"
)

type Options struct {
	Config        *config.Config
	StrategyNames []string
	TopN          int
	Realtime      bool
	PortfolioPath string
	ForwardDir    string
}

type Result struct {
	StrategyNames    []string
	Dataset          *dataset.Dataset
	MarketStatus     *market.MarketStatus
	NewsSummary      *news.NewsSummary
	PortfolioSummary *portfolio.Summary
	PositionDecision signals.PositionDecision
	Signals          []signals.SignalResult
	RealtimeLoaded   int
	NewsErr          error
	RealtimeErr      error
	ForwardErr       error
	PriceQuality     data.PriceDataQuality
}

func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("缺少配置")
	}
	cfg := opts.Config
	if opts.PortfolioPath == "" {
		opts.PortfolioPath = "portfolio.yaml"
	}
	if opts.ForwardDir == "" {
		opts.ForwardDir = cfg.Data.RawDir + "/../forward_test"
	}

	selectedStrategies, strategyNames, err := selectStrategies(opts.StrategyNames, cfg.Signal.DefaultStrategies)
	if err != nil {
		return nil, err
	}
	ds, err := dataset.Load(dataset.LoadOptions{
		RawDir:       cfg.Data.RawDir,
		LatestOnly:   true,
		FilterST:     true,
		MinMarketCap: cfg.Fetch.MinMarketCap,
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
	result.MarketStatus = market.Analyze(ds.Bars)
	result.NewsSummary, result.NewsErr = news.Analyze(ctx, nil, cfg.Data.RawDir, 8)

	ledger, _ := portfolio.Load(opts.PortfolioPath)
	if ledger != nil {
		result.PortfolioSummary = portfolio.Analyze(ledger, ds.CodeMap, ds.StockNames)
	}

	result.Signals = signals.GenerateWithContextAndMoneyflow(ds.CodeMap, selectedStrategies, topN, ds.StockNames, result.MarketStatus, ds.Moneyflows)

	if opts.Realtime {
		quoteMap, err := fetchRealtimeQuotes(result.Signals, result.PortfolioSummary)
		if err != nil {
			result.RealtimeErr = err
		} else if len(quoteMap) > 0 {
			signals.ApplyRealtimeQuotes(result.Signals, quoteMap, ds.StkLimits)
			portfolio.ApplyRealtimeQuotes(result.PortfolioSummary, quoteMap)
			result.RealtimeLoaded = len(quoteMap)
		}
	}

	result.PositionDecision = signals.ApplyPositionPolicy(result.Signals, result.MarketStatus)
	if len(result.Signals) > 0 {
		result.ForwardErr = forward.RecordWithDecision(opts.ForwardDir, result.Signals, result.MarketStatus, 5, ds.TradingDates, result.PositionDecision)
	}
	return result, nil
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

func fetchRealtimeQuotes(results []signals.SignalResult, portfolioSummary *portfolio.Summary) (map[string]realtime.Quote, error) {
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
	provider := realtime.NewSinaProvider()
	quotes, err := provider.Fetch(codes)
	if err != nil {
		return nil, err
	}
	return realtime.MapByCode(quotes), nil
}
