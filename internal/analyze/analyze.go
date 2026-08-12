package analyze

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"quant/internal/config"
	"quant/internal/data"
	"quant/internal/realtime"
	"quant/internal/sector"
	"quant/internal/strategy"

	"github.com/parquet-go/parquet-go"
)

type Report struct {
	Code      string
	Name      string
	Industry  string
	Bars      int
	DateRange string
	Latest    data.DailyBar
	RawPrice  float64
	AdjFactor float64

	MA5   float64
	MA10  float64
	MA20  float64
	MA60  float64
	MA120 float64
	MA250 float64

	Chg5d  float64
	Chg20d float64
	High60 float64
	Low60  float64

	PE        float64
	PETTM     float64
	PB        float64
	MarketCap float64
	DivYield  float64
	Turnover  float64

	IndustryPETTM      float64
	IndustryPETTMCount int
	IndustryPETTMAvg   float64
	IndustryPEAvg      float64

	BuySignals  []string
	SellSignals []string

	Realtime *realtime.Quote
}

// SetRealtime attaches a live quote pulled from the realtime provider. The
// quote is raw (non-adjusted) market price; all comparisons in Print convert
// adjusted references through the latest adj factor.
func (r *Report) SetRealtime(q realtime.Quote) {
	r.Realtime = &q
}

func Run(code, configPath string) (*Report, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	bars, err := data.ReadParquetDir(filepath.Join(cfg.Data.RawDir, "daily"))
	if err != nil {
		return nil, err
	}

	sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
	codeMap := data.GroupByCode(bars)
	for c, stockBars := range codeMap {
		sort.Slice(stockBars, func(i, j int) bool { return stockBars[i].TradeDate < stockBars[j].TradeDate })
		codeMap[c] = stockBars
	}

	stockBars := codeMap[code]
	if len(stockBars) == 0 {
		return nil, fmt.Errorf("未找到 %s 的数据", code)
	}

	last := len(stockBars) - 1
	fundStore := loadFundStore(cfg.Data.RawDir)

	r := &Report{
		Code:      code,
		Bars:      len(stockBars),
		DateRange: fmt.Sprintf("%s ~ %s", stockBars[0].TradeDate, stockBars[last].TradeDate),
		Latest:    stockBars[last],
	}
	if r.Latest.HasRawPrices() {
		r.RawPrice = r.Latest.RawClose
		r.AdjFactor = r.Latest.AdjFactor
	}

	r.loadNameAndIndustry(filepath.Join(cfg.Data.RawDir, "stocks.parquet"))
	r.loadMA(stockBars, last)
	r.loadPriceStats(stockBars, last)
	r.loadFundamentals(fundStore, code, stockBars[last].TradeDate)
	r.loadIndustryValuation(cfg.Data.RawDir, stockBars[last].TradeDate)
	r.loadStrategySignals(stockBars, last, codeMap, fundStore)

	return r, nil
}

func (r *Report) loadIndustryValuation(rawDir, tradeDate string) {
	if r.Industry == "" {
		return
	}
	report, err := sector.LoadReport(rawDir, tradeDate)
	if err != nil || report == nil {
		return
	}
	row, ok := report.Find(sector.TypeIndustry, r.Industry)
	if !ok {
		return
	}
	r.IndustryPETTM = row.PETTMAggregate
	r.IndustryPETTMCount = row.PETTMCount
	r.IndustryPETTMAvg = row.PETTMAvg
	r.IndustryPEAvg = row.PEAvg
}

func (r *Report) loadNameAndIndustry(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	reader := parquet.NewReader(f, parquet.SchemaOf(&data.StockInfo{}))
	defer reader.Close()
	for {
		var s data.StockInfo
		if err := reader.Read(&s); err != nil {
			break
		}
		if s.TsCode == r.Code {
			r.Name = s.Name
			r.Industry = s.Industry
			return
		}
	}
}

func (r *Report) loadMA(bars []data.DailyBar, idx int) {
	r.MA5 = sma(bars, idx, 5)
	r.MA10 = sma(bars, idx, 10)
	r.MA20 = sma(bars, idx, 20)
	r.MA60 = sma(bars, idx, 60)
	r.MA120 = sma(bars, idx, 120)
	r.MA250 = sma(bars, idx, 250)
}

func (r *Report) loadPriceStats(bars []data.DailyBar, idx int) {
	r.Chg5d = (bars[idx].Close/bars[maxI(0, idx-5)].Close - 1) * 100
	r.Chg20d = (bars[idx].Close/bars[maxI(0, idx-20)].Close - 1) * 100

	start60 := idx - 60
	if start60 < 0 {
		start60 = 0
	}
	r.High60 = bars[idx].High
	r.Low60 = bars[idx].Low
	for i := start60; i <= idx; i++ {
		if bars[i].High > r.High60 {
			r.High60 = bars[i].High
		}
		if bars[i].Low < r.Low60 {
			r.Low60 = bars[i].Low
		}
	}
}

func (r *Report) loadFundamentals(store *data.FundamentalStore, code, latestDate string) {
	if store == nil {
		return
	}
	b := store.GetDailyBasicAsOf(code, latestDate)
	if b == nil {
		return
	}
	r.PE = b.Pe
	r.PETTM = b.PeTTM
	r.PB = b.Pb
	r.MarketCap = b.TotalMv
	r.DivYield = b.DvRatio
	r.Turnover = b.TurnoverRate
}

