// Package sectorbuild constructs the persisted sector snapshot used by both
// the command line and higher-level task workflows.
package sectorbuild

import (
	"fmt"

	"quant/internal/data"
	"quant/internal/sector"
)

// Result describes one persisted sector snapshot.
type Result struct {
	TradeDate string
	Rows      int
	Report    *sector.Report
}

// BuildLatest derives a sector snapshot from the newest local daily bar. It
// performs no network calls, which lets a daily task safely use the data it
// has just refreshed.
func BuildLatest(rawDir string) (*Result, error) {
	bars, err := data.ReadParquetDir(rawDir + "/daily")
	if err != nil {
		return nil, fmt.Errorf("加载日线数据失败: %w", err)
	}
	latest := ""
	for _, bar := range bars {
		if bar.TradeDate > latest {
			latest = bar.TradeDate
		}
	}
	if latest == "" {
		return nil, fmt.Errorf("没有可构建板块的交易日期")
	}
	return BuildDate(rawDir, latest)
}

// BuildDate aggregates one local trading date into sector_daily. Valuation
// fields are included only because the slow value workflow consumes the same
// snapshot; normal daily signal generation does not read those fields.
func BuildDate(rawDir, tradeDate string) (*Result, error) {
	return BuildDates(rawDir, []string{tradeDate})
}

// BuildDates aggregates local trading dates into sector_daily in one pass.
// The returned report is limited to the newest date, matching the report a
// caller needs for a current market analysis.
func BuildDates(rawDir string, tradeDates []string) (*Result, error) {
	if len(tradeDates) == 0 {
		return nil, fmt.Errorf("缺少待构建的交易日期")
	}
	latest := ""
	for _, tradeDate := range tradeDates {
		if len(tradeDate) != 8 {
			return nil, fmt.Errorf("交易日期应为 YYYYMMDD")
		}
		if tradeDate > latest {
			latest = tradeDate
		}
	}
	bars, err := data.ReadParquetDir(rawDir + "/daily")
	if err != nil {
		return nil, fmt.Errorf("加载日线数据失败: %w", err)
	}

	fetcher := data.NewFetcher(nil, rawDir, nil)
	if limits, err := fetcher.LoadStkLimitStore(); err == nil {
		bars = data.ApplyStkLimits(bars, limits)
	}
	memberships, err := sector.LoadIndustryMemberships(rawDir)
	if err != nil {
		return nil, fmt.Errorf("加载行业归属失败: %w", err)
	}
	moneyflows, _ := fetcher.LoadMoneyflowStore()
	fundamentals, _ := fetcher.LoadDailyBasicStore()
	rows := sector.Analyze(data.GroupByCode(bars), memberships, moneyflows, sector.AnalyzeOptions{
		Dates:        tradeDates,
		Fundamentals: fundamentals,
	})
	if len(rows) == 0 {
		return nil, fmt.Errorf("没有生成板块数据")
	}
	if err := sector.WriteSectorDaily(rawDir, rows); err != nil {
		return nil, fmt.Errorf("写入板块数据失败: %w", err)
	}
	return &Result{
		TradeDate: latest,
		Rows:      len(rows),
		Report:    sector.NewReport(rowsForDate(rows, latest)),
	}, nil
}

func rowsForDate(rows []data.SectorDaily, date string) []data.SectorDaily {
	filtered := make([]data.SectorDaily, 0, len(rows))
	for _, row := range rows {
		if row.TradeDate == date {
			filtered = append(filtered, row)
		}
	}
	return filtered
}
