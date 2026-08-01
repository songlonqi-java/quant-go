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
	"quant/internal/dataset"
	"quant/internal/execution"
	"quant/internal/forward"
	"quant/internal/market"
	"quant/internal/portfolio"
	"quant/internal/realtime"
	"quant/internal/sector"
	"quant/internal/signal"
	"quant/internal/strategy"
	"quant/internal/validation"
	"quant/internal/value"
	"quant/internal/workflow/sectorbuild"
	signalworkflow "quant/internal/workflow/signal"

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
	rootCmd.AddCommand(marketCmd())
	rootCmd.AddCommand(sectorCmd())
	rootCmd.AddCommand(backtestCmd())
	rootCmd.AddCommand(signalCmd())
	rootCmd.AddCommand(forwardCmd())
	rootCmd.AddCommand(validationCmd())
	rootCmd.AddCommand(valueCmd())
	rootCmd.AddCommand(analyzeCmd())
	rootCmd.AddCommand(listCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func marketCmd() *cobra.Command {
	var (
		window      time.Duration
		minCoverage float64
		source      string
	)
	cmd := &cobra.Command{
		Use:   "market",
		Short: "查看全市场盘中行情",
	}
	realtimeCmd := &cobra.Command{
		Use:   "realtime",
		Short: "分批拉取全市场实时行情并计算盘中宽度",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !realtime.IsAShareTradingHours(time.Now()) {
				return fmt.Errorf("全市场实时行情只在A股连续竞价时段运行（09:30-11:30、13:00-15:00，Asia/Shanghai）")
			}
			if window < time.Second {
				return fmt.Errorf("刷新窗口至少为1秒")
			}
			if minCoverage <= 0 || minCoverage > 100 {
				return fmt.Errorf("最低覆盖率应在(0,100]之间")
			}
			cfg := loadConfig()
			ds, err := dataset.Load(dataset.LoadOptions{
				RawDir:       cfg.Data.RawDir,
				LatestOnly:   true,
				FilterST:     true,
				MinMarketCap: cfg.Fetch.MinMarketCap,
			})
			if err != nil {
				return err
			}
			codes := market.SortedQuoteCodes(ds.CodeMap)
			provider, err := realtime.NewProvider(source)
			if err != nil {
				return err
			}
			paced, ok := provider.(realtime.PacedProvider)
			if !ok {
				return fmt.Errorf("实时行情提供方不支持全市场限速刷新")
			}
			quotes, stats, fetchErr := paced.FetchPaced(codes, window)
			status := market.AnalyzeIntraday(quotes, len(codes), ds.StkLimits)
			status.Print()
			fmt.Printf(">>> %s全市场行情: %d批，批次间隔%s，耗时%s\n", realtimeFetchSource(stats),
				stats.Batches, stats.Interval.Round(time.Millisecond), stats.Elapsed.Round(time.Millisecond))
			if fetchErr != nil {
				return fetchErr
			}
			if status.CoveragePct < minCoverage {
				return fmt.Errorf("盘中行情覆盖率%.1f%%低于%.1f%%，不改变仓位结论", status.CoveragePct, minCoverage)
			}
			return nil
		},
	}
	realtimeCmd.Flags().DurationVar(&window, "window", time.Minute, "完成全市场刷新所用窗口，例如1m")
	realtimeCmd.Flags().Float64Var(&minCoverage, "min-coverage", 90, "接受盘中快照所需的最低覆盖率")
	realtimeCmd.Flags().StringVar(&source, "source", realtime.SourceAuto, "实时行情来源: auto、eastmoney、sina")
	cmd.AddCommand(realtimeCmd)
	return cmd
}

func sectorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sector",
		Short: "构建和查看板块数据",
	}

	var (
		today     bool
		date      string
		startDate string
		endDate   string
	)
	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "聚合并持久化行业板块日度数据",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()

			bars, err := data.ReadParquetDir(cfg.Data.RawDir + "/daily")
			if err != nil {
				return fmt.Errorf("加载日线数据失败: %w", err)
			}
			dates := resolveSectorBuildDates(bars, today, date, startDate, endDate)
			if len(dates) == 0 {
				return fmt.Errorf("没有可构建的交易日期")
			}
			built, err := sectorbuild.BuildDates(cfg.Data.RawDir, dates)
			if err != nil {
				return err
			}

			fmt.Printf(">>> 板块日度数据已写入: %d 行, 日期 %s ~ %s → %s\n",
				built.Rows, dates[0], dates[len(dates)-1], cfg.Data.RawDir+"/sector_daily")
			built.Report.Print()
			return nil
		},
	}
	buildCmd.Flags().BoolVar(&today, "today", false, "构建最新本地交易日板块数据")
	buildCmd.Flags().StringVar(&date, "date", "", "构建指定日期 (YYYYMMDD)")
	buildCmd.Flags().StringVar(&startDate, "start", "", "起始日期 (YYYYMMDD)")
	buildCmd.Flags().StringVar(&endDate, "end", "", "结束日期 (YYYYMMDD)")

	cmd.AddCommand(buildCmd)
	return cmd
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
  fetch --today                拉取今日收盘数据（16:00后可用）
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

			if dailyBasic && (today || date != "") {
				tradeDate := date
				if today {
					tradeDate = time.Now().Format("20060102")
				}
				_, err := fetcher.FetchDailyBasicForDate(ctx, tradeDate)
				return err
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
				if err := fetcher.FetchIndexData(ctx, time.Now().Year(), time.Now().Year()); err != nil {
					return fmt.Errorf("拉取今日指数失败: %w", err)
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

	cmd.Flags().BoolVar(&today, "today", false, "拉取今日收盘数据（16:00后可用）")
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
		ensemble      bool
		ablation      string
		allowAdjusted bool
	)

	cmd := &cobra.Command{
		Use:   "backtest",
		Short: "运行量化策略回测",
		Long: `加载本地数据并输出绩效报告。

默认模式逐只股票、逐个策略独立满仓，仅用于策略诊断；--ensemble 使用
多股票共享资金账户，复用正式信号的聚合、市场状态、资格过滤、Top-N 和组合预算。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			if ablation != "" && !ensemble {
				return fmt.Errorf("--ablation 仅支持 --ensemble 共享资金组合回测")
			}
			liquidityPolicy := cfg.Liquidity.Policy()
			if err := liquidityPolicy.Validate(); err != nil {
				return fmt.Errorf("流动性配置无效: %w", err)
			}
			reg := strategy.DefaultRegistry()

			var selectedStrategies []strategy.Strategy
			strategyNames = resolveBacktestStrategyNames(strategyNames, cfg.Signal.DefaultStrategies)
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
			fundStore := dataset.LoadFundamentals(cfg.Data.RawDir)
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

			qualityBars := bars
			if startDate != "" || endDate != "" {
				qualityBars = filterByDateRange(bars, startDate, endDate)
			}
			if !ensemble && (startDate != "" || endDate != "") {
				bars = filterByDateRange(bars, startDate, endDate)
			}
			if q := data.CheckPriceDataQuality(qualityBars); !q.HasCompleteRawPrices() && !allowAdjusted {
				return fmt.Errorf("%s；回测成交需要真实价字段，请重新拉取行情数据，或临时使用 --allow-adjusted-trades", q.Summary())
			}

			sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
			codeMap := data.GroupByCode(bars)
			stockInfos := data.LoadStockInfos(cfg.Data.RawDir + "/stocks.parquet")
			codes := sortedCodes(codeMap)
			fmt.Printf("加载 %d 只股票, %d 条记录\n", len(codeMap), len(bars))

			if capital == 0 {
				capital = cfg.Backtest.InitialCapital
			}
			btCfg := backtest.Config{
				InitialCapital: capital,
				Commission:     cfg.Backtest.Commission,
				Slippage:       cfg.Backtest.Slippage,
				LotSize:        cfg.Backtest.LotSize,
				ManagedExits:   true,
				Liquidity:      liquidityPolicy,
			}

			if ensemble {
				portfolioCfg := cfg.Portfolio.Normalized(capital)
				moneyflows, moneyflowErr := fetcher.LoadMoneyflowStore()
				if moneyflowErr != nil {
					fmt.Printf("警告: 加载资金流数据失败: %v\n", moneyflowErr)
				}
				memberships, membershipErr := sector.LoadIndustryMemberships(cfg.Data.RawDir)
				if membershipErr != nil {
					fmt.Printf("警告: 加载行业归属失败，组合回测将不启用行业上限: %v\n", membershipErr)
				}
				candidateTopN := topN
				if candidateTopN == 0 {
					candidateTopN = cfg.Signal.TopN
				}
				portfolioOptions := backtest.PortfolioOptions{
					CodeMap:      codeMap,
					StockNames:   data.LoadStockNames(cfg.Data.RawDir + "/stocks.parquet"),
					Strategies:   selectedStrategies,
					Moneyflows:   moneyflows,
					StockInfos:   stockInfos,
					Fundamentals: fundStore,
					Liquidity:    liquidityPolicy,
					StartDate:    startDate,
					EndDate:      endDate,
					TopN:         candidateTopN,
					Config:       btCfg,
					MaxTotalPct:  portfolioCfg.MaxTotalPositionPct,
					MaxSinglePct: portfolioCfg.MaxSinglePositionPct,
					MaxSectorPct: portfolioCfg.MaxSectorPositionPct,
					SectorName: func(code, date string) string {
						if membership, ok := memberships.PrimaryIndustry(code, date); ok {
							return membership.SectorName
						}
						return ""
					},
				}
				if ablation != "" {
					baselineNames := withoutStrategy(strategyNames, ablation)
					if len(baselineNames) == 0 {
						return fmt.Errorf("消融基线至少需要一个非 %s 策略", ablation)
					}
					variantNames := withStrategy(baselineNames, ablation)
					baselineStrategies, err := registeredStrategies(baselineNames)
					if err != nil {
						return err
					}
					variantStrategies, err := registeredStrategies(variantNames)
					if err != nil {
						return err
					}
					environmentStarted := time.Now()
					fmt.Println("\n>>> 准备可复用的消融回测环境...")
					environment, err := backtest.PreparePortfolioEnvironment(codeMap)
					if err != nil {
						return fmt.Errorf("准备消融回测环境: %w", err)
					}
					portfolioOptions.Environment = environment
					portfolioOptions.CodeMap = nil
					fmt.Printf(">>> 回测环境已准备，基线和实验组共用不可变行情/索引/市场状态（耗时 %s）\n",
						time.Since(environmentStarted).Round(time.Millisecond))
					portfolioOptions.Strategies = baselineStrategies
					baselineStarted := time.Now()
					fmt.Printf(">>> 运行消融基线（%d 个策略）...\n", len(baselineStrategies))
					baselineResult, err := backtest.RunPortfolio(portfolioOptions)
					if err != nil {
						return fmt.Errorf("运行消融基线: %w", err)
					}
					portfolioOptions.Strategies = variantStrategies
					fmt.Printf(">>> 基线完成（耗时 %s），运行加入 %s 的实验组（%d 个策略）...\n",
						time.Since(baselineStarted).Round(time.Millisecond), ablation, len(variantStrategies))
					variantStarted := time.Now()
					variantResult, err := backtest.RunPortfolio(portfolioOptions)
					if err != nil {
						return fmt.Errorf("运行加入 %s 的组合: %w", ablation, err)
					}
					fmt.Printf(">>> 实验组完成（耗时 %s）\n", time.Since(variantStarted).Round(time.Millisecond))
					comparison := backtest.CompareAblation(baselineResult, variantResult, capital, cfg.Backtest.RiskFreeRate, 252)
					printAblationComparison(ablation, baselineNames, variantNames, comparison)
					return nil
				}
				result, err := backtest.RunPortfolio(portfolioOptions)
				if err != nil {
					return err
				}
				fmt.Println("\n========== 多策略组合回测 ==========")
				fmt.Printf("策略: %s\n", strings.Join(strategyNames, ", "))
				fmt.Printf("候选上限: 每周期 %d，组合/单票/行业上限: %.0f%% / %.0f%% / %.0f%%\n",
					candidateTopN, portfolioCfg.MaxTotalPositionPct, portfolioCfg.MaxSinglePositionPct, portfolioCfg.MaxSectorPositionPct)
				metrics := backtest.CalculateMetrics(result, capital, cfg.Backtest.RiskFreeRate, 252)
				metrics.Print()
				printCostAttribution(backtest.CalculateCostAttribution(result, capital))
				fmt.Printf("未成交/延迟成交次数: %d\n", result.SkippedSignals)
				printExitSummary(result)
				fmt.Println("说明: evidence.json 不会回灌历史日期，以免使用未来汇总证据；组合回测按当日信号资格规则逐日决策。")
				return nil
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
				exitSummary := &backtest.Result{ExitReasonCounts: make(map[string]int)}
				strategyCfg := btCfg
				strategyCfg.Horizon = strategy.HorizonForStrategy(s.Name())

				for _, code := range codes {
					stockBars := codeMap[code]
					sort.Slice(stockBars, func(i, j int) bool { return stockBars[i].TradeDate < stockBars[j].TradeDate })
					if len(stockBars) < s.Warmup() {
						continue
					}
					wrapFn := func(bars []data.DailyBar, idx int) strategy.SignalType {
						return s.Signal(bars, idx)
					}
					runCfg := strategyCfg
					info := stockInfos[code]
					runCfg.ListDate = info.ListDate
					// stocks.parquet stores the current name. Applying it to every
					// historical date would turn today's ST status into look-ahead.
					runCfg.StockName = ""
					if fundStore != nil {
						currentCode := code
						runCfg.TurnoverRate = func(date string) (float64, bool) {
							basic := fundStore.GetDailyBasic(currentCode, date)
							if basic == nil {
								return 0, false
							}
							return basic.TurnoverRate, true
						}
					}
					result := backtest.Run(stockBars, wrapFn, runCfg)
					if result.TradeCount > 0 {
						mergeExitSummary(exitSummary, result)
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
				printExitSummary(exitSummary)
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
	cmd.Flags().IntVarP(&topN, "top", "n", 0, "单策略模式只测前N只股票；组合模式为每周期候选数（0=配置值）")
	cmd.Flags().BoolVar(&ensemble, "ensemble", false, "运行共享资金账户的多股票、多策略组合回测")
	cmd.Flags().StringVar(&ablation, "ablation", "", "组合消融：比较基线与加入指定策略后的总绩效、换手和逐年稳定性")
	cmd.Flags().BoolVar(&allowAdjusted, "allow-adjusted-trades", false, "允许用复权价近似成交价（仅用于旧数据临时验证）")

	return cmd
}

func mergeExitSummary(target, source *backtest.Result) {
	if target == nil || source == nil {
		return
	}
	targetExits := 0
	for _, count := range target.ExitReasonCounts {
		targetExits += count
	}
	if target.ExitReasonCounts == nil {
		target.ExitReasonCounts = make(map[string]int)
	}
	for reason, count := range source.ExitReasonCounts {
		target.ExitReasonCounts[reason] += count
	}
	target.DelayedExitTrades += source.DelayedExitTrades
	target.ExitDelayDays += source.ExitDelayDays
	if source.MaxExitDelayDays > target.MaxExitDelayDays {
		target.MaxExitDelayDays = source.MaxExitDelayDays
	}
	target.TailLossTrades += source.TailLossTrades
	target.ImpactedTrades += source.ImpactedTrades
	target.TotalImpactRate += source.TotalImpactRate
	if source.MaxImpactRate > target.MaxImpactRate {
		target.MaxImpactRate = source.MaxImpactRate
	}
	if source.MaxParticipationPct > target.MaxParticipationPct {
		target.MaxParticipationPct = source.MaxParticipationPct
	}
	if targetExits == 0 || source.WorstTradeReturnPct < target.WorstTradeReturnPct {
		target.WorstTradeReturnPct = source.WorstTradeReturnPct
	}
}

func printExitSummary(result *backtest.Result) {
	if result == nil || len(result.ExitReasonCounts) == 0 {
		return
	}
	reasons := make([]string, 0, len(result.ExitReasonCounts))
	for reason := range result.ExitReasonCounts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		parts = append(parts, fmt.Sprintf("%s=%d", reason, result.ExitReasonCounts[reason]))
	}
	fmt.Printf("退出原因:     %s\n", strings.Join(parts, ", "))
	fmt.Printf("延迟退出:     %d 笔 / %d 个交易日（最长 %d 日）\n", result.DelayedExitTrades, result.ExitDelayDays, result.MaxExitDelayDays)
	fmt.Printf("尾部亏损:     %d 笔，最差单笔净收益 %.2f%%\n", result.TailLossTrades, result.WorstTradeReturnPct)
	if result.ImpactedTrades > 0 {
		fmt.Printf("流动性冲击:   %d 个成交边，平均 %.3f%%，最大 %.3f%%；最大成交占比 %.2f%%\n",
			result.ImpactedTrades, result.TotalImpactRate/float64(result.ImpactedTrades)*100,
			result.MaxImpactRate*100, result.MaxParticipationPct)
	}
}

func printCostAttribution(attribution backtest.CostAttribution) {
	fmt.Println("\n========== 交易成本归因 ==========")
	fmt.Printf("成本前毛盈亏: %12.2f (%8.2f%%)\n", attribution.GrossPnLAmount, attribution.GrossReturnPct)
	fmt.Printf("手续费:       %12.2f\n", attribution.CommissionAmount)
	fmt.Printf("固定滑点:     %12.2f\n", attribution.SlippageAmount)
	fmt.Printf("市场冲击:     %12.2f\n", attribution.ImpactAmount)
	fmt.Printf("总交易成本:   %12.2f (%8.2f%%)\n", attribution.TotalCostAmount, attribution.CostDragPct)
	fmt.Printf("净盈亏:       %12.2f (%8.2f%%)\n", attribution.NetPnLAmount, attribution.NetReturnPct)
	fmt.Println("说明: 毛盈亏按实际成交路径加回成本，不是按无成本仓位重新回测。")
}

func withoutStrategy(names []string, target string) []string {
	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if name != target {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func withStrategy(names []string, target string) []string {
	result := append([]string(nil), names...)
	for _, name := range result {
		if name == target {
			return result
		}
	}
	return append(result, target)
}

func printAblationComparison(name string, baselineNames, variantNames []string, comparison backtest.AblationComparison) {
	base := comparison.BaselineMetrics
	variant := comparison.VariantMetrics
	baseCosts := comparison.BaselineCosts
	variantCosts := comparison.VariantCosts
	fmt.Printf("\n========== 策略消融: %s ==========\n", name)
	fmt.Printf("基线策略: %s\n", strings.Join(baselineNames, ", "))
	fmt.Printf("实验策略: %s\n", strings.Join(variantNames, ", "))
	fmt.Println("指标                 基线          加入因子       差值")
	fmt.Printf("总净收益           %9.2f%%    %9.2f%%    %+9.2f%%\n", base.TotalReturn, variant.TotalReturn, variant.TotalReturn-base.TotalReturn)
	fmt.Printf("成本前毛收益       %9.2f%%    %9.2f%%    %+9.2f%%\n", baseCosts.GrossReturnPct, variantCosts.GrossReturnPct, variantCosts.GrossReturnPct-baseCosts.GrossReturnPct)
	fmt.Printf("交易成本拖累       %9.2f%%    %9.2f%%    %+9.2f%%\n", baseCosts.CostDragPct, variantCosts.CostDragPct, variantCosts.CostDragPct-baseCosts.CostDragPct)
	fmt.Printf("年化收益           %9.2f%%    %9.2f%%    %+9.2f%%\n", base.AnnualizedReturn, variant.AnnualizedReturn, variant.AnnualizedReturn-base.AnnualizedReturn)
	fmt.Printf("最大回撤           %9.2f%%    %9.2f%%    %+9.2f%%\n", base.MaxDrawdown, variant.MaxDrawdown, variant.MaxDrawdown-base.MaxDrawdown)
	fmt.Printf("夏普比率           %9.2f     %9.2f     %+9.2f\n", base.SharpeRatio, variant.SharpeRatio, variant.SharpeRatio-base.SharpeRatio)
	fmt.Printf("半边换手率         %9.2f%%    %9.2f%%    %+9.2f%%\n", comparison.BaselineTurnover, comparison.VariantTurnover, comparison.VariantTurnover-comparison.BaselineTurnover)
	fmt.Printf("成交记录           %9d     %9d     %+9d\n", base.TotalTrades, variant.TotalTrades, variant.TotalTrades-base.TotalTrades)
	fmt.Printf("已平仓胜率         %9.2f%%    %9.2f%%    %+9.2f%%\n", base.WinRate, variant.WinRate, variant.WinRate-base.WinRate)
	fmt.Printf("盈亏比             %9.2f     %9.2f     %+9.2f\n", base.ProfitFactor, variant.ProfitFactor, variant.ProfitFactor-base.ProfitFactor)
	fmt.Printf("平均盈利/亏损      %6.2f%%/%6.2f%%  %6.2f%%/%6.2f%%\n", base.AvgWin, base.AvgLoss, variant.AvgWin, variant.AvgLoss)
	fmt.Printf("成本金额(手/滑/冲) %7.0f/%7.0f/%7.0f  %7.0f/%7.0f/%7.0f\n",
		baseCosts.CommissionAmount, baseCosts.SlippageAmount, baseCosts.ImpactAmount,
		variantCosts.CommissionAmount, variantCosts.SlippageAmount, variantCosts.ImpactAmount)

	baselinePeriods := make(map[string]backtest.PeriodPerformance, len(comparison.BaselinePeriods))
	for _, period := range comparison.BaselinePeriods {
		baselinePeriods[period.Period] = period
	}
	fmt.Println("\n年度       基线收益   实验收益   收益差值   基线回撤   实验回撤   基线/实验换手")
	for _, period := range comparison.VariantPeriods {
		baseline, ok := baselinePeriods[period.Period]
		if !ok {
			continue
		}
		fmt.Printf("%s    %8.2f%%  %8.2f%%  %+8.2f%%  %8.2f%%  %8.2f%%  %7.1f%%/%7.1f%%\n",
			period.Period, baseline.ReturnPct, period.ReturnPct, period.ReturnPct-baseline.ReturnPct,
			baseline.MaxDrawdown, period.MaxDrawdown, baseline.TurnoverPct, period.TurnoverPct)
	}
	fmt.Printf("跨年稳定性: %d/%d 个可比年份收益改善。只有收益、回撤和换手同时可接受，才建议进入默认策略。\n",
		comparison.PositivePeriods, comparison.ComparablePeriods)
	if comparison.Admission.Passed {
		fmt.Println("准入门禁: 通过；仍需重建历史证据并继续前向观察。")
	} else {
		fmt.Printf("准入门禁: 不通过（%s）\n", strings.Join(comparison.Admission.Reasons, "；"))
	}
}

func formatCountMap(counts map[string]int) string {
	if len(counts) == 0 {
		return "无"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func signalCmd() *cobra.Command {
	var (
		strategyNames    []string
		format           string
		topN             int
		watchN           int
		realtimeOn       bool
		marketRealtimeOn bool
		marketWindow     time.Duration
		realtimeSource   string
	)
	realtimeOn = true
	marketRealtimeOn = true
	watchN = 15

	cmd := &cobra.Command{
		Use:   "signal",
		Short: "生成今日买卖信号",
		Long:  `基于本地数据和多策略分析，生成今日的买入/卖出建议。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			inTradingHours := realtime.IsAShareTradingHours(time.Now())
			useRealtime := realtimeOn && inTradingHours
			var provider realtime.Provider
			if useRealtime {
				var err error
				provider, err = realtime.NewProvider(realtimeSource)
				if err != nil {
					return err
				}
			}
			result, err := signalworkflow.Run(context.Background(), signalworkflow.Options{
				Config:              cfg,
				StrategyNames:       strategyNames,
				TopN:                topN,
				WatchN:              watchN,
				Realtime:            useRealtime,
				MarketRealtime:      useRealtime && marketRealtimeOn,
				MarketRefreshWindow: marketWindow,
				PortfolioPath:       "portfolio.yaml",
				ForwardDir:          cfg.Data.RawDir + "/../forward_test",
				RealtimeProvider:    provider,
			})
			if err != nil {
				return err
			}
			reporter := signal.NewReporter(format)

			ds := result.Dataset
			fmt.Printf("最新交易日: %s, 有效股票: %d (跳过%d只过期股票)\n", ds.LatestDate, len(ds.CodeMap), ds.SkippedStale)
			if ds.FilteredST > 0 {
				fmt.Printf("过滤ST股: %d只, 剩余: %d只\n", ds.FilteredST, len(ds.CodeMap))
			}
			if ds.FilteredNoBasic > 0 {
				fmt.Printf("过滤无市值数据: %d只, 剩余: %d只\n", ds.FilteredNoBasic, len(ds.CodeMap))
			}
			if ds.FilteredMarketCap > 0 {
				fmt.Printf("过滤市值不足: %d只, 剩余: %d只\n", ds.FilteredMarketCap, len(ds.CodeMap))
			}
			if !result.PriceQuality.HasCompleteRawPrices() {
				fmt.Printf("警告: 最近交易日%s；将跳过依赖真实成交价的策略，持仓/前向验证需重拉数据后才准确\n", result.PriceQuality.Summary())
			}
			fmt.Printf("策略: %s\n", strings.Join(result.StrategyNames, ", "))
			fmt.Printf("股票数: %d, 数据量: %d\n", len(ds.CodeMap), len(ds.Bars))
			if result.MarketStatus != nil {
				result.MarketStatus.Print()
			}
			if result.IntradayMarket != nil && format == "table" {
				result.IntradayMarket.Print()
			}
			if result.NewsErr != nil {
				fmt.Printf("新闻分析: %v\n", result.NewsErr)
			} else if result.NewsSummary != nil {
				result.NewsSummary.Print()
			}
			if result.SectorErr != nil {
				fmt.Printf("板块分析: %v\n", result.SectorErr)
			} else if result.SectorReport != nil {
				result.SectorReport.Print()
			}
			if realtimeOn && !inTradingHours && format == "table" {
				fmt.Println(">>> 非A股连续竞价时段，未请求实时行情；请使用收盘后的Tushare日线")
			}
			if result.MarketRealtimeErr != nil && format == "table" {
				fmt.Printf("全市场盘中行情: %v\n", result.MarketRealtimeErr)
			} else if result.IntradayMarket != nil && format == "table" {
				fmt.Printf(">>> %s全市场盘中行情已加载: %d/%d，%d批，耗时%s\n", realtimeFetchSource(result.MarketRealtimeStats),
					result.IntradayMarket.Quoted, result.IntradayMarket.Requested,
					result.MarketRealtimeStats.Batches, result.MarketRealtimeStats.Elapsed.Round(time.Millisecond))
			}
			if result.RealtimeErr != nil && format == "table" {
				fmt.Printf("实时行情: %v\n", result.RealtimeErr)
			} else if result.RealtimeLoaded > 0 && format == "table" {
				fmt.Printf(">>> 实时行情已加载: %d 只\n", result.RealtimeLoaded)
			}
			if result.ValidationErr != nil && format == "table" {
				fmt.Printf("历史验证: %v\n", result.ValidationErr)
			} else if result.ValidationStore != nil && format == "table" {
				fmt.Printf(">>> 历史验证已加载: 样本外统计 %d 桶，区间 %s ~ %s\n", len(result.ValidationStore.Stats), result.ValidationStore.StartDate, result.ValidationStore.EndDate)
			}

			if format == "table" {
				signal.PrintPositionDecision(result.PositionDecision)
			}
			if result.PortfolioSummary != nil {
				portfolio.PrintSummary(result.PortfolioSummary)
				portfolio.SaveReport(result.PortfolioSummary, cfg.Data.RawDir+"/reports")
			}

			if len(result.Signals) == 0 && len(result.Watchlist) == 0 {
				fmt.Println("今日无交易信号")
				return nil
			}
			if result.ForwardErr != nil && format == "table" {
				fmt.Printf("前向测试记录失败: %v\n", result.ForwardErr)
			}

			if err := reporter.PrintWithWatch(result.Signals, result.Watchlist); err != nil {
				return err
			}
			if format == "table" {
				signal.PrintHistoricalEvidence(result.Signals, result.Watchlist)
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&strategyNames, "strategy", "s", nil, "策略名称 (多个用逗号分隔)")
	cmd.Flags().StringVarP(&format, "format", "f", "table", "输出格式: table, csv, json")
	cmd.Flags().IntVarP(&topN, "top", "n", 0, "显示前 N 条信号")
	cmd.Flags().IntVar(&watchN, "watch", 15, "显示观察机会数量（0=不显示）")
	cmd.Flags().BoolVar(&realtimeOn, "realtime", true, "仅在A股连续竞价时段使用实时行情")
	cmd.Flags().BoolVar(&marketRealtimeOn, "market-realtime", true, "盘中加载全市场实时行情并复用至候选和持仓")
	cmd.Flags().DurationVar(&marketWindow, "market-window", time.Minute, "盘中全市场行情刷新窗口，例如1m")
	cmd.Flags().StringVar(&realtimeSource, "realtime-source", realtime.SourceAuto, "实时行情来源: auto、eastmoney、sina")

	return cmd
}

func realtimeFetchSource(stats realtime.FetchStats) string {
	name := stats.Source
	switch name {
	case realtime.SourceEastmoney:
		name = "东方财富"
	case realtime.SourceSina:
		name = "新浪"
	case "":
		name = "实时"
	}
	if stats.FallbackFrom == "" {
		return name
	}
	from := stats.FallbackFrom
	if from == realtime.SourceEastmoney {
		from = "东方财富"
	} else if from == realtime.SourceSina {
		from = "新浪"
	}
	return name + "（由" + from + "降级）"
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
		Short: "用本地行情回填前向测试毛收益、交易成本和净收益",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			liquidityPolicy := cfg.Liquidity.Policy()
			if err := liquidityPolicy.Validate(); err != nil {
				return fmt.Errorf("流动性配置无效: %w", err)
			}
			bars, err := data.ReadParquetDir(cfg.Data.RawDir + "/daily")
			if err != nil {
				return fmt.Errorf("加载数据失败: %w", err)
			}
			bars = applyStkLimitStore(data.NewFetcher(nil, cfg.Data.RawDir, nil), bars)
			if q := data.CheckPriceDataQuality(bars); !q.HasCompleteRawPrices() && !allowAdjusted {
				return fmt.Errorf("%s；前向验证需要真实价字段，请重新拉取行情数据，或临时使用 --allow-adjusted-trades", q.Summary())
			}
			codeMap := data.GroupByCode(bars)
			portfolioCfg := cfg.Portfolio.Normalized(cfg.Backtest.InitialCapital)
			updated, err := forward.ValidateWithExecution(dir, codeMap, execution.CostModel{
				Commission: cfg.Backtest.Commission,
				Slippage:   cfg.Backtest.Slippage,
			}, liquidityPolicy, portfolioCfg.ReferenceEquity)
			if err != nil {
				return err
			}
			fmt.Printf("前向测试验证完成: 更新 %d 项观察期或退出状态\n", updated)
			fmt.Printf("成本口径: 手续费 %.4f%%/边，滑点 %.4f%%/边，买卖双边计入净收益\n",
				cfg.Backtest.Commission*100, cfg.Backtest.Slippage*100)
			if liquidityPolicy.Enabled {
				fmt.Printf("流动性口径: 订单占日均成交额不超过 %.2f%%，冲击系数 %.4f，单边冲击上限 %.2f%%\n",
					liquidityPolicy.MaxParticipationPct, liquidityPolicy.ImpactCoefficient, liquidityPolicy.MaxImpactRate*100)
			}
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

func validationCmd() *cobra.Command {
	var (
		strategyNames []string
		startDate     string
		endDate       string
		outputPath    string
		workers       int
		allowAdjusted bool
	)
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "构建和查看历史样本外验证证据",
	}
	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "回放全历史信号并生成推荐资格、胜率和权重证据",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			liquidityPolicy := cfg.Liquidity.Policy()
			if err := liquidityPolicy.Validate(); err != nil {
				return fmt.Errorf("流动性配置无效: %w", err)
			}
			names := strategyNames
			if len(names) == 0 {
				names = strategy.DailyStrategyNames(cfg.Signal.DefaultStrategies)
			}
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
			ds, err := dataset.Load(dataset.LoadOptions{RawDir: cfg.Data.RawDir, LoadFundamentals: loadFundamentals})
			if err != nil {
				return err
			}
			if q := data.CheckPriceDataQuality(ds.Bars); !q.HasCompleteRawPrices() && !allowAdjusted {
				return fmt.Errorf("%s；历史验证需要真实成交价，请重新拉取行情数据，或临时使用 --allow-adjusted-trades", q.Summary())
			}
			path := outputPath
			if path == "" {
				path = validation.DefaultPath(cfg.Data.RawDir, cfg.Validation.Path)
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
				Workers:         workers,
				StartDate:       startDate,
				EndDate:         endDate,
			})
			if err != nil {
				return err
			}
			if err := store.Save(path); err != nil {
				return err
			}
			fmt.Printf("历史验证完成: %s\n", path)
			fmt.Printf("回放区间: %s ~ %s，样本外折数: %d\n", store.StartDate, store.EndDate, len(store.Folds))
			fmt.Printf("信号快照: %d，独立可成交样本: %d，不可成交: %d，重叠过滤: %d，embargo: %d，跨折清除: %d，统计桶: %d\n",
				store.ScannedSignals, store.FeasibleTrades, store.SkippedTrades, store.OverlappingSignals,
				store.EmbargoedSignals, store.PurgedSignals, len(store.Stats))
			fmt.Printf("退出原因: %s；延迟退出: %d 笔/%d 日（最长 %d 日）；尾部亏损: %d；最差净收益: %.2f%%\n",
				formatCountMap(store.ExitReasonCounts), store.DelayedExitTrades, store.ExitDelayDays,
				store.MaxExitDelayDays, store.TailLossTrades, store.WorstNetReturnPct)
			fmt.Printf("流动性过滤: %d；受冲击成本影响: %d 笔；平均进/出冲击: %.3f%% / %.3f%%；最大冲击/成交占比: %.3f%% / %.2f%%\n",
				store.LiquidityFiltered, store.ImpactedTrades, store.AverageEntryImpactPct, store.AverageExitImpactPct,
				store.MaxImpactPct, store.MaxParticipationPct)
			return nil
		},
	}
	buildCmd.Flags().StringSliceVarP(&strategyNames, "strategy", "s", nil, "回放策略（默认使用 signal.default_strategies）")
	buildCmd.Flags().StringVar(&startDate, "start", "", "回放起始日期 YYYYMMDD")
	buildCmd.Flags().StringVar(&endDate, "end", "", "回放结束日期 YYYYMMDD")
	buildCmd.Flags().StringVar(&outputPath, "output", "", "验证结果路径（默认 data.raw_dir/validation/evidence.json）")
	buildCmd.Flags().IntVar(&workers, "workers", 0, "回放并行工作数（默认 GOMAXPROCS）")
	buildCmd.Flags().BoolVar(&allowAdjusted, "allow-adjusted-trades", false, "允许用复权价近似历史成交价（仅用于旧数据临时验证）")
	cmd.AddCommand(buildCmd)
	return cmd
}

func valueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "value",
		Short: "慢频价值投资筛选与季度复核",
		Long:  "价值模块独立于日常交易信号：月度使用 PE_TTM/PB 与财务质量筛选，季度复核基本面和估值回归。",
	}
	var (
		monthlyDate   string
		monthlyTopN   int
		quarterlyDate string
		quarterlyTopN int
	)
	monthlyCmd := &cobra.Command{
		Use:   "monthly",
		Short: "生成并保存月度价值候选池",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			report, err := value.Monthly(value.MonthlyOptions{
				RawDir:       cfg.Data.RawDir,
				Date:         monthlyDate,
				TopN:         monthlyTopN,
				MinMarketCap: cfg.Fetch.MinMarketCap,
			})
			if err != nil {
				return err
			}
			report.Print()
			return nil
		},
	}
	monthlyCmd.Flags().StringVar(&monthlyDate, "date", "", "估值快照日期 YYYYMMDD，默认最新日线交易日")
	monthlyCmd.Flags().IntVarP(&monthlyTopN, "top", "n", 20, "保存和显示前 N 个价值候选，0=全部")

	quarterlyCmd := &cobra.Command{
		Use:   "quarterly",
		Short: "复核最近月度价值候选池并保存结果",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig()
			report, err := value.Quarterly(value.QuarterlyOptions{
				RawDir: cfg.Data.RawDir,
				Date:   quarterlyDate,
				TopN:   quarterlyTopN,
			})
			if err != nil {
				return err
			}
			report.Print()
			return nil
		},
	}
	quarterlyCmd.Flags().StringVar(&quarterlyDate, "date", "", "复核日期 YYYYMMDD，默认最新日线交易日")
	quarterlyCmd.Flags().IntVarP(&quarterlyTopN, "top", "n", 0, "保存和显示前 N 个复核结果，0=全部")

	cmd.AddCommand(monthlyCmd)
	cmd.AddCommand(quarterlyCmd)
	return cmd
}

