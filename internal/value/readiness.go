package value

import (
	"fmt"
	"path/filepath"
	"sort"

	"quant/internal/data"
	"quant/internal/sector"
)

type Readiness struct {
	TradeDate       string
	Stocks          int
	DailyBasicCount int
	FinancialCount  int
	SectorReady     bool
	Ready           bool
	Issues          []string
}

// CheckReadiness verifies the slow-data inputs without running the screen or
// weakening its policy. It is intentionally independent from daily signals.
func CheckReadiness(rawDir string) (*Readiness, error) {
	bars, err := data.ReadParquetDir(filepath.Join(rawDir, "daily"))
	if err != nil {
		return nil, fmt.Errorf("读取日线: %w", err)
	}
	result := &Readiness{}
	for _, bar := range bars {
		if bar.TradeDate > result.TradeDate {
			result.TradeDate = bar.TradeDate
		}
	}
	if result.TradeDate == "" {
		return nil, fmt.Errorf("没有最新交易日")
	}
	codes := make(map[string]bool)
	for _, bar := range bars {
		if bar.TradeDate == result.TradeDate {
			codes[bar.TsCode] = true
		}
	}
	result.Stocks = len(codes)
	fetcher := data.NewFetcher(nil, rawDir, nil)
	basic, _ := fetcher.LoadDailyBasicStore()
	fina, _ := fetcher.LoadFinaStore()
	ordered := make([]string, 0, len(codes))
	for code := range codes {
		ordered = append(ordered, code)
	}
	sort.Strings(ordered)
	for _, code := range ordered {
		if basic != nil && basic.GetDailyBasic(code, result.TradeDate) != nil {
			result.DailyBasicCount++
		}
		if fina != nil {
			if _, ok := fina.GetFinaIndicatorAsOf(code, result.TradeDate); ok {
				result.FinancialCount++
			}
		}
	}
	report, _ := sector.LoadReport(rawDir, result.TradeDate)
	result.SectorReady = report != nil && len(report.Sectors) > 0
	if result.DailyBasicCount == 0 {
		result.Issues = append(result.Issues, "缺少最新交易日 daily_basic 估值快照")
	}
	if result.FinancialCount == 0 {
		result.Issues = append(result.Issues, "缺少可用于最新交易日的财务指标")
	}
	if !result.SectorReady {
		result.Issues = append(result.Issues, "缺少最新交易日行业估值快照")
	}
	result.Ready = len(result.Issues) == 0
	return result, nil
}
