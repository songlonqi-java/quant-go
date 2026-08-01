package dataset

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"quant/internal/data"
)

type LoadOptions struct {
	RawDir           string
	LatestOnly       bool
	FilterST         bool
	MinMarketCap     float64
	LoadFundamentals bool
}

type Dataset struct {
	Bars              []data.DailyBar
	AllCodeMap        map[string][]data.DailyBar
	CodeMap           map[string][]data.DailyBar
	StockNames        map[string]string
	Fundamentals      *data.FundamentalStore
	StkLimits         *data.StkLimitStore
	Moneyflows        *data.MoneyflowStore
	TradingDates      []string
	LatestDate        string
	SkippedStale      int
	FilteredST        int
	FilteredNoBasic   int
	FilteredMarketCap int
}

func Load(opts LoadOptions) (*Dataset, error) {
	if opts.RawDir == "" {
		opts.RawDir = "./data/raw"
	}

	bars, err := data.ReadParquetDir(filepath.Join(opts.RawDir, "daily"))
	if err != nil {
		return nil, fmt.Errorf("加载数据失败: %w", err)
	}

	fetcher := data.NewFetcher(nil, opts.RawDir, nil)
	stkLimits, _ := fetcher.LoadStkLimitStore()
	bars = data.ApplyStkLimits(bars, stkLimits)
	sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })

	allCodeMap := sortedCodeMap(bars)
	ds := &Dataset{
		Bars:         bars,
		AllCodeMap:   allCodeMap,
		CodeMap:      cloneCodeMap(allCodeMap),
		StockNames:   data.LoadStockNames(filepath.Join(opts.RawDir, "stocks.parquet")),
		StkLimits:    stkLimits,
		TradingDates: data.LoadTradeDates(opts.RawDir, bars),
	}
	if opts.LoadFundamentals || opts.MinMarketCap > 0 {
		ds.Fundamentals = LoadFundamentals(opts.RawDir)
	}
	ds.LatestDate = latestTradeDate(ds.CodeMap)

	if opts.LatestOnly {
		ds.keepLatestOnly()
	}
	if opts.FilterST {
		ds.filterST()
	}
	if opts.MinMarketCap > 0 {
		ds.filterByMarketCap(opts.MinMarketCap)
	}

	ds.Moneyflows, _ = fetcher.LoadMoneyflowStore()
	return ds, nil
}

func cloneCodeMap(source map[string][]data.DailyBar) map[string][]data.DailyBar {
	cloned := make(map[string][]data.DailyBar, len(source))
	for code, bars := range source {
		cloned[code] = bars
	}
	return cloned
}

func LoadFundamentals(rawDir string) *data.FundamentalStore {
	fetcher := data.NewFetcher(nil, rawDir, nil)
	store := data.NewFundamentalStore()

	if basicStore, err := fetcher.LoadDailyBasicStore(); err == nil && basicStore != nil {
		store.MergeFrom(basicStore)
	}
	if finaStore, err := fetcher.LoadFinaStore(); err == nil && finaStore != nil {
		store.MergeFrom(finaStore)
	}
	if !store.HasData() {
		return nil
	}
	return store
}

func (d *Dataset) RecentBars(lookback int) []data.DailyBar {
	if lookback <= 0 {
		lookback = 1
	}
	recent := make([]data.DailyBar, 0, len(d.CodeMap)*lookback)
	for _, bars := range d.CodeMap {
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

// ActiveBars returns the complete history for the current, investable universe.
// It applies the same latest-date, ST, and market-cap filters as CodeMap, so
// market analysis cannot accidentally include securities that signal generation
// has excluded.
func (d *Dataset) ActiveBars() []data.DailyBar {
	if len(d.CodeMap) == 0 {
		return nil
	}
	active := make([]data.DailyBar, 0, len(d.Bars))
	for _, bars := range d.CodeMap {
		active = append(active, bars...)
	}
	sort.Slice(active, func(i, j int) bool {
		if active[i].TradeDate == active[j].TradeDate {
			return active[i].TsCode < active[j].TsCode
		}
		return active[i].TradeDate < active[j].TradeDate
	})
	return active
}

func (d *Dataset) PriceQuality(lookback int) data.PriceDataQuality {
	return data.CheckPriceDataQuality(d.RecentBars(lookback))
}

func (d *Dataset) keepLatestOnly() {
	if d.LatestDate == "" {
		return
	}
	filtered := make(map[string][]data.DailyBar, len(d.CodeMap))
	for code, bars := range d.CodeMap {
		if len(bars) == 0 {
			continue
		}
		if bars[len(bars)-1].TradeDate == d.LatestDate {
			filtered[code] = bars
		} else {
			d.SkippedStale++
		}
	}
	d.CodeMap = filtered
}

func (d *Dataset) filterST() {
	for code, name := range d.StockNames {
		if isSTName(name) {
			if _, ok := d.CodeMap[code]; ok {
				delete(d.CodeMap, code)
				d.FilteredST++
			}
		}
	}
}

func (d *Dataset) filterByMarketCap(minMarketCap float64) {
	for code := range d.CodeMap {
		marketCap := 0.0
		if d.Fundamentals != nil {
			marketCap = d.Fundamentals.GetMarketCap(code, d.LatestDate)
		}
		if marketCap <= 0 {
			delete(d.CodeMap, code)
			d.FilteredNoBasic++
			continue
		}
		if marketCap < minMarketCap*10000 {
			delete(d.CodeMap, code)
			d.FilteredMarketCap++
		}
	}
}

func sortedCodeMap(bars []data.DailyBar) map[string][]data.DailyBar {
	codeMap := data.GroupByCode(bars)
	for code, stockBars := range codeMap {
		sort.Slice(stockBars, func(i, j int) bool {
			if stockBars[i].TradeDate == stockBars[j].TradeDate {
				return stockBars[i].TsCode < stockBars[j].TsCode
			}
			return stockBars[i].TradeDate < stockBars[j].TradeDate
		})
		codeMap[code] = stockBars
	}
	return codeMap
}

func latestTradeDate(codeMap map[string][]data.DailyBar) string {
	var latest string
	for _, bars := range codeMap {
		if len(bars) == 0 {
			continue
		}
		if last := bars[len(bars)-1].TradeDate; last > latest {
			latest = last
		}
	}
	return latest
}

func isSTName(name string) bool {
	name = strings.ToUpper(strings.TrimSpace(name))
	return strings.Contains(name, "ST")
}
