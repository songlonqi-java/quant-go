package data

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Fetcher struct {
	client   *Client
	rawDir   string
	prefixes []string
}

func NewFetcher(client *Client, rawDir string, prefixes []string) *Fetcher {
	return &Fetcher{
		client:   client,
		rawDir:   rawDir,
		prefixes: prefixes,
	}
}

// FilterStocksByMarketCap 根据最新日期的 daily_basic 数据过滤市值不达标的股票
// minCap 单位：亿元（如 100 表示 100 亿）
func (f *Fetcher) FilterStocksByMarketCap(ctx context.Context, stocks []StockInfo, minCap float64) ([]StockInfo, []string, error) {
	if minCap <= 0 || len(stocks) == 0 {
		codes := make([]string, len(stocks))
		for i, s := range stocks {
			codes[i] = s.TsCode
		}
		return stocks, codes, nil
	}

	fmt.Printf(">>> 按市值过滤 (>=%.0f亿)...\n", minCap)
	today := time.Now().Format("20060102")
	yesterday := time.Now().AddDate(0, 0, -1).Format("20060102")

	var basics []DailyBasic
	var err error
	for _, tryDate := range []string{today, yesterday} {
		basics, err = f.client.FetchDailyBasicByDate(ctx, tryDate)
		if err == nil && len(basics) > 0 {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}

	if len(basics) == 0 {
		fmt.Println("  警告: 无法获取市值数据，跳过市值过滤")
		codes := make([]string, len(stocks))
		for i, s := range stocks {
			codes[i] = s.TsCode
		}
		return stocks, codes, nil
	}

	mvMap := make(map[string]float64)
	for _, b := range basics {
		if b.TotalMv > 0 {
			mvMap[b.TsCode] = b.TotalMv
		}
	}

	minMvWanYuan := minCap * 10000 // 亿元 → 万元 (1亿=10000万)

	var filtered []StockInfo
	var filteredCodes []string
	excluded := 0
	for _, s := range stocks {
		mv, ok := mvMap[s.TsCode]
		if ok && mv >= minMvWanYuan {
			filtered = append(filtered, s)
			filteredCodes = append(filteredCodes, s.TsCode)
		} else {
			excluded++
		}
	}

	fmt.Printf(">>> 市值过滤: %d 只保留, %d 只排除 (阈值 %.0f亿)\n", len(filtered), excluded, minCap)
	return filtered, filteredCodes, nil
}

func (f *Fetcher) FetchHistorical(ctx context.Context, startYear, endYear int, minMarketCap float64) error {
	fmt.Println(">>> 获取股票列表...")
	allStocks, err := f.client.FetchStockList(ctx)
	if err != nil {
		return fmt.Errorf("获取股票列表失败: %w", err)
	}
	stocks := FilterStocksByPrefix(allStocks, f.prefixes)
	fmt.Printf(">>> 前缀过滤后共 %d 只股票\n", len(stocks))
	if minMarketCap > 0 {
		fmt.Printf(">>> 历史行情归档不应用当前市值过滤（配置 %.0f 亿仅用于当前候选池）\n", minMarketCap)
	}
	allowedCodes := stockCodes(stocks)
	codeSet := make(map[string]bool, len(allowedCodes))
	for _, c := range allowedCodes {
		codeSet[c] = true
	}

	stockPath := filepath.Join(f.rawDir, "stocks.parquet")
	if err := WriteStocksParquet(stockPath, stocks); err != nil {
		return fmt.Errorf("保存股票列表失败: %w", err)
	}

	fmt.Println(">>> 获取交易日历...")
	startDate := fmt.Sprintf("%d0101", startYear)
	endDate := fmt.Sprintf("%d1231", endYear)
	cal, err := f.client.FetchTradeCal(ctx, "SSE", startDate, endDate)
	if err != nil {
		return fmt.Errorf("获取交易日历失败: %w", err)
	}
	tradeDays := TradingDays(cal)
	fmt.Printf(">>> 共 %d 个交易日\n", len(tradeDays))
	if err := writeGenericParquet(filepath.Join(f.rawDir, "trade_cal.parquet"), cal); err != nil {
		fmt.Printf("  警告: 保存交易日历失败: %v\n", err)
	}

	for year := startYear; year <= endYear; year++ {
		fmt.Printf("\n>>> 拉取 %d 年数据 (%d只股票)...\n", year, len(stocks))
		var allBars []DailyBar
		yearStart := fmt.Sprintf("%d0101", year)
		yearEnd := fmt.Sprintf("%d1231", year)

		processed := 0
		for _, day := range tradeDays {
			if day < yearStart || day > yearEnd {
				continue
			}

			bars, err := f.client.FetchAllDailyByDate(ctx, day)
			if err != nil {
				fmt.Printf("  警告: %s 拉取失败: %v\n", day, err)
				continue
			}
			if len(bars) == 0 {
				continue
			}

			factors, err := f.client.FetchAdjFactorsByDate(ctx, day)
			if err == nil && len(factors) > 0 {
				bars = ApplyAdjFactors(bars, factors)
			} else {
				fmt.Printf("  ⚠ %s 复权因子缺失(数据未复权!)\n", day)
			}

			if len(codeSet) > 0 {
				var filtered []DailyBar
				for _, b := range bars {
					if codeSet[b.TsCode] {
						filtered = append(filtered, b)
					}
				}
				bars = filtered
			}

			allBars = append(allBars, bars...)
			processed++

			if processed%20 == 0 {
				fmt.Printf("  进度: %d 交易日, 已收集 %d 行, API %d次\n",
					processed, len(allBars), f.client.CallCount())
			}
		}

		if len(allBars) == 0 {
			fmt.Printf(">>> %d 年无数据\n", year)
			continue
		}

		yearFile := filepath.Join(f.rawDir, "daily", fmt.Sprintf("%d.parquet", year))
		if err := WriteParquetFile(yearFile, allBars); err != nil {
			return fmt.Errorf("保存 %d 年数据失败: %w", year, err)
		}
		fmt.Printf(">>> %d 年完成: %d 行, %d 交易日 → %s\n", year, len(allBars), processed, yearFile)
	}

	f.client.LogStatus()
	return nil
}

func (f *Fetcher) FetchToday(ctx context.Context, force bool) ([]DailyBar, error) {
	today := time.Now().Format("20060102")
	fmt.Printf(">>> 拉取今日数据 (%s)...\n", today)

	yearFile := filepath.Join(f.rawDir, "daily", fmt.Sprintf("%s.parquet", today[:4]))

	if !force {
		if existing, err := ReadParquetFile(yearFile); err == nil {
			if bars := FilterBarsByDate(existing, today); len(bars) > 0 {
				fmt.Printf(">>> 今日数据已在年度文件中: %s (%d 条, 使用 --force 强制覆盖)\n", yearFile, len(bars))
				return bars, nil
			}
		}
	}

	bars, err := f.client.FetchAllDailyByDate(ctx, today)
	if err != nil {
		return nil, fmt.Errorf("拉取今日行情失败: %w", err)
	}
	if len(bars) == 0 {
		fmt.Println(">>> 今日数据尚未发布（Tushare 日线通常在16:00后更新），将使用本地已有数据")
		return nil, nil
	}

	factors, err := f.client.FetchAdjFactorsByDate(ctx, today)
	if err == nil && len(factors) > 0 {
		bars = ApplyAdjFactors(bars, factors)
	} else {
		fmt.Printf("  ⚠ 复权因子缺失(数据未复权!)\n")
	}

	var filtered []DailyBar
	for _, b := range bars {
		for _, p := range f.prefixes {
			if strings.HasPrefix(b.TsCode, p) {
				filtered = append(filtered, b)
				break
			}
		}
	}
	if len(f.prefixes) > 0 {
		bars = filtered
	}

	if err := WriteMergedParquetFile(yearFile, bars); err != nil {
		fmt.Printf("  警告: 保存今日数据失败: %v\n", err)
	} else {
		fmt.Printf(">>> 今日数据已合并保存: %s (%d 条)\n", yearFile, len(bars))
	}

	return bars, nil
}

func (f *Fetcher) FetchDate(ctx context.Context, date string, force bool) ([]DailyBar, error) {
	fmt.Printf(">>> 拉取 %s 数据...\n", date)

	yearFile := filepath.Join(f.rawDir, "daily", fmt.Sprintf("%s.parquet", date[:4]))
	if !force {
		if existing, err := ReadParquetFile(yearFile); err == nil {
			if bars := FilterBarsByDate(existing, date); len(bars) > 0 {
				fmt.Printf(">>> %s 数据已在年度文件中: %s (%d 条, 使用 --force 强制覆盖)\n", date, yearFile, len(bars))
				return bars, nil
			}
		}
	}

	bars, err := f.client.FetchAllDailyByDate(ctx, date)
	if err != nil {
		return nil, fmt.Errorf("拉取 %s 行情失败: %w", date, err)
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("%s 无行情数据（非交易日或数据未发布）", date)
	}

	factors, err := f.client.FetchAdjFactorsByDate(ctx, date)
	if err == nil && len(factors) > 0 {
		bars = ApplyAdjFactors(bars, factors)
	} else {
		fmt.Printf("  ⚠ 复权因子缺失(数据未复权!)\n")
	}

	var filtered []DailyBar
	for _, b := range bars {
		for _, p := range f.prefixes {
			if strings.HasPrefix(b.TsCode, p) {
				filtered = append(filtered, b)
				break
			}
		}
	}
	if len(f.prefixes) > 0 {
		bars = filtered
	}

	if err := WriteMergedParquetFile(yearFile, bars); err != nil {
		return nil, err
	}
	fmt.Printf(">>> %s 数据已合并保存: %s (%d 条)\n", date, yearFile, len(bars))
	return bars, nil
}

// FetchDailyBasicForDate pulls one end-of-day valuation snapshot and merges it
// into the matching yearly Parquet file. It intentionally keeps loss-making
// companies (whose PE is non-positive); sector aggregation decides which values
// have a meaningful average.
func (f *Fetcher) FetchDailyBasicForDate(ctx context.Context, tradeDate string) ([]DailyBasic, error) {
	if f.client == nil {
		return nil, fmt.Errorf("Tushare 客户端未初始化")
	}
	if len(tradeDate) != 8 {
		return nil, fmt.Errorf("交易日期格式错误: %q", tradeDate)
	}
	basics, err := f.client.FetchDailyBasicByDate(ctx, tradeDate)
	if err != nil {
		return nil, err
	}
	if len(basics) == 0 {
		return nil, fmt.Errorf("%s 无日度估值数据", tradeDate)
	}
	if len(f.prefixes) > 0 {
		filtered := make([]DailyBasic, 0, len(basics))
		for _, basic := range basics {
			for _, prefix := range f.prefixes {
				if strings.HasPrefix(basic.TsCode, prefix) {
					filtered = append(filtered, basic)
					break
				}
			}
		}
		basics = filtered
	}
	if len(basics) == 0 {
		return nil, fmt.Errorf("%s 没有符合股票池前缀的日度估值数据", tradeDate)
	}
	path := filepath.Join(f.rawDir, "daily_basic", tradeDate[:4]+".parquet")
	if err := writeMergedDailyBasicParquet(path, basics); err != nil {
		return nil, fmt.Errorf("保存日度估值失败: %w", err)
	}
	fmt.Printf(">>> 日度估值已合并保存: %s (%d 条)\n", path, len(basics))
	return basics, nil
}

func (f *Fetcher) FetchDateRange(ctx context.Context, startDate, endDate string) error {
	fmt.Printf(">>> 拉取 %s ~ %s 数据...\n", startDate, endDate)

	allStocks, err := f.client.FetchStockList(ctx)
	if err != nil {
		return err
	}
	stocks := FilterStocksByPrefix(allStocks, f.prefixes)
	codes := make([]string, len(stocks))
	for i, s := range stocks {
		codes[i] = s.TsCode
	}

	var allBars []DailyBar
	for i, code := range codes {
		bars, err := f.client.FetchDaily(ctx, code, startDate, endDate)
		if err != nil {
			continue
		}
		if len(bars) == 0 {
			continue
		}
		adjFactors, err := f.client.FetchAdjFactors(ctx, code, startDate, endDate)
		if err == nil && len(adjFactors) > 0 {
			bars = ApplyAdjFactors(bars, adjFactors)
		}
		allBars = append(allBars, bars...)
		if (i+1)%100 == 0 {
			fmt.Printf("  进度: %d / %d\n", i+1, len(codes))
		}
	}

	if len(allBars) == 0 {
		return fmt.Errorf("指定日期范围无数据")
	}

	mkdirAll(f.rawDir + "/daily")
	outFile := filepath.Join(f.rawDir, "daily", fmt.Sprintf("%s_%s.parquet", startDate, endDate))
	if err := WriteParquetFile(outFile, allBars); err != nil {
		return err
	}
	fmt.Printf(">>> 完成，共 %d 行 → %s\n", len(allBars), outFile)
	f.client.LogStatus()
	return nil
}

func mkdirAll(path string) {
	os.MkdirAll(path, 0755)
}

func (f *Fetcher) FetchDailyBasicHistory(ctx context.Context, startYear, endYear int, minMarketCap float64) error {
	allStocks, err := f.client.FetchStockList(ctx)
	if err != nil {
		return err
	}
	stocks := FilterStocksByPrefix(allStocks, f.prefixes)
	if minMarketCap > 0 {
		fmt.Printf(">>> 历史估值归档不应用当前市值过滤（配置 %.0f 亿仅用于当前候选池）\n", minMarketCap)
	}
	allowedCodes := stockCodes(stocks)
	codeSet := make(map[string]bool, len(allowedCodes))
	for _, c := range allowedCodes {
		codeSet[c] = true
	}

	fmt.Println(">>> 获取交易日历...")
	startDate := fmt.Sprintf("%d0101", startYear)
	endDate := fmt.Sprintf("%d1231", endYear)
	cal, err := f.client.FetchTradeCal(ctx, "SSE", startDate, endDate)
	if err != nil {
		return fmt.Errorf("获取交易日历失败: %w", err)
	}
	tradeDays := TradingDays(cal)
	fmt.Printf(">>> 共 %d 个交易日\n", len(tradeDays))

	for year := startYear; year <= endYear; year++ {
		fmt.Printf("\n>>> 拉取 %d 年基本面数据 (PE/PB/市值/股息率, %d只股票)...\n", year, len(stocks))
		yearStart := fmt.Sprintf("%d0101", year)
		yearEnd := fmt.Sprintf("%d1231", year)

		var allBasics []DailyBasic
		processed := 0

		for _, day := range tradeDays {
			if day < yearStart || day > yearEnd {
				continue
			}

			basics, err := f.client.FetchDailyBasicByDate(ctx, day)
			if err != nil {
				fmt.Printf("  警告: %s 拉取失败: %v\n", day, err)
				continue
			}

			if len(codeSet) > 0 {
				var filtered []DailyBasic
				for _, b := range basics {
					if codeSet[b.TsCode] {
						filtered = append(filtered, b)
					}
				}
				basics = filtered
			}

			allBasics = append(allBasics, basics...)
			processed++

			if processed%20 == 0 && len(basics) > 0 {
				fmt.Printf("  进度: %d 交易日, 已收集 %d 行, API %d次\n",
					processed, len(allBasics), f.client.CallCount())
			}
		}

		if len(allBasics) == 0 {
			fmt.Printf(">>> %d 年无基本面数据\n", year)
			continue
		}

		outFile := filepath.Join(f.rawDir, "daily_basic", fmt.Sprintf("%d.parquet", year))
		if err := writeDailyBasicParquet(outFile, allBasics); err != nil {
			return err
		}
		fmt.Printf(">>> %d 年基本面完成: %d 行, %d 交易日 → %s\n",
			year, len(allBasics), processed, outFile)
	}

	f.client.LogStatus()
	return nil
}

func (f *Fetcher) FetchStkLimitHistory(ctx context.Context, startYear, endYear int) error {
	startDate := fmt.Sprintf("%d0101", startYear)
	endDate := fmt.Sprintf("%d1231", endYear)
	return f.FetchStkLimitRange(ctx, startDate, endDate)
}

func (f *Fetcher) FetchStkLimitRange(ctx context.Context, startDate, endDate string) error {
	fmt.Printf(">>> 拉取涨跌停价格 %s ~ %s...\n", startDate, endDate)
	tradeDays, err := f.tradeDaysForRange(ctx, startDate, endDate)
	if err != nil {
		return err
	}
	codeSet := f.loadUniverseCodeSet()
	byYear := make(map[string][]StkLimit)
	processed := 0

	for _, day := range tradeDays {
		limits, err := f.client.FetchStkLimitByDate(ctx, day)
		if err != nil {
			fmt.Printf("  警告: %s 涨跌停价格拉取失败: %v\n", day, err)
			continue
		}
		limits = filterStkLimits(limits, codeSet)
		if len(limits) > 0 {
			byYear[day[:4]] = append(byYear[day[:4]], limits...)
		}
		processed++
		if processed%20 == 0 {
			fmt.Printf("  进度: %d 交易日, 已收集 %d 行, API %d次\n",
				processed, countStkLimits(byYear), f.client.CallCount())
		}
	}

	for year, limits := range byYear {
		outFile := filepath.Join(f.rawDir, "stk_limit", year+".parquet")
		if err := writeMergedGenericParquet(outFile, limits, func(l StkLimit) string {
			return l.TradeDate + "|" + l.TsCode
		}); err != nil {
			return err
		}
		fmt.Printf(">>> %s 年涨跌停价格完成: %d 行 → %s\n", year, len(limits), outFile)
	}
	f.client.LogStatus()
	return nil
}

func (f *Fetcher) FetchMoneyflowHistory(ctx context.Context, startYear, endYear int) error {
	startDate := fmt.Sprintf("%d0101", startYear)
	endDate := fmt.Sprintf("%d1231", endYear)
	return f.FetchMoneyflowRange(ctx, startDate, endDate)
}

func (f *Fetcher) FetchMoneyflowRange(ctx context.Context, startDate, endDate string) error {
	fmt.Printf(">>> 拉取个股资金流向 %s ~ %s...\n", startDate, endDate)
	tradeDays, err := f.tradeDaysForRange(ctx, startDate, endDate)
	if err != nil {
		return err
	}
	codeSet := f.loadUniverseCodeSet()
	byYear := make(map[string][]Moneyflow)
	processed := 0

	for _, day := range tradeDays {
		flows, err := f.client.FetchMoneyflowByDate(ctx, day)
		if err != nil {
			fmt.Printf("  警告: %s 资金流向拉取失败: %v\n", day, err)
			continue
		}
		flows = filterMoneyflows(flows, codeSet)
		if len(flows) > 0 {
			byYear[day[:4]] = append(byYear[day[:4]], flows...)
		}
		processed++
		if processed%20 == 0 {
			fmt.Printf("  进度: %d 交易日, 已收集 %d 行, API %d次\n",
				processed, countMoneyflows(byYear), f.client.CallCount())
		}
	}

	for year, flows := range byYear {
		outFile := filepath.Join(f.rawDir, "moneyflow", year+".parquet")
		if err := writeMergedGenericParquet(outFile, flows, func(m Moneyflow) string {
			return m.TradeDate + "|" + m.TsCode
		}); err != nil {
			return err
		}
		fmt.Printf(">>> %s 年资金流向完成: %d 行 → %s\n", year, len(flows), outFile)
	}
	f.client.LogStatus()
	return nil
}

func (f *Fetcher) FetchFinancials(ctx context.Context, startDate, endDate string, minMarketCap float64) error {
	allStocks, err := f.client.FetchStockList(ctx)
	if err != nil {
		return err
	}
	stocks := FilterStocksByPrefix(allStocks, f.prefixes)
	fmt.Printf(">>> 前缀过滤后 %d 只\n", len(stocks))
	if minMarketCap > 0 {
		fmt.Printf(">>> 历史财务归档不应用当前市值过滤（配置 %.0f 亿仅用于当前候选池）\n", minMarketCap)
	}
	codes := stockCodes(stocks)
	fmt.Printf(">>> 财务数据拉取 %d 只股票\n", len(codes))

	fmt.Printf(">>> 拉取财务指标 (ROE/ROA/利润率)...\n")
	var allFina []FinaIndicator
	for i, code := range codes {
		fina, err := f.client.FetchFinaIndicator(ctx, code, startDate, endDate)
		if err != nil {
			continue
		}
		allFina = append(allFina, fina...)
		if (i+1)%200 == 0 {
			fmt.Printf("  财务指标进度: %d / %d\n", i+1, len(codes))
		}
	}
	finaFile := filepath.Join(f.rawDir, "fina", "fina_indicator.parquet")
	if err := writeFinaParquet(finaFile, allFina); err != nil {
		return err
	}
	fmt.Printf(">>> 财务指标完成: %d 条 → %s\n", len(allFina), finaFile)

	fmt.Printf(">>> 拉取利润表 (营收/净利润)...\n")
	var allIncome []Income
	for i, code := range codes {
		income, err := f.client.FetchIncome(ctx, code, startDate, endDate)
		if err != nil {
			continue
		}
		allIncome = append(allIncome, income...)
		if (i+1)%200 == 0 {
			fmt.Printf("  利润表进度: %d / %d\n", i+1, len(codes))
		}
	}
	incomeFile := filepath.Join(f.rawDir, "fina", "income.parquet")
	if err := writeIncomeParquet(incomeFile, allIncome); err != nil {
		return err
	}
	fmt.Printf(">>> 利润表完成: %d 条 → %s\n", len(allIncome), incomeFile)

	f.client.LogStatus()
	return nil
}

func stockCodes(stocks []StockInfo) []string {
	codes := make([]string, 0, len(stocks))
	for _, stock := range stocks {
		if stock.TsCode != "" {
			codes = append(codes, stock.TsCode)
		}
	}
	sort.Strings(codes)
	return codes
}

func (f *Fetcher) FetchHs300(ctx context.Context) ([]HsConst, error) {
	fmt.Println(">>> 拉取沪深300成分股...")
	sh, err := f.client.FetchHsConst(ctx, "SH")
	if err != nil {
		return nil, fmt.Errorf("拉取沪深300(沪市)失败: %w", err)
	}
	sz, err := f.client.FetchHsConst(ctx, "SZ")
	if err != nil {
		return nil, fmt.Errorf("拉取沪深300(深市)失败: %w", err)
	}
	all := append(sh, sz...)
	fmt.Printf(">>> 沪深300成分股共 %d 只\n", len(all))

	hsFile := filepath.Join(f.rawDir, "index", "hs300.parquet")
	if err := writeHsConstParquet(hsFile, all); err != nil {
		return nil, err
	}
	fmt.Printf(">>> 已保存: %s\n", hsFile)
	f.client.LogStatus()
	return all, nil
}

func (f *Fetcher) FetchIndexData(ctx context.Context, startYear, endYear int) error {
	indices := []struct {
		code string
		name string
	}{
		{"000001.SH", "上证指数"},
		{"399001.SZ", "深证成指"},
		{"399006.SZ", "创业板指"},
	}
	startDate := fmt.Sprintf("%d0101", startYear)
	endDate := fmt.Sprintf("%d1231", endYear)

	for _, idx := range indices {
		fmt.Printf(">>> 拉取 %s (%s)...\n", idx.name, idx.code)
		bars, err := f.client.FetchIndexDaily(ctx, idx.code, startDate, endDate)
		if err != nil {
			return fmt.Errorf("拉取 %s 失败: %w", idx.name, err)
		}
		if len(bars) == 0 {
			fmt.Printf("  无数据\n")
			continue
		}
		outFile := filepath.Join(f.rawDir, "index", fmt.Sprintf("%s.parquet", idx.code))
		if err := writeGenericParquet(outFile, bars); err != nil {
			return err
		}
		fmt.Printf("  %s 完成: %d 条 → %s\n", idx.name, len(bars), outFile)
	}

	f.client.LogStatus()
	return nil
}

func (f *Fetcher) tradeDaysForRange(ctx context.Context, startDate, endDate string) ([]string, error) {
	tradeDays := LoadTradeDates(f.rawDir, nil)
	if len(tradeDays) == 0 {
		cal, err := f.client.FetchTradeCal(ctx, "SSE", startDate, endDate)
		if err != nil {
			return nil, fmt.Errorf("获取交易日历失败: %w", err)
		}
		tradeDays = TradingDays(cal)
		if err := writeGenericParquet(filepath.Join(f.rawDir, "trade_cal.parquet"), cal); err != nil {
			fmt.Printf("  警告: 保存交易日历失败: %v\n", err)
		}
	}

	var filtered []string
	for _, day := range tradeDays {
		if day >= startDate && day <= endDate {
			filtered = append(filtered, day)
		}
	}
	return filtered, nil
}

func (f *Fetcher) loadUniverseCodeSet() map[string]bool {
	names := LoadStockNames(filepath.Join(f.rawDir, "stocks.parquet"))
	if len(names) == 0 {
		return nil
	}
	codeSet := make(map[string]bool, len(names))
	for code := range names {
		codeSet[code] = true
	}
	return codeSet
}

func filterStkLimits(limits []StkLimit, codeSet map[string]bool) []StkLimit {
	if len(codeSet) == 0 {
		return limits
	}
	filtered := make([]StkLimit, 0, len(limits))
	for _, limit := range limits {
		if codeSet[limit.TsCode] {
			filtered = append(filtered, limit)
		}
	}
	return filtered
}

func filterMoneyflows(flows []Moneyflow, codeSet map[string]bool) []Moneyflow {
	if len(codeSet) == 0 {
		return flows
	}
	filtered := make([]Moneyflow, 0, len(flows))
	for _, flow := range flows {
		if codeSet[flow.TsCode] {
			filtered = append(filtered, flow)
		}
	}
	return filtered
}

func countStkLimits(byYear map[string][]StkLimit) int {
	total := 0
	for _, rows := range byYear {
		total += len(rows)
	}
	return total
}

func countMoneyflows(byYear map[string][]Moneyflow) int {
	total := 0
	for _, rows := range byYear {
		total += len(rows)
	}
	return total
}
