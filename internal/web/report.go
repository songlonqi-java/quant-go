package web

import (
	"fmt"
	"time"

	"quant/internal/data"
	"quant/internal/market"
	"quant/internal/news"
	"quant/internal/portfolio"
	"quant/internal/signal"
	"quant/internal/workflow/daily"
)

// DailyReport is the stable, compact task output stored by the Web module.
// It intentionally excludes the loaded Dataset and other large working data
// from signalworkflow.Result, while preserving enough evidence for a person
// (and a future AI analysis module) to inspect a completed run.
type DailyReport struct {
	Version         string                     `json:"version"`
	GeneratedAt     time.Time                  `json:"generated_at"`
	TargetDate      string                     `json:"target_date"`
	TradeDate       string                     `json:"trade_date"`
	Steps           []daily.Step               `json:"steps"`
	StrategyNames   []string                   `json:"strategy_names"`
	Market          *market.MarketStatus       `json:"market,omitempty"`
	Intraday        *market.IntradayStatus     `json:"intraday,omitempty"`
	News            *news.NewsSummary          `json:"news,omitempty"`
	Position        signal.PositionDecision    `json:"position"`
	Signals         []signal.SignalResult      `json:"signals"`
	Recommendations []signal.SignalResult      `json:"recommendations"`
	Watchlist       []signal.SignalResult      `json:"watchlist"`
	Holdings        []portfolio.PositionStatus `json:"holdings"`
	Sectors         []data.SectorDaily         `json:"sectors"`
	Warnings        []string                   `json:"warnings,omitempty"`
}

func reportFromDaily(result *daily.Result) *DailyReport {
	report := &DailyReport{
		Version:     "daily-report-v1",
		GeneratedAt: time.Now().UTC(),
	}
	if result == nil {
		report.Warnings = append(report.Warnings, "日终工作流没有返回结果")
		return report
	}
	report.TargetDate = result.TargetDate
	report.Steps = append(report.Steps, result.Steps...)
	if result.Signal == nil {
		report.Warnings = append(report.Warnings, "交易信号尚未生成")
		return report
	}

	signalResult := result.Signal
	report.StrategyNames = append(report.StrategyNames, signalResult.StrategyNames...)
	report.Market = signalResult.MarketStatus
	report.Intraday = signalResult.IntradayMarket
	report.News = signalResult.NewsSummary
	report.Position = signalResult.PositionDecision
	report.Signals = append(report.Signals, signalResult.Signals...)
	if report.Position.Action != signal.PositionActionCash && report.Position.Action != signal.PositionActionWatch {
		for _, candidate := range signalResult.Signals {
			if candidate.Recommendation() == "买入" {
				report.Recommendations = append(report.Recommendations, candidate)
			}
		}
	}
	report.Watchlist = append(report.Watchlist, signalResult.Watchlist...)
	if signalResult.Dataset != nil {
		report.TradeDate = signalResult.Dataset.LatestDate
	}
	if signalResult.PortfolioSummary != nil {
		report.Holdings = append(report.Holdings, signalResult.PortfolioSummary.Holdings...)
	}
	if signalResult.SectorReport != nil {
		report.Sectors = append(report.Sectors, signalResult.SectorReport.Sectors...)
	}
	appendWarning(&report.Warnings, "新闻", signalResult.NewsErr)
	appendWarning(&report.Warnings, "板块", signalResult.SectorErr)
	appendWarning(&report.Warnings, "实时行情", signalResult.RealtimeErr)
	appendWarning(&report.Warnings, "全市场盘中行情", signalResult.MarketRealtimeErr)
	appendWarning(&report.Warnings, "历史验证", signalResult.ValidationErr)
	appendWarning(&report.Warnings, "前向测试记录", signalResult.ForwardErr)
	return report
}

func appendWarning(warnings *[]string, label string, err error) {
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("%s：%v", label, err))
	}
}
