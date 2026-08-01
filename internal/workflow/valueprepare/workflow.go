// Package valueprepare refreshes the slow-changing inputs used by value tasks.
package valueprepare

import (
	"context"
	"fmt"
	"path/filepath"

	"quant/internal/config"
	"quant/internal/data"
	"quant/internal/value"
	"quant/internal/workflow/sectorbuild"
)

type Result struct {
	Readiness *value.Readiness `json:"readiness"`
}

func Run(ctx context.Context, cfg *config.Config, progress func(string)) (*Result, error) {
	if cfg == nil || cfg.Tushare.Token == "" {
		return nil, fmt.Errorf("未配置 Tushare token，无法刷新慢频数据")
	}
	bars, err := data.ReadParquetDir(filepath.Join(cfg.Data.RawDir, "daily"))
	if err != nil {
		return nil, fmt.Errorf("读取日线: %w", err)
	}
	latest := ""
	for _, bar := range bars {
		if bar.TradeDate > latest {
			latest = bar.TradeDate
		}
	}
	if latest == "" {
		return nil, fmt.Errorf("没有可用于准备慢频数据的交易日")
	}
	if progress != nil {
		progress("刷新 " + latest + " 的 daily_basic 估值快照")
	}
	client := data.NewClient(cfg.Tushare.BaseURL, cfg.Tushare.Token, cfg.Tushare.RateLimitMs)
	if _, err := data.NewFetcher(client, cfg.Data.RawDir, cfg.Fetch.StockPrefixes).FetchDailyBasicForDate(ctx, latest); err != nil {
		return nil, fmt.Errorf("刷新 daily_basic: %w", err)
	}
	if progress != nil {
		progress("重建最新行业估值快照")
	}
	if _, err := sectorbuild.BuildLatest(cfg.Data.RawDir); err != nil {
		return nil, fmt.Errorf("构建行业快照: %w", err)
	}
	readiness, err := value.CheckReadiness(cfg.Data.RawDir)
	if err != nil {
		return nil, err
	}
	if !readiness.Ready {
		return &Result{Readiness: readiness}, fmt.Errorf("慢频数据仍未就绪: %v；财务指标请先使用 fetch-financials 更新", readiness.Issues)
	}
	return &Result{Readiness: readiness}, nil
}
