package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"quant/internal/analyze"
	"quant/internal/backtest"
	"quant/internal/config"
	"quant/internal/data"
	"quant/internal/forward"
	"quant/internal/market"
	"quant/internal/news"
	"quant/internal/portfolio"
	"quant/internal/realtime"
	"quant/internal/signal"
	"quant/internal/strategy"

	"github.com/spf13/cobra"
)

var (
	cfgPath string
	cfg     *config.Config
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "go-quant",
		Short: "Go 量化工具 - A股数据拉取、回测与交易信号",
	}

	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "config.yaml", "配置文件路径")

	rootCmd.AddCommand(fetchCmd())
	rootCmd.AddCommand(backtestCmd())
	rootCmd.AddCommand(signalCmd())
	rootCmd.AddCommand(forwardCmd())
	rootCmd.AddCommand(analyzeCmd())
	rootCmd.AddCommand(listCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadConfig() *config.Config {
	c, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}
	return c
}

func fetchCmd() *cobra.Command {
	var (
		today      bool
		force      bool
		date       string
		financials bool
		hs300      bool
		dailyBasic bool
		stkLimit   bool
		moneyflow  bool
		indexData  bool
		startYear  int
		endYear    int
		startDate  string
		endDate    string
	)

	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "拉取股市数据",
		Long: `从 Tushare 拉取 A 股日线、涨跌停价、资金流向、基本面、财务和指数数据并存储。

使用场景：
  fetch                        拉取全量历史行情（按配置年份）
  fetch --today                拉取今日收盘数据（15:30后可用）
  fetch --today --force        强制重新拉取今日数据
  fetch --date 20260721        补拉指定某一天
  fetch --daily-basic          拉取历史 PE/PB/市值/股息率
  fetch --stk-limit            拉取每日涨跌停价格
  fetch --moneyflow            拉取个股资金流向
  fetch --financials           拉取财务指标(ROE/利润率) + 利润表
  fetch --hs300                拉取沪深300成分股名单`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()

			if cfg.Tushare.Token == "" || cfg.Tushare.Token == "YOUR_TUSHARE_TOKEN_HERE" {
				return fmt.Errorf("请在配置文件或环境变量 QUANT_TUSHARE_TOKEN 中设置 Tushare Token")
			}

			client := data.NewClient(cfg.Tushare.BaseURL, cfg.Tushare.Token, cfg.Tushare.RateLimitMs)
			if cfg.Tushare.DailyCallLimit > 0 {
				client.SetDailyLimit(cfg.Tushare.DailyCallLimit)
			}
			fetcher := data.NewFetcher(client, cfg.Data.RawDir, cfg.Fetch.StockPrefixes)
			ctx := context.Background()

			if hs300 {
				_, err := fetcher.FetchHs300(ctx)
				return err
			}

			if indexData {
				return fetcher.FetchIndexData(ctx, cfg.Fetch.StartYear, cfg.Fetch.EndYear)
			}

			if financials {
				sd := fmt.Sprintf("%d0101", cfg.Fetch.StartYear)
				ed := fmt.Sprintf("%d1231", cfg.Fetch.EndYear)
				return fetcher.FetchFinancials(ctx, sd, ed, cfg.Fetch.MinMarketCap)
			}

			if dailyBasic {
				return fetcher.FetchDailyBasicHistory(ctx, cfg.Fetch.StartYear, cfg.Fetch.EndYear, cfg.Fetch.MinMarketCap)
			}

			if stkLimit || moneyflow {
				sd, ed := resolveAuxFetchRange(today, date, startDate, endDate, startYear, endYear, cfg)
				if stkLimit {
					if err := fetcher.FetchStkLimitRange(ctx, sd, ed); err != nil {
						return err
					}
				}
				if moneyflow {
					if err := fetcher.FetchMoneyflowRange(ctx, sd, ed); err != nil {
						return err
					}
				}
				return nil
			}

			if date != "" {
				bars, err := fetcher.FetchDate(ctx, date, force)
				if err != nil {
					return err
				}
				if bars != nil {
					fmt.Printf(">>> 共 %d 条\n", len(bars))
				}
				return nil
			}

			if today {
				bars, err := fetcher.FetchToday(ctx, force)
				if err != nil {
					return err
				}
				if bars != nil {
					fmt.Printf(">>> 今日数据共 %d 条\n", len(bars))
				}
				return nil
			}

			if startDate != "" && endDate != "" {
				return fetcher.FetchDateRange(ctx, startDate, endDate)
			}

			if startYear == 0 {
				startYear = cfg.Fetch.StartYear
			}
			if endYear == 0 {
				endYear = cfg.Fetch.EndYear
			}

			if err := data.InitSQLite(cfg.Data.DBPath); err != nil {
				return fmt.Errorf("初始化数据库失败: %w", err)
			}

			return fetcher.FetchHistorical(ctx, startYear, endYear, cfg.Fetch.MinMarketCap)
		},
	}

	cmd.Flags().BoolVar(&today, "today", false, "拉取今日收盘数据（15:30后可用）")
	cmd.Flags().BoolVar(&force, "force", false, "强制重新拉取（覆盖已有文件）")
	cmd.Flags().StringVar(&date, "date", "", "拉取指定日期 (YYYYMMDD)")
	cmd.Flags().BoolVar(&financials, "financials", false, "拉取财务指标(ROE等) + 利润表")
	cmd.Flags().BoolVar(&hs300, "hs300", false, "拉取沪深300成分股名单")
	cmd.Flags().BoolVar(&indexData, "index", false, "拉取上证/深证/创业板指数数据")
	cmd.Flags().BoolVar(&dailyBasic, "daily-basic", false, "拉取PE/PB/市值/股息率历史数据")
	cmd.Flags().BoolVar(&stkLimit, "stk-limit", false, "拉取每日涨跌停价格")
	cmd.Flags().BoolVar(&moneyflow, "moneyflow", false, "拉取个股资金流向")
	cmd.Flags().IntVar(&startYear, "start-year", 0, "起始年份")
	cmd.Flags().IntVar(&endYear, "end-year", 0, "结束年份")
	cmd.Flags().StringVar(&startDate, "start", "", "起始日期 (YYYYMMDD)")
	cmd.Flags().StringVar(&endDate, "end", "", "结束日期 (YYYYMMDD)")

	return cmd
}

