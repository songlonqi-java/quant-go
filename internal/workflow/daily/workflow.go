// Package daily owns the end-of-day orchestration used by interactive
// applications. It calls the existing data and signal modules directly; it
// never shells out to the go-quant executable.
package daily

import (
	"context"
	"fmt"
	"time"

	"quant/internal/config"
	"quant/internal/data"
	"quant/internal/portfolio"
	"quant/internal/realtime"
	"quant/internal/workflow/sectorbuild"
	signalworkflow "quant/internal/workflow/signal"
)

const (
	StepSucceeded = "succeeded"
	StepSkipped   = "skipped"
	StepFailed    = "failed"
)

// Step is deliberately small so task runners can persist progress without
// knowing any implementation details of data fetching or signal generation.
type Step struct {
	Name       string    `json:"name"`
	State      string    `json:"state"`
	Detail     string    `json:"detail"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// Progress receives each completed step. It is optional and must return
// quickly; callers use it for durable task logs rather than business logic.
type Progress func(Step)

type Options struct {
	Config              *config.Config
	PortfolioPath       string
	PortfolioLedger     *portfolio.Ledger
	ForwardDir          string
	TopN                int
	WatchN              int
	RealtimeSource      string
	MarketRefreshWindow time.Duration
	Progress            Progress
}

// Result keeps the execution trace and the structured signal result. The
// presentation layer converts it into its own durable report format.
type Result struct {
	TargetDate string
	Steps      []Step
	Signal     *signalworkflow.Result
}

// Run performs the standard daily sequence: daily bars, price limits,
// moneyflow, indexes, local sector aggregation and signal generation. A
// missing same-day daily bar is a successful skipped step because Tushare
// publishes it after the close; the latest available bar is then analysed.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("缺少配置")
	}
	if opts.Config.Tushare.Token == "" || opts.Config.Tushare.Token == "YOUR_TUSHARE_TOKEN_HERE" {
		return nil, fmt.Errorf("请在配置文件或环境变量 QUANT_TUSHARE_TOKEN 中设置 Tushare Token")
	}
	if opts.TopN <= 0 {
		opts.TopN = 10
	}
	if opts.WatchN < 0 {
		opts.WatchN = 5
	}
	if opts.WatchN == 0 {
		opts.WatchN = 5
	}
	if opts.RealtimeSource == "" {
		opts.RealtimeSource = realtime.SourceAuto
	}
	if opts.MarketRefreshWindow <= 0 {
		opts.MarketRefreshWindow = time.Minute
	}
	if opts.PortfolioPath == "" {
		opts.PortfolioPath = "portfolio.yaml"
	}
	if opts.ForwardDir == "" {
		opts.ForwardDir = opts.Config.Data.RawDir + "/../forward_test"
	}

	now := time.Now()
	china, err := time.LoadLocation("Asia/Shanghai")
	if err == nil {
		now = now.In(china)
	}
	result := &Result{TargetDate: now.Format("20060102")}
	client := data.NewClient(opts.Config.Tushare.BaseURL, opts.Config.Tushare.Token, opts.Config.Tushare.RateLimitMs)
	if opts.Config.Tushare.DailyCallLimit > 0 {
		client.SetDailyLimit(opts.Config.Tushare.DailyCallLimit)
	}
	fetcher := data.NewFetcher(client, opts.Config.Data.RawDir, opts.Config.Fetch.StockPrefixes)

	if err := runDailyBarsStep(&result.Steps, opts.Progress, fetcher, ctx); err != nil {
		return result, fmt.Errorf("拉取日线: %w", err)
	}

	if err := runStep(&result.Steps, opts.Progress, "拉取涨跌停价", func() (string, error) {
		if err := fetcher.FetchStkLimitRange(ctx, result.TargetDate, result.TargetDate); err != nil {
			return "", err
		}
		return "涨跌停价格已刷新", nil
	}); err != nil {
		return result, fmt.Errorf("拉取涨跌停价: %w", err)
	}
	if err := runStep(&result.Steps, opts.Progress, "拉取资金流向", func() (string, error) {
		if err := fetcher.FetchMoneyflowRange(ctx, result.TargetDate, result.TargetDate); err != nil {
			return "", err
		}
		return "个股资金流向已刷新", nil
	}); err != nil {
		return result, fmt.Errorf("拉取资金流向: %w", err)
	}
	if err := runStep(&result.Steps, opts.Progress, "拉取指数", func() (string, error) {
		if err := fetcher.FetchIndexData(ctx, now.Year(), now.Year()); err != nil {
			return "", err
		}
		return "上证、深证、创业板指数已刷新", nil
	}); err != nil {
		return result, fmt.Errorf("拉取指数: %w", err)
	}
	if err := runStep(&result.Steps, opts.Progress, "构建板块快照", func() (string, error) {
		built, err := sectorbuild.BuildLatest(opts.Config.Data.RawDir)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s 已聚合 %d 个板块", built.TradeDate, built.Rows), nil
	}); err != nil {
		return result, fmt.Errorf("构建板块快照: %w", err)
	}

	var provider realtime.Provider
	inTradingHours := realtime.IsAShareTradingHours(now)
	if inTradingHours {
		provider, err = realtime.NewProvider(opts.RealtimeSource)
		if err != nil {
			return result, err
		}
	}
	if err := runStep(&result.Steps, opts.Progress, "生成交易信号", func() (string, error) {
		signalResult, err := signalworkflow.Run(ctx, signalworkflow.Options{
			Config:              opts.Config,
			TopN:                opts.TopN,
			WatchN:              opts.WatchN,
			Realtime:            inTradingHours,
			MarketRealtime:      inTradingHours,
			MarketRefreshWindow: opts.MarketRefreshWindow,
			PortfolioPath:       opts.PortfolioPath,
			PortfolioLedger:     opts.PortfolioLedger,
			ForwardDir:          opts.ForwardDir,
			RealtimeProvider:    provider,
		})
		if err != nil {
			return "", err
		}
		result.Signal = signalResult
		return fmt.Sprintf("分析 %s，正式信号 %d 条、观察机会 %d 条", signalResult.Dataset.LatestDate, len(signalResult.Signals), len(signalResult.Watchlist)), nil
	}); err != nil {
		return result, fmt.Errorf("生成交易信号: %w", err)
	}

	return result, nil
}

func runDailyBarsStep(steps *[]Step, progress Progress, fetcher *data.Fetcher, ctx context.Context) error {
	step := Step{Name: "拉取日线", StartedAt: time.Now().UTC()}
	bars, err := fetcher.FetchToday(ctx, false)
	if err != nil {
		step.State = StepFailed
		step.Detail = err.Error()
	} else if len(bars) == 0 {
		step.State = StepSkipped
		step.Detail = "当日日线尚未发布，使用本地最近交易日"
	} else {
		step.State = StepSucceeded
		step.Detail = fmt.Sprintf("已更新 %d 条日线", len(bars))
	}
	step.FinishedAt = time.Now().UTC()
	*steps = append(*steps, step)
	if progress != nil {
		progress(step)
	}
	return err
}

func runStep(steps *[]Step, progress Progress, name string, run func() (string, error)) error {
	step := Step{Name: name, StartedAt: time.Now().UTC()}
	detail, err := run()
	step.Detail = detail
	if err != nil {
		step.State = StepFailed
		step.Detail = err.Error()
		step.FinishedAt = time.Now().UTC()
		*steps = append(*steps, step)
		if progress != nil {
			progress(step)
		}
		return err
	}
	step.State = StepSucceeded
	step.FinishedAt = time.Now().UTC()
	*steps = append(*steps, step)
	if progress != nil {
		progress(step)
	}
	return nil
}
