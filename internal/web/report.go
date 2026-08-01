package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"quant/internal/data"
	"quant/internal/market"
	"quant/internal/news"
	"quant/internal/portfolio"
	"quant/internal/signal"
	"quant/internal/value"
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
	CodeVersion     string                     `json:"code_version,omitempty"`
	StrategyVersion string                     `json:"strategy_version,omitempty"`
	DataVersion     string                     `json:"data_version,omitempty"`
	SnapshotLedger  []portfolio.Transaction    `json:"-"`
	ValueMonthly    *value.MonthlyReport       `json:"value_monthly,omitempty"`
	ValueQuarterly  *value.QuarterlyReport     `json:"value_quarterly,omitempty"`
}

func reportFromValueMonthly(result *value.MonthlyReport) *DailyReport {
	report := &DailyReport{Version: "value-monthly-report-v1", GeneratedAt: time.Now().UTC(), CodeVersion: currentCodeVersion()}
	if result == nil {
		report.Warnings = append(report.Warnings, "月度价值工作流没有返回结果")
		return report
	}
	report.TargetDate = result.ScreenDate
	report.TradeDate = result.ScreenDate
	report.DataVersion = result.ScreenDate
	report.StrategyVersion = result.Policy.Version
	report.ValueMonthly = result
	return report
}

func reportFromValueQuarterly(result *value.QuarterlyReport) *DailyReport {
	report := &DailyReport{Version: "value-quarterly-report-v1", GeneratedAt: time.Now().UTC(), CodeVersion: currentCodeVersion()}
	if result == nil {
		report.Warnings = append(report.Warnings, "季度价值工作流没有返回结果")
		return report
	}
	report.TargetDate = result.ReviewDate
	report.TradeDate = result.ReviewDate
	report.DataVersion = result.ReviewDate
	report.StrategyVersion = result.Policy.Version
	report.ValueQuarterly = result
	return report
}

func reportFromDaily(result *daily.Result) *DailyReport {
	report := &DailyReport{
		Version:     "daily-report-v1",
		GeneratedAt: time.Now().UTC(),
		CodeVersion: currentCodeVersion(),
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
	report.StrategyVersion = strategyVersion(report.StrategyNames)
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
		report.DataVersion = signalResult.Dataset.LatestDate
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

func currentCodeVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "development"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			version := setting.Value
			if len(version) > 12 {
				version = version[:12]
			}
			return version
		}
	}
	if version := strings.TrimSpace(info.Main.Version); version != "" && version != "(devel)" {
		return version
	}
	return "development"
}

func strategyVersion(names []string) string {
	if len(names) == 0 {
		return ""
	}
	ordered := append([]string(nil), names...)
	sort.Strings(ordered)
	digest := sha256.Sum256([]byte(strings.Join(ordered, "\n")))
	return hex.EncodeToString(digest[:6])
}