func backtestCmd() *cobra.Command {
	var (
		strategyNames []string
		startDate     string
		endDate       string
		capital       float64
		topN          int
		allowAdjusted bool
	)

	cmd := &cobra.Command{
		Use:   "backtest",
		Short: "运行量化策略回测",
		Long:  `加载本地数据，对指定策略进行历史回测，输出绩效报告。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			reg := strategy.DefaultRegistry()

			var selectedStrategies []strategy.Strategy
			if len(strategyNames) == 0 {
				strategyNames = cfg.Signal.DefaultStrategies
			}
			for _, name := range strategyNames {
				s, ok := reg.Get(name)
				if !ok {
					fmt.Printf("警告: 策略 %q 不存在，跳过\n", name)
					continue
				}
				selectedStrategies = append(selectedStrategies, s)
			}
			if len(selectedStrategies) == 0 {
				return fmt.Errorf("没有可用的策略")
			}

			fetcher := data.NewFetcher(nil, cfg.Data.RawDir, nil)
			fundStore := loadFundStore(fetcher)
			if fundStore != nil {
				for _, s := range selectedStrategies {
					if u, ok := s.(strategy.FundStoreUser); ok {
						u.SetFundStore(fundStore)
					}
				}
			}

			bars, err := data.ReadParquetDir(cfg.Data.RawDir + "/daily")
			if err != nil {
				return fmt.Errorf("加载数据失败: %w", err)
			}
			bars = applyStkLimitStore(fetcher, bars)

			if startDate != "" {
				bars = filterByDateRange(bars, startDate, endDate)
			}
			if q := data.CheckPriceDataQuality(bars); !q.HasCompleteRawPrices() && !allowAdjusted {
				return fmt.Errorf("%s；回测成交需要真实价字段，请重新拉取行情数据，或临时使用 --allow-adjusted-trades", q.Summary())
			}

			sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
			codeMap := data.GroupByCode(bars)
			codes := sortedCodes(codeMap)
			fmt.Printf("加载 %d 只股票, %d 条记录\n", len(codeMap), len(bars))

			if capital == 0 {
				capital = cfg.Backtest.InitialCapital
			}
			btCfg := backtest.Config{
				InitialCapital: capital,
				Commission:     cfg.Backtest.Commission,
				Slippage:       cfg.Backtest.Slippage,
			}

			for _, s := range selectedStrategies {
				if h, ok := s.(strategy.HistoricalUniverseUser); ok {
					h.SetHistoricalUniverse(codeMap)
				}
				fmt.Printf("\n========== 策略: %s ==========\n", s.Name())
				count := 0
				var bestResult *backtest.Result
				var bestCode string
				var bestEquity float64
				bestSet := false
				var metricsList []backtest.PerformanceMetrics

				for _, code := range codes {
					stockBars := codeMap[code]
					sort.Slice(stockBars, func(i, j int) bool { return stockBars[i].TradeDate < stockBars[j].TradeDate })
					if len(stockBars) < s.Warmup() {
						continue
					}
					wrapFn := func(bars []data.DailyBar, idx int) strategy.SignalType {
						return s.Signal(bars, idx)
					}
					result := backtest.Run(stockBars, wrapFn, btCfg)
					if result.TradeCount > 0 {
						returnPct := (result.FinalEquity - capital) / capital * 100
						if !bestSet || returnPct > bestEquity {
							bestEquity = returnPct
							bestResult = result
							bestCode = code
							bestSet = true
						}
						metricsList = append(metricsList, backtest.CalculateMetrics(result, capital, cfg.Backtest.RiskFreeRate, 252))
						count++
						if topN > 0 && count >= topN {
							break
						}
					}
				}

				fmt.Printf("有效回测: %d 只股票\n", count)
				printBacktestAggregate(metricsList)
				if bestResult != nil {
					fmt.Printf("\n最佳表现: %s\n", bestCode)
					metrics := backtest.CalculateMetrics(bestResult, capital, cfg.Backtest.RiskFreeRate, 252)
					metrics.Print()
				}
			}

			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&strategyNames, "strategy", "s", nil, "策略名称 (多个用逗号分隔)")
	cmd.Flags().StringVar(&startDate, "start", "", "起始日期 (YYYYMMDD)")
	cmd.Flags().StringVar(&endDate, "end", "", "结束日期 (YYYYMMDD)")
	cmd.Flags().Float64Var(&capital, "capital", 0, "初始资金")
	cmd.Flags().IntVarP(&topN, "top", "n", 0, "只回测前 N 只股票，按代码排序 (0=全部)")
	cmd.Flags().BoolVar(&allowAdjusted, "allow-adjusted-trades", false, "允许用复权价近似成交价（仅用于旧数据临时验证）")

	return cmd
}

func signalCmd() *cobra.Command {
	var (
		strategyNames []string
		format        string
		topN          int
		realtimeOn    bool
	)
	realtimeOn = true

	cmd := &cobra.Command{
		Use:   "signal",
		Short: "生成今日买卖信号",
		Long:  `基于本地数据和多策略分析，生成今日的买入/卖出建议。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			reg := strategy.DefaultRegistry()

			var selectedStrategies []strategy.Strategy
			if len(strategyNames) == 0 {
				strategyNames = cfg.Signal.DefaultStrategies
			}
			for _, name := range strategyNames {
				s, ok := reg.Get(name)
				if !ok {
					fmt.Printf("警告: 策略 %q 不存在，跳过\n", name)
					continue
				}
				selectedStrategies = append(selectedStrategies, s)
			}
			if len(selectedStrategies) == 0 {
				return fmt.Errorf("没有可用的策略")
			}

			bars, err := data.ReadParquetDir(cfg.Data.RawDir + "/daily")
			if err != nil {
				return fmt.Errorf("加载数据失败: %w", err)
			}
			bars = applyStkLimitStore(data.NewFetcher(nil, cfg.Data.RawDir, nil), bars)
			sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
			codeMap := data.GroupByCode(bars)

			var latestDate string
			for _, stockBars := range codeMap {
				sort.Slice(stockBars, func(i, j int) bool { return stockBars[i].TradeDate < stockBars[j].TradeDate })
				last := stockBars[len(stockBars)-1].TradeDate
				if last > latestDate {
					latestDate = last
				}
			}
			filteredMap := make(map[string][]data.DailyBar)
			skipped := 0
			for code, stockBars := range codeMap {
				if stockBars[len(stockBars)-1].TradeDate == latestDate {
					filteredMap[code] = stockBars
				} else {
					skipped++
				}
			}
			codeMap = filteredMap
			fmt.Printf("最新交易日: %s, 有效股票: %d (跳过%d只过期股票)\n", latestDate, len(codeMap), skipped)

			stockNames := data.LoadStockNames(cfg.Data.RawDir + "/stocks.parquet")
			stFiltered := 0
			for code, name := range stockNames {
				if strings.Contains(name, "ST") || strings.Contains(name, "*ST") {
					delete(codeMap, code)
					stFiltered++
				}
			}
			if stFiltered > 0 {
				fmt.Printf("过滤ST股: %d只, 剩余: %d只\n", stFiltered, len(codeMap))
			}

			fetcher := data.NewFetcher(nil, cfg.Data.RawDir, nil)
			fundStore := loadFundStore(fetcher)
			if fundStore != nil {
				if cfg.Fetch.MinMarketCap > 0 {
					noBasicFiltered := 0
					for code := range codeMap {
						_, _, hasBasic := fundStore.GetLatestPE(code)
						if !hasBasic {
							delete(codeMap, code)
							noBasicFiltered++
						}
					}
					if noBasicFiltered > 0 {
						fmt.Printf("过滤无基本面数据(小盘股): %d只, 剩余: %d只\n", noBasicFiltered, len(codeMap))
					}
				}
				for _, s := range selectedStrategies {
					if u, ok := s.(strategy.FundStoreUser); ok {
						u.SetFundStore(fundStore)
					}
				}
			}
			rawQuality := data.CheckPriceDataQuality(recentBarsForQuality(codeMap, 2))
			hasRecentRawPrices := rawQuality.HasCompleteRawPrices()
			if !hasRecentRawPrices {
				fmt.Printf("警告: 最近交易日%s；将跳过依赖真实成交价的策略，持仓/前向验证需重拉数据后才准确\n", rawQuality.Summary())
				selectedStrategies = filterStrategies(selectedStrategies, map[string]bool{"limit_up": true})
				strategyNames = strategyNamesFrom(selectedStrategies)
				if len(selectedStrategies) == 0 {
					return fmt.Errorf("真实价字段缺失后没有可用策略")
				}
			}

			if topN == 0 {
				topN = cfg.Signal.TopN
			}

			reporter := signal.NewReporter(format)

			fmt.Printf("策略: %s\n", strings.Join(strategyNames, ", "))
			fmt.Printf("股票数: %d, 数据量: %d\n", len(codeMap), len(bars))

			marketStatus := market.Analyze(bars)
			if marketStatus != nil {
				marketStatus.Print()
			}

			summary, err := news.Analyze(context.Background(), nil, cfg.Data.RawDir, 8)
			if err != nil {
				fmt.Printf("新闻分析: %v\n", err)
			} else if summary != nil {
				summary.Print()
			}

			var portfolioSummary *portfolio.Summary
			ledger, _ := portfolio.Load("portfolio.yaml")
			if ledger != nil {
				portfolioSummary = portfolio.Analyze(ledger, codeMap, stockNames)
			}

			moneyflowStore := loadMoneyflowStore(fetcher)
			results := signal.GenerateWithContextAndMoneyflow(codeMap, selectedStrategies, topN, stockNames, marketStatus, moneyflowStore)

			if realtimeOn {
				limitStore := loadStkLimitStore(fetcher)
				if quoteMap, err := fetchRealtimeQuotes(results, portfolioSummary); err != nil && format == "table" {
					fmt.Printf("实时行情: %v\n", err)
				} else if len(quoteMap) > 0 {
					signal.ApplyRealtimeQuotes(results, quoteMap, limitStore)
					portfolio.ApplyRealtimeQuotes(portfolioSummary, quoteMap)
					if format == "table" {
						fmt.Printf(">>> 实时行情已加载: %d 只\n", len(quoteMap))
					}
				}
			}
			positionDecision := signal.ApplyPositionPolicy(results, marketStatus)
			if format == "table" {
				signal.PrintPositionDecision(positionDecision)
			}

			if portfolioSummary != nil {
				portfolio.PrintSummary(portfolioSummary)
				portfolio.SaveReport(portfolioSummary, cfg.Data.RawDir+"/reports")
			}

			if len(results) == 0 {
				fmt.Println("今日无交易信号")
				return nil
			}

			tradingDates := data.LoadTradeDates(cfg.Data.RawDir, bars)
			shortResults := signal.FilterByHorizon(results, strategy.HorizonShort)
			if err := forward.RecordWithDecision(cfg.Data.RawDir+"/../forward_test", shortResults, marketStatus, 5, tradingDates, positionDecision); err != nil && format == "table" {
				fmt.Printf("前向测试记录失败: %v\n", err)
			}

			return reporter.Print(results)
		},
	}

	cmd.Flags().StringSliceVarP(&strategyNames, "strategy", "s", nil, "策略名称 (多个用逗号分隔)")
	cmd.Flags().StringVarP(&format, "format", "f", "table", "输出格式: table, csv, json")
	cmd.Flags().IntVarP(&topN, "top", "n", 0, "显示前 N 条信号")
	cmd.Flags().BoolVar(&realtimeOn, "realtime", true, "使用新浪实时行情对候选股和持仓做盘中校验")

	return cmd
}