func (r *Report) loadStrategySignals(bars []data.DailyBar, idx int, codeMap map[string][]data.DailyBar, fundStore *data.FundamentalStore) {
	reg := strategy.DefaultRegistry()
	strategies := reg.All()
	sort.Slice(strategies, func(i, j int) bool { return strategies[i].Name() < strategies[j].Name() })
	for _, s := range strategies {
		if fundStore != nil {
			if u, ok := s.(strategy.FundStoreUser); ok {
				u.SetFundStore(fundStore)
			}
		}
		if u, ok := s.(strategy.UniverseUser); ok {
			u.SetUniverse(codeMap)
		}
		if idx < s.Warmup() {
			continue
		}
		sig := s.Signal(bars, idx)
		switch sig {
		case strategy.Buy:
			r.BuySignals = append(r.BuySignals, s.Name())
		case strategy.Sell:
			r.SellSignals = append(r.SellSignals, s.Name())
		}
	}
}

func loadFundStore(rawDir string) *data.FundamentalStore {
	fetcher := data.NewFetcher(nil, rawDir, nil)
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

func (r *Report) SetAdjFactor(factor float64) {
	r.AdjFactor = factor
	r.RawPrice = r.Latest.Close / factor
}

func (r *Report) Print() {
	fmt.Println()
	fmt.Printf("╔══════════════════════════════════════════╗\n")
	fmt.Printf("║  %-10s  %-8s", r.Code, r.Name)
	if r.Industry != "" {
		fmt.Printf("  %s", r.Industry)
	}
	fmt.Println()
	fmt.Printf("╠══════════════════════════════════════════╣\n")
	fmt.Printf("║  数据: %d条  %s\n", r.Bars, r.DateRange)

	if r.AdjFactor > 0 {
		fmt.Printf("║  最新: %s  不复权 ¥%.2f  前复权 ¥%.2f\n",
			r.Latest.TradeDate, r.RawPrice, r.Latest.Close)
	} else {
		fmt.Printf("║  最新: %s  ¥%.2f  O:%.2f H:%.2f L:%.2f V:%.0f\n",
			r.Latest.TradeDate, r.Latest.Close, r.Latest.Open, r.Latest.High, r.Latest.Low, r.Latest.Vol)
	}
	fmt.Printf("╠══════════════════════════════════════════╣\n")

	if r.Realtime != nil && r.Realtime.Current > 0 {
		fmt.Printf("║  实时: ¥%.2f  %+.2f%%  开%.2f 高%.2f 低%.2f  [%s %s]\n",
			r.Realtime.Current, r.Realtime.ChangePct,
			r.Realtime.Open, r.Realtime.High, r.Realtime.Low,
			r.Realtime.Source, r.Realtime.UpdateAt)
		if r.AdjFactor > 0 {
			fmt.Printf("║  真实价均线: MA5:%.2f MA10:%.2f MA20:%.2f MA60:%.2f\n",
				r.MA5/r.AdjFactor, r.MA10/r.AdjFactor, r.MA20/r.AdjFactor, r.MA60/r.AdjFactor)
		}
	}

	fmt.Printf("║  MA5:%.2f  MA10:%.2f  MA20:%.2f\n", r.MA5, r.MA10, r.MA20)
	fmt.Printf("║  MA60:%.2f  MA120:%.2f  MA250:%.2f\n", r.MA60, r.MA120, r.MA250)
	fmt.Printf("║  5日: %+.2f%%  20日: %+.2f%%\n", r.Chg5d, r.Chg20d)
	fmt.Printf("║  60日区间: %.2f ~ %.2f\n", r.Low60, r.High60)

	positions := ""
	if r.MA20 > 0 {
		positions += fmt.Sprintf(" vsMA20:%+.1f%%", (r.Latest.Close/r.MA20-1)*100)
	}
	if r.MA60 > 0 {
		positions += fmt.Sprintf(" vsMA60:%+.1f%%", (r.Latest.Close/r.MA60-1)*100)
	}
	fmt.Printf("║  位置:%s\n", positions)

	fmt.Printf("╠══════════════════════════════════════════╣\n")
	if r.MarketCap > 0 {
		fmt.Printf("║  PE:%.2f  PE_TTM:%.2f  PB:%.2f  市值:%.0f亿\n",
			r.PE, r.PETTM, r.PB, r.MarketCap/10000)
		fmt.Printf("║  股息率:%.2f%%  换手率:%.2f%%\n", r.DivYield, r.Turnover)
	}
	if r.IndustryPETTM > 0 {
		fmt.Printf("║  行业PE_TTM:%.2f（聚合，样本%d；算术均值%.2f）\n",
			r.IndustryPETTM, r.IndustryPETTMCount, r.IndustryPETTMAvg)
	}

	fmt.Printf("╠══════════════════════════════════════════╣\n")
	fmt.Printf("║  策略信号: 买入 %d  卖出 %d  观望 %d\n",
		len(r.BuySignals), len(r.SellSignals), strategy.DefaultRegistry().Count()-len(r.BuySignals)-len(r.SellSignals))
	if len(r.BuySignals) > 0 {
		fmt.Printf("║  BUY:  %v\n", r.BuySignals)
	}
	if len(r.SellSignals) > 0 {
		fmt.Printf("║  SELL: %v\n", r.SellSignals)
	}
	fmt.Printf("╚══════════════════════════════════════════╝\n")
	fmt.Println()
}

func sma(bars []data.DailyBar, idx, period int) float64 {
	if period <= 0 || idx < period-1 {
		return 0
	}
	var sum float64
	for i := idx - period + 1; i <= idx; i++ {
		sum += bars[i].Close
	}
	return sum / float64(period)
}

func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}
