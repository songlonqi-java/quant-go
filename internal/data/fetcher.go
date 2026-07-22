package data

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Fetcher struct {
	client    *Client
	rawDir    string
	prefixes  []string
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

	stocks, allowedCodes, err := f.FilterStocksByMarketCap(ctx, stocks, minMarketCap)
	if err != nil {
		return fmt.Errorf("市值过滤失败: %w", err)
	}
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

	todayFile := filepath.Join(f.rawDir, "daily", fmt.Sprintf("today_%s.parquet", today))

	if !force {
		if _, err := os.Stat(todayFile); err == nil {
			fmt.Printf(">>> 今日数据文件已存在: %s (使用 --force 强制覆盖)\n", todayFile)
			return ReadParquetFile(todayFile)
		}
	}

	bars, err := f.client.FetchAllDailyByDate(ctx, today)
	if err != nil {
		return nil, fmt.Errorf("拉取今日行情失败: %w", err)
	}
	if len(bars) == 0 {
		fmt.Println(">>> 今日数据尚未发布（Tushare 日线通常在15:30后更新），将使用本地已有数据")
		return nil, nil
	}

	factors, err := f.client.FetchAdjFactorsByDate(ctx, today)
	if err == nil && len(factors) > 0 {
		bars = ApplyAdjFactors(bars, factors)
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

	if err := WriteParquetFile(todayFile, bars); err != nil {
		fmt.Printf("  警告: 保存今日数据失败: %v\n", err)
	} else {
		fmt.Printf(">>> 今日数据已保存: %s (%d 条)\n", todayFile, len(bars))
	}

	return bars, nil
}

func (f *Fetcher) FetchDate(ctx context.Context, date string, force bool) ([]DailyBar, error) {
	fmt.Printf(">>> 拉取 %s 数据...\n", date)

	dateFile := filepath.Join(f.rawDir, "daily", fmt.Sprintf("today_%s.parquet", date))
	if !force {
		if _, err := os.Stat(dateFile); err == nil {
			fmt.Printf(">>> 数据文件已存在: %s (使用 --force 强制覆盖)\n", dateFile)
			return ReadParquetFile(dateFile)
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

	if err := WriteParquetFile(dateFile, bars); err != nil {
		return nil, err
	}
	fmt.Printf(">>> %s 数据已保存 (%d 条)\n", date, len(bars))
	return bars, nil
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
	stocks, allowedCodes, err := f.FilterStocksByMarketCap(ctx, stocks, minMarketCap)
	if err != nil {
		return err
	}
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

func (f *Fetcher) FetchFinancials(ctx context.Context, startDate, endDate string, minMarketCap float64) error {
	allStocks, err := f.client.FetchStockList(ctx)
	if err != nil {
		return err
	}
	stocks := FilterStocksByPrefix(allStocks, f.prefixes)
	fmt.Printf(">>> 前缀过滤后 %d 只\n", len(stocks))

	stocks, allowedCodes, err := f.FilterStocksByMarketCap(ctx, stocks, minMarketCap)
	if err != nil {
		return err
	}
	codeSet := make(map[string]bool, len(allowedCodes))
	for _, c := range allowedCodes {
		codeSet[c] = true
	}

	codes := allowedCodes
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

func (f *Fetcher) LoadDailyBasicStore() (*FundamentalStore, error) {
	store := NewFundamentalStore()
	dir := filepath.Join(f.rawDir, "daily_basic")
	files, err := filepath.Glob(filepath.Join(dir, "*.parquet"))
	if err != nil || len(files) == 0 {
		return store, nil
	}
	fmt.Printf(">>> 加载基本面数据... (%d 文件)\n", len(files))
	for _, file := range files {
		basics, err := readDailyBasicParquet(file)
		if err != nil {
			fmt.Printf("  警告: 读取 %s 失败: %v\n", file, err)
			continue
		}
		store.LoadDailyBasics(basics)
	}
	return store, nil
}

func (f *Fetcher) LoadFinaStore() (*FundamentalStore, error) {
	store := NewFundamentalStore()

	finaFile := filepath.Join(f.rawDir, "fina", "fina_indicator.parquet")
	if _, err := os.Stat(finaFile); err == nil {
		indicators, err := readFinaParquet(finaFile)
		if err != nil {
			fmt.Printf("  警告: 读取财务指标失败: %v\n", err)
		} else {
			store.LoadFinaIndicators(indicators)
			fmt.Printf(">>> 财务指标已加载: %d 条\n", len(indicators))
		}
	}

	hsFile := filepath.Join(f.rawDir, "index", "hs300.parquet")
	if _, err := os.Stat(hsFile); err == nil {
		consts, err := readHsConstParquet(hsFile)
		if err != nil {
			fmt.Printf("  警告: 读取沪深300失败: %v\n", err)
		} else {
			store.LoadHsConst(consts)
			fmt.Printf(">>> 沪深300成分股已加载: %d 只\n", len(consts))
		}
	}

	return store, nil
}

func writeDailyBasicParquet(path string, basics []DailyBasic) error {
	return writeGenericParquet(path, basics)
}

func writeFinaParquet(path string, data []FinaIndicator) error {
	return writeGenericParquet(path, data)
}

func writeIncomeParquet(path string, data []Income) error {
	return writeGenericParquet(path, data)
}

func writeHsConstParquet(path string, data []HsConst) error {
	return writeGenericParquet(path, data)
}

func readDailyBasicParquet(filePath string) ([]DailyBasic, error) {
	return readGenericParquet[DailyBasic](filePath)
}

func readFinaParquet(filePath string) ([]FinaIndicator, error) {
	return readGenericParquet[FinaIndicator](filePath)
}

func readHsConstParquet(filePath string) ([]HsConst, error) {
	return readGenericParquet[HsConst](filePath)
}