func forwardCmd() *cobra.Command {
	var dir string
	var allowAdjusted bool
	cmd := &cobra.Command{
		Use:   "forward",
		Short: "管理前向测试记录",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "data/forward_test", "前向测试记录目录")

	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "用本地行情回填前向测试收益",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			bars, err := data.ReadParquetDir(cfg.Data.RawDir + "/daily")
			if err != nil {
				return fmt.Errorf("加载数据失败: %w", err)
			}
			bars = applyStkLimitStore(data.NewFetcher(nil, cfg.Data.RawDir, nil), bars)
			if q := data.CheckPriceDataQuality(bars); !q.HasCompleteRawPrices() && !allowAdjusted {
				return fmt.Errorf("%s；前向验证需要真实价字段，请重新拉取行情数据，或临时使用 --allow-adjusted-trades", q.Summary())
			}
			codeMap := data.GroupByCode(bars)
			updated, err := forward.Validate(dir, codeMap)
			if err != nil {
				return err
			}
			fmt.Printf("前向测试验证完成: 更新 %d 个字段\n", updated)
			return nil
		},
	}
	validateCmd.Flags().BoolVar(&allowAdjusted, "allow-adjusted-trades", false, "允许用复权价近似前向验证价格（仅用于旧数据临时验证）")
	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "迁移前向测试记录到当前 CSV schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := forward.Migrate(dir); err != nil {
				return err
			}
			fmt.Printf("前向测试记录已迁移: %s\n", dir)
			return nil
		},
	}
	cmd.AddCommand(validateCmd)
	cmd.AddCommand(migrateCmd)
	return cmd
}

func analyzeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "analyze <股票代码>",
		Short: "分析单只股票",
		Long:  `显示单只股票的价格/均线/策略信号/基本面/复权换算。示例: ./go-quant analyze 002594.SZ`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := args[0]
			cfg := loadConfig()

			r, err := analyze.Run(code, cfgPath)
			if err != nil {
				return err
			}

			// try to get adj factor
			client := data.NewClient(cfg.Tushare.BaseURL, cfg.Tushare.Token, cfg.Tushare.RateLimitMs)
			ctx := context.Background()
			today := time.Now().Format("20060102")
			yesterday := time.Now().AddDate(0, 0, -1).Format("20060102")
			factors, err := client.FetchAdjFactors(ctx, code, yesterday, today)
			if err == nil && len(factors) > 0 && !r.Latest.HasRawPrices() {
				r.SetAdjFactor(factors[len(factors)-1].AdjFactor)
			}

			r.Print()
			return nil
		},
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出所有可用策略",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := strategy.DefaultRegistry()
			fmt.Println("可用策略:")
			for _, name := range reg.List() {
				s, _ := reg.Get(name)
				horizon := strategy.HorizonForStrategy(name)
				fmt.Printf("  %s [%s] (预热期: %d 天)\n", name, strategy.HorizonLabel(horizon), s.Warmup())
			}
			return nil
		},
	}
}

