package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"quant/internal/ai"
	"quant/internal/config"
	"quant/internal/dataset"
	"quant/internal/realtime"
	"quant/internal/signal"
	"quant/internal/strategy"
	"quant/internal/validation"
	workflowdaily "quant/internal/workflow/daily"
	signalworkflow "quant/internal/workflow/signal"

	"github.com/spf13/cobra"
)

// dailyCmd is the one-command end-of-day flow: fetch data, rebuild the sector
// snapshot, generate trading signals, refresh the validation evidence and
// print a compact recommendation summary with an optional AI brief.
func dailyCmd() *cobra.Command {
	var (
		topN            int
		watchN          int
		withAI          bool
		rebuildEvidence bool
		realtimeSource  string
	)
	topN = 3
	watchN = 3
	rebuildEvidence = true

	cmd := &cobra.Command{
		Use:   "daily",
		Short: "一键日终：拉数据、生成信号、重建验证证据并输出精简推荐",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			ctx := context.Background()

			fmt.Println(">>> 日终任务：拉取数据 → 板块快照 → 交易信号")
			result, err := workflowdaily.Run(ctx, workflowdaily.Options{
				Config:         cfg,
				PortfolioPath:  "portfolio.yaml",
				TopN:           topN,
				WatchN:         watchN,
				RealtimeSource: realtimeSource,
				Progress: func(step workflowdaily.Step) {
					fmt.Printf("  [%s] %s: %s\n", stepStateLabel(step.State), step.Name, step.Detail)
				},
			})
			if err != nil {
				return err
			}
			if result.Signal == nil {
				return fmt.Errorf("信号生成为空")
			}

			if rebuildEvidence && cfg.Validation.Enabled {
				fmt.Println(">>> 重建历史验证证据（保持正式推荐资格有效）...")
				if err := buildEvidenceFile(ctx, cfg); err != nil {
					fmt.Printf("  [警告] 证据重建失败，正式推荐将被禁用: %v\n", err)
				}
			}

			printDailySummary(result.Signal, rebuildEvidence && cfg.Validation.Enabled)
			if withAI {
				printAIBrief(ctx, cfg, result.Signal)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&topN, "top", "n", 3, "买入推荐数量")
	cmd.Flags().IntVar(&watchN, "watch", 3, "观察机会数量")
	cmd.Flags().BoolVar(&withAI, "ai", false, "生成 AI 点评（需 config.yaml 启用 ai 并设置 QUANT_AI_API_KEY）")
	cmd.Flags().BoolVar(&rebuildEvidence, "validate", true, "信号生成后自动重建历史验证证据")
	cmd.Flags().StringVar(&realtimeSource, "realtime-source", realtime.SourceAuto, "实时行情来源: auto、eastmoney、sina")
	return cmd
}

func stepStateLabel(state string) string {
	switch state {
	case workflowdaily.StepSucceeded:
		return "OK"
	case workflowdaily.StepSkipped:
		return "跳过"
	case workflowdaily.StepFailed:
		return "失败"
	}
	return state
}

// buildEvidenceFile replays the full recommendation chain and writes the
// out-of-sample evidence used to qualify formal buys.
func buildEvidenceFile(ctx context.Context, cfg *config.Config) error {
	liquidityPolicy := cfg.Liquidity.Policy()
	if err := liquidityPolicy.Validate(); err != nil {
		return err
	}
	names := strategy.DailyStrategyNames(cfg.Signal.DefaultStrategies)
	strategies, err := registeredStrategies(names)
	if err != nil {
		return err
	}
	loadFundamentals := liquidityPolicy.Enabled && liquidityPolicy.MinTurnoverRatePct > 0
	for _, current := range strategies {
		if _, ok := current.(strategy.FundStoreUser); ok {
			loadFundamentals = true
			break
		}
	}
	ds, err := dataset.Load(dataset.LoadOptions{RawDir: cfg.Data.RawDir, LoadFundamentals: loadFundamentals, SkipMoneyflows: true})
	if err != nil {
		return err
	}
	if q := ds.CheckPriceQualityAll(); !q.HasCompleteRawPrices() {
		return fmt.Errorf("%s；历史验证需要真实成交价", q.Summary())
	}
	store, err := validation.Build(validation.BuildOptions{
		CodeMap:         ds.CodeMap,
		StockNames:      ds.StockNames,
		StockInfos:      ds.StockInfos,
		Strategies:      strategies,
		Fundamentals:    ds.Fundamentals,
		Moneyflows:      ds.Moneyflows,
		Commission:      cfg.Backtest.Commission,
		Slippage:        cfg.Backtest.Slippage,
		Liquidity:       liquidityPolicy,
		ReferenceEquity: cfg.Portfolio.Normalized(cfg.Backtest.InitialCapital).ReferenceEquity,
		Workers:         32,
	})
	if err != nil {
		return err
	}
	path := validation.DefaultPath(cfg.Data.RawDir, cfg.Validation.Path)
	if err := store.Save(path); err != nil {
		return err
	}
	fmt.Printf("  证据已重建: %s ~ %s，样本外 %d 折，独立可成交样本 %d\n",
		store.StartDate, store.EndDate, len(store.Folds), store.FeasibleTrades)
	return nil
}

// printDailySummary prints the trimmed end-of-day view: market, position
// policy, buy recommendations, watchlist, sell signals, holdings and news.
// evidenceRebuilt reports whether the evidence was refreshed after signal
// generation; when true the pre-rebuild validation error is stale and hidden.
func printDailySummary(sr *signalworkflow.Result, evidenceRebuilt bool) {
	fmt.Printf("\n========== 日终摘要 %s ==========\n", sr.Dataset.LatestDate)
	if ms := sr.MarketStatus; ms != nil {
		fmt.Printf("市场: 指数%.2f (%+.2f%%) %s | 宽度%.0f%% | 涨%d/跌%d 涨停%d/跌停%d | 成交%.0f亿 | 情绪%s\n",
			ms.IndexClose, ms.IndexChg, ms.MATrend, ms.Breadth,
			ms.RisingCount, ms.FallingCount, ms.LimitUpCount, ms.LimitDownCount,
			ms.TurnoverAmount/100000, ms.Sentiment)
	}
	fmt.Printf("策略: %s\n", strings.Join(sr.StrategyNames, ", "))
	fmt.Printf("仓位策略: %s | 候选%d/合格%d | %s\n",
		sr.PositionDecision.Action, sr.PositionDecision.CandidateBuys,
		sr.PositionDecision.QualifiedBuys, sr.PositionDecision.Advice)

	var buys, sells []signal.SignalResult
	for _, r := range sr.Signals {
		if r.BuyCount > 0 {
			buys = append(buys, r)
		} else {
			sells = append(sells, r)
		}
	}
	if len(buys) == 0 {
		fmt.Println("买入推荐: 无")
	} else {
		fmt.Printf("买入推荐 (%d):\n", len(buys))
		for i, r := range buys {
			fmt.Printf("  %d. [%s] %s %s 收盘%.2f 置信%.0f 仓位%.0f%% | %s\n",
				i+1, strategy.HorizonLabel(r.Horizon), r.Code, r.Name, r.Close,
				r.Confidence, r.PositionPct, strings.Join(r.RiskLabels, ","))
		}
	}
	if len(sells) > 0 {
		fmt.Printf("卖出/回避信号: %d 条 | 示例: %s\n", len(sells), strings.Join(firstCodes(sells, 5), ", "))
	}
	if len(sr.Watchlist) > 0 {
		fmt.Printf("观察机会 (%d):\n", len(sr.Watchlist))
		for i, r := range sr.Watchlist {
			if i >= 3 {
				break
			}
			fmt.Printf("  %d. [%s] %s %s 收盘%.2f 置信%.0f | %s\n",
				i+1, strategy.HorizonLabel(r.Horizon), r.Code, r.Name, r.Close,
				r.Confidence, strings.Join(r.RiskLabels, ","))
		}
	}
	if p := sr.PortfolioSummary; p != nil {
		if len(p.Holdings) == 0 {
			line := "持仓: 空仓"
			if total := p.WinCount + p.LossCount; total > 0 {
				line += fmt.Sprintf("（历史已平仓 %d 笔, 胜率 %.0f%%, 累计 %+.0f）", total, p.WinRate, p.TotalRealized)
			}
			fmt.Println(line)
		} else {
			fmt.Printf("持仓 (%d 只):\n", len(p.Holdings))
			for _, h := range p.Holdings {
				fmt.Printf("  %s %s %.0f股 成本%.2f 现价%.2f %+.2f%%\n",
					h.Code, h.Name, h.Shares, h.Cost, h.LastPrice, h.PnLPct)
			}
		}
	}
	if n := sr.NewsSummary; n != nil {
		var topics []string
		for _, t := range n.RecentHotTopics {
			if len(topics) >= 5 {
				break
			}
			topics = append(topics, t.Keyword)
		}
		var stocks []string
		for _, s := range n.HotStocks {
			if len(stocks) >= 3 {
				break
			}
			stocks = append(stocks, s.Name)
		}
		fmt.Printf("新闻热点: %s | 关注: %s\n", strings.Join(topics, "/"), strings.Join(stocks, ", "))
	}
	if sr.ValidationErr != nil && !evidenceRebuilt {
		fmt.Printf("验证状态: %v\n", sr.ValidationErr)
	}
}

func firstCodes(results []signal.SignalResult, n int) []string {
	codes := make([]string, 0, n)
	for i, r := range results {
		if i >= n {
			break
		}
		codes = append(codes, r.Code)
	}
	return codes
}

// printAIBrief sends the structured daily facts to the configured model and
// prints a short recommendation brief. AI failures never fail the task.
func printAIBrief(ctx context.Context, cfg *config.Config, sr *signalworkflow.Result) {
	if !cfg.AI.Enabled {
		fmt.Println("\n>>> AI 点评跳过：config.yaml 中 ai.enabled 未启用")
		return
	}
	client, err := ai.New(ai.Config{
		BaseURL: cfg.AI.BaseURL,
		APIKey:  cfg.AI.APIKey,
		Model:   cfg.AI.Model,
		Timeout: time.Duration(cfg.AI.TimeoutSec) * time.Second,
	})
	if err != nil {
		fmt.Printf("\n>>> AI 点评失败: %v\n", err)
		return
	}
	system := "你是A股日终分析助手。根据量化系统输出的事实给出简短建议。必须区分\"系统结论\"和\"你的推断\"；系统没有数据时明确说\"数据不足\"。不要虚构价格、涨跌幅或新闻。回答不超过20行，按小节输出：今日判断 / 买入建议 / 观察机会 / 持仓操作 / 风险提示。"
	comp, err := client.Complete(ctx, system, buildAIBriefPrompt(sr))
	if err != nil {
		fmt.Printf("\n>>> AI 点评失败: %v\n", err)
		return
	}
	fmt.Printf("\n========== AI 点评 (%s) ==========\n%s\n", client.Model(), strings.TrimSpace(comp.Content))
}

func buildAIBriefPrompt(sr *signalworkflow.Result) string {
	type brief struct {
		Date        string   `json:"日期"`
		IndexClose  float64  `json:"指数收盘"`
		IndexChgPct float64  `json:"指数涨跌幅pct"`
		MATrend     string   `json:"均线趋势"`
		BreadthPct  float64  `json:"市场宽度pct"`
		UpDown      string   `json:"涨跌家数"`
		LimitUp     int      `json:"涨停"`
		LimitDown   int      `json:"跌停"`
		TurnoverYi  float64  `json:"成交额亿"`
		Sentiment   string   `json:"情绪"`
		Position    string   `json:"仓位策略"`
		PositionWhy string   `json:"仓位策略原因"`
		Buys        []string `json:"买入推荐"`
		Watchlist   []string `json:"观察机会"`
		SellCount   int      `json:"卖出回避信号数"`
		SellCodes   []string `json:"卖出回避示例"`
		Holdings    []string `json:"持仓"`
		NewsTopics  string   `json:"新闻热点"`
		NewsStocks  string   `json:"新闻关注个股"`
	}
	b := brief{Date: sr.Dataset.LatestDate, Position: string(sr.PositionDecision.Action), PositionWhy: sr.PositionDecision.Advice}
	if ms := sr.MarketStatus; ms != nil {
		b.IndexClose = ms.IndexClose
		b.IndexChgPct = ms.IndexChg
		b.MATrend = ms.MATrend
		b.BreadthPct = ms.Breadth * 100
		b.UpDown = fmt.Sprintf("%d/%d", ms.RisingCount, ms.FallingCount)
		b.LimitUp = ms.LimitUpCount
		b.LimitDown = ms.LimitDownCount
		b.TurnoverYi = ms.TurnoverAmount / 1e8
		b.Sentiment = ms.Sentiment
	}
	for _, r := range sr.Signals {
		if r.BuyCount == 0 {
			b.SellCount++
			if len(b.SellCodes) < 5 {
				b.SellCodes = append(b.SellCodes, r.Code)
			}
			continue
		}
		b.Buys = append(b.Buys, fmt.Sprintf("%s %s [%s] 收盘%.2f 置信%.0f 仓位%.0f%% 风险:%s",
			r.Code, r.Name, strategy.HorizonLabel(r.Horizon), r.Close, r.Confidence, r.PositionPct, strings.Join(r.RiskLabels, ",")))
	}
	for _, r := range sr.Watchlist {
		if len(b.Watchlist) >= 3 {
			break
		}
		b.Watchlist = append(b.Watchlist, fmt.Sprintf("%s %s [%s] 收盘%.2f 置信%.0f 风险:%s",
			r.Code, r.Name, strategy.HorizonLabel(r.Horizon), r.Close, r.Confidence, strings.Join(r.RiskLabels, ",")))
	}
	if p := sr.PortfolioSummary; p != nil {
		for _, h := range p.Holdings {
			b.Holdings = append(b.Holdings, fmt.Sprintf("%s %s %.0f股 成本%.2f 现价%.2f 盈亏%+.2f%%",
				h.Code, h.Name, h.Shares, h.Cost, h.LastPrice, h.PnLPct))
		}
	}
	if n := sr.NewsSummary; n != nil {
		var topics []string
		for _, t := range n.RecentHotTopics {
			if len(topics) >= 5 {
				break
			}
			topics = append(topics, t.Keyword)
		}
		var stocks []string
		for _, s := range n.HotStocks {
			if len(stocks) >= 3 {
				break
			}
			stocks = append(stocks, s.Name)
		}
		b.NewsTopics = strings.Join(topics, "/")
		b.NewsStocks = strings.Join(stocks, ", ")
	}
	payload, _ := json.MarshalIndent(b, "", "  ")
	return "以下是今日量化系统输出（全部为系统数据，请基于它给出简短建议）：\n" + string(payload)
}