func registeredStrategies(names []string) ([]strategy.Strategy, error) {
	registry := strategy.DefaultRegistry()
	selected := make([]strategy.Strategy, 0, len(names))
	for _, name := range names {
		s, ok := registry.Get(name)
		if !ok {
			return nil, fmt.Errorf("未知策略: %s", name)
		}
		selected = append(selected, s)
	}
	return selected, nil
}

func resolveBacktestStrategyNames(requested, configured []string) []string {
	if len(requested) > 0 {
		return append([]string(nil), requested...)
	}
	// Match the default end-of-day workflow. Slow/value and experimental
	// strategies remain available when explicitly requested, but an old local
	// config must not silently put them back into the default trading baseline.
	return strategy.DailyStrategyNames(configured)
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

func applyStkLimitStore(fetcher *data.Fetcher, bars []data.DailyBar) []data.DailyBar {
	store, err := fetcher.LoadStkLimitStore()
	if err != nil {
		fmt.Printf("警告: 加载涨跌停价格失败: %v\n", err)
		return bars
	}
	return data.ApplyStkLimits(bars, store)
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

func resolveSectorBuildDates(bars []data.DailyBar, today bool, date, startDate, endDate string) []string {
	dates := data.TradingDatesFromBars(bars)
	if len(dates) == 0 {
		return nil
	}
	if date != "" {
		return []string{date}
	}
	if today || (startDate == "" && endDate == "") {
		return []string{dates[len(dates)-1]}
	}
	var out []string
	for _, d := range dates {
		if startDate != "" && d < startDate {
			continue
		}
		if endDate != "" && d > endDate {
			continue
		}
		out = append(out, d)
	}
	return out
}

func sortedCodes(codeMap map[string][]data.DailyBar) []string {
	codes := make([]string, 0, len(codeMap))
	for code := range codeMap {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
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
	fmt.Printf("\n========== 单标的样本汇总（非组合回测） ==========\n")
	fmt.Printf("样本数:       %d 只\n", len(metricsList))
	fmt.Printf("平均收益:     %11.2f%%\n", sumReturn/n)
	fmt.Printf("中位收益:     %11.2f%%\n", median)
	fmt.Printf("平均年化:     %11.2f%%\n", sumAnnual/n)
	fmt.Printf("平均最大回撤: %11.2f%%\n", sumDD/n)
	fmt.Printf("平均夏普:     %11.2f\n", sumSharpe/n)
	fmt.Printf("正收益股票:   %d / %d\n", winStocks, len(metricsList))
	fmt.Printf("总交易次数:   %d\n", totalTrades)
	fmt.Printf("收益区间:     %11.2f%% ~ %.2f%%\n", returns[0], returns[len(returns)-1])
	fmt.Println("说明: 每只股票独立满仓模拟；均值不代表按 signal 选股后的组合收益")
	fmt.Println("==============================")
}