func loadFundStore(fetcher *data.Fetcher) *data.FundamentalStore {
	store := data.NewFundamentalStore()

	basicStore, err := fetcher.LoadDailyBasicStore()
	if err == nil && basicStore != nil {
		store.MergeFrom(basicStore)
	}

	finaStore, err := fetcher.LoadFinaStore()
	if err == nil && finaStore != nil {
		store.MergeFrom(finaStore)
	}

	if !store.HasData() {
		return nil
	}
	return store
}

func loadMoneyflowStore(fetcher *data.Fetcher) *data.MoneyflowStore {
	store, err := fetcher.LoadMoneyflowStore()
	if err != nil {
		fmt.Printf("警告: 加载资金流向失败: %v\n", err)
		return nil
	}
	return store
}

func loadStkLimitStore(fetcher *data.Fetcher) *data.StkLimitStore {
	store, err := fetcher.LoadStkLimitStore()
	if err != nil {
		fmt.Printf("警告: 加载涨跌停价格失败: %v\n", err)
		return nil
	}
	return store
}

func applyStkLimitStore(fetcher *data.Fetcher, bars []data.DailyBar) []data.DailyBar {
	store := loadStkLimitStore(fetcher)
	return data.ApplyStkLimits(bars, store)
}

func fetchRealtimeQuotes(results []signal.SignalResult, portfolioSummary *portfolio.Summary) (map[string]realtime.Quote, error) {
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

func resolveAuxFetchRange(today bool, date, startDate, endDate string, startYear, endYear int, cfg *config.Config) (string, string) {
	if date != "" {
		return date, date
	}
	if today {
		d := time.Now().Format("20060102")
		return d, d
	}
	if startDate != "" && endDate != "" {
		return startDate, endDate
	}
	if startYear == 0 {
		startYear = cfg.Fetch.StartYear
	}
	if endYear == 0 {
		endYear = cfg.Fetch.EndYear
	}
	return fmt.Sprintf("%d0101", startYear), fmt.Sprintf("%d1231", endYear)
}

func filterByDateRange(bars []data.DailyBar, start, end string) []data.DailyBar {
	var filtered []data.DailyBar
	for _, b := range bars {
		if start != "" && b.TradeDate < start {
			continue
		}
		if end != "" && b.TradeDate > end {
			continue
		}
		filtered = append(filtered, b)
	}
	return filtered
}

func sortedCodes(codeMap map[string][]data.DailyBar) []string {
	codes := make([]string, 0, len(codeMap))
	for code := range codeMap {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
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

func recentBarsForQuality(codeMap map[string][]data.DailyBar, lookback int) []data.DailyBar {
	if lookback <= 0 {
		lookback = 1
	}
	recent := make([]data.DailyBar, 0, len(codeMap)*lookback)
	for _, bars := range codeMap {
		if len(bars) == 0 {
			continue
		}
		start := len(bars) - lookback
		if start < 0 {
			start = 0
		}
		recent = append(recent, bars[start:]...)
	}
	return recent
}

func printBacktestAggregate(metricsList []backtest.PerformanceMetrics) {
	if len(metricsList) == 0 {
		return
	}
	returns := make([]float64, 0, len(metricsList))
	var sumReturn, sumAnnual, sumDD, sumSharpe float64
	winStocks := 0
	totalTrades := 0
	for _, m := range metricsList {
		returns = append(returns, m.TotalReturn)
		sumReturn += m.TotalReturn
		sumAnnual += m.AnnualizedReturn
		sumDD += m.MaxDrawdown
		sumSharpe += m.SharpeRatio
		totalTrades += m.TotalTrades
		if m.TotalReturn > 0 {
			winStocks++
		}
	}
	sort.Float64s(returns)
	median := returns[len(returns)/2]
	if len(returns)%2 == 0 {
		median = (returns[len(returns)/2-1] + returns[len(returns)/2]) / 2
	}
	n := float64(len(metricsList))
	fmt.Printf("\n========== 样本汇总 ==========\n")
	fmt.Printf("样本数:       %d 只\n", len(metricsList))
	fmt.Printf("平均收益:     %11.2f%%\n", sumReturn/n)
	fmt.Printf("中位收益:     %11.2f%%\n", median)
	fmt.Printf("平均年化:     %11.2f%%\n", sumAnnual/n)
	fmt.Printf("平均最大回撤: %11.2f%%\n", sumDD/n)
	fmt.Printf("平均夏普:     %11.2f\n", sumSharpe/n)
	fmt.Printf("正收益股票:   %d / %d\n", winStocks, len(metricsList))
	fmt.Printf("总交易次数:   %d\n", totalTrades)
	fmt.Printf("收益区间:     %11.2f%% ~ %.2f%%\n", returns[0], returns[len(returns)-1])
	fmt.Println("==============================")
}
