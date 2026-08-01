package backtest

import (
	"fmt"
	"sort"

	"quant/internal/data"
	"quant/internal/market"
)

// PortfolioEnvironment owns the immutable, strategy-independent inputs used
// by a portfolio replay. It can be shared by baseline and variant runs while
// their strategies and account state remain fully isolated.
type PortfolioEnvironment struct {
	codeMap  map[string][]data.DailyBar
	indexes  map[string]map[string]int
	dates    []string
	codes    []string
	statuses map[string]*market.MarketStatus
}

// PreparePortfolioEnvironment copies and sorts the source once, then builds
// date indexes and no-look-ahead market states once. Callers must treat the
// returned environment as immutable and may safely reuse it for sequential or
// concurrent portfolio runs with independent strategy instances.
func PreparePortfolioEnvironment(source map[string][]data.DailyBar) (*PortfolioEnvironment, error) {
	if len(source) == 0 {
		return nil, fmt.Errorf("组合回测缺少行情数据")
	}
	codeMap, indexes, dates := preparePortfolioData(source)
	codes := make([]string, 0, len(codeMap))
	for code := range codeMap {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return &PortfolioEnvironment{
		codeMap: codeMap, indexes: indexes, dates: dates, codes: codes,
		statuses: market.BuildHistoricalStatus(codeMap),
	}, nil
}

func (e *PortfolioEnvironment) valid() bool {
	return e != nil && e.codeMap != nil && e.indexes != nil && e.dates != nil && e.codes != nil && e.statuses != nil
}
