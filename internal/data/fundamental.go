package data

import (
	"sort"
	"sync"
)

type FundamentalStore struct {
	mu             sync.RWMutex
	dailyBasics    map[string]map[string]*DailyBasic // ts_code → trade_date → *DailyBasic
	finaIndicators map[string][]FinaIndicator        // ts_code → sorted by ann_date desc
	hs300Set       map[string]bool                   // 当前沪深300成分股
	hs300Entries   map[string][]HsConst              // ts_code → membership history
	loaded         bool
}

func NewFundamentalStore() *FundamentalStore {
	return &FundamentalStore{
		dailyBasics:    make(map[string]map[string]*DailyBasic),
		finaIndicators: make(map[string][]FinaIndicator),
		hs300Set:       make(map[string]bool),
		hs300Entries:   make(map[string][]HsConst),
	}
}

func (fs *FundamentalStore) LoadDailyBasics(basics []DailyBasic) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for i := range basics {
		b := &basics[i]
		if fs.dailyBasics[b.TsCode] == nil {
			fs.dailyBasics[b.TsCode] = make(map[string]*DailyBasic)
		}
		fs.dailyBasics[b.TsCode][b.TradeDate] = b
	}
	fs.loaded = true
}

func (fs *FundamentalStore) LoadFinaIndicators(indicators []FinaIndicator) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, fi := range indicators {
		fs.finaIndicators[fi.TsCode] = append(fs.finaIndicators[fi.TsCode], fi)
	}
	for code := range fs.finaIndicators {
		sort.Slice(fs.finaIndicators[code], func(i, j int) bool {
			return fs.finaIndicators[code][i].AnnDate > fs.finaIndicators[code][j].AnnDate
		})
	}
}

func (fs *FundamentalStore) LoadHsConst(consts []HsConst) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, c := range consts {
		fs.hs300Entries[c.TsCode] = append(fs.hs300Entries[c.TsCode], c)
		if c.IsNew == "1" && c.OutDate == "" {
			fs.hs300Set[c.TsCode] = true
		}
	}
	for code := range fs.hs300Entries {
		sort.Slice(fs.hs300Entries[code], func(i, j int) bool {
			return fs.hs300Entries[code][i].InDate > fs.hs300Entries[code][j].InDate
		})
	}
}

func (fs *FundamentalStore) GetDailyBasic(tsCode, date string) *DailyBasic {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if m, ok := fs.dailyBasics[tsCode]; ok {
		return m[date]
	}
	return nil
}

func (fs *FundamentalStore) GetDailyBasicAsOf(tsCode, date string) *DailyBasic {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return latestDailyBasicAsOf(fs.dailyBasics[tsCode], date)
}

func (fs *FundamentalStore) GetLatestPE(tsCode string) (pe, peTTM float64, ok bool) {
	return fs.GetPEAsOf(tsCode, "99999999")
}

func (fs *FundamentalStore) GetPEAsOf(tsCode, date string) (pe, peTTM float64, ok bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if b := latestDailyBasicAsOf(fs.dailyBasics[tsCode], date); b != nil {
		return b.Pe, b.PeTTM, true
	}
	return 0, 0, false
}

func (fs *FundamentalStore) GetMarketCap(tsCode, date string) float64 {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if m, ok := fs.dailyBasics[tsCode]; ok {
		if d, ok2 := m[date]; ok2 {
			return d.TotalMv
		}
		if d := latestDailyBasicAsOf(m, date); d != nil {
			return d.TotalMv
		}
	}
	return 0
}

func (fs *FundamentalStore) GetLatestROE(tsCode string) (float64, bool) {
	return fs.GetROEAsOf(tsCode, "99999999")
}

func (fs *FundamentalStore) GetROEAsOf(tsCode, date string) (float64, bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if list, ok := fs.finaIndicators[tsCode]; ok && len(list) > 0 {
		for _, fi := range list {
			if fi.AnnDate != "" && fi.AnnDate <= date {
				return fi.Roe, true
			}
		}
	}
	return 0, false
}

func (fs *FundamentalStore) GetAverageROE(tsCode string, periods int) (float64, bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	list, ok := fs.finaIndicators[tsCode]
	if !ok || len(list) == 0 {
		return 0, false
	}
	count := periods
	if count > len(list) {
		count = len(list)
	}
	if count == 0 {
		return 0, false
	}
	var sum float64
	for i := 0; i < count; i++ {
		sum += list[i].Roe
	}
	return sum / float64(count), true
}

func (fs *FundamentalStore) IsHs300(tsCode string) bool {
	return fs.IsHs300AsOf(tsCode, "99999999")
}

func (fs *FundamentalStore) IsHs300AsOf(tsCode, date string) bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if entries, ok := fs.hs300Entries[tsCode]; ok {
		for _, e := range entries {
			if e.InDate != "" && e.InDate <= date && (e.OutDate == "" || e.OutDate > date) {
				return true
			}
		}
		return false
	}
	return fs.hs300Set[tsCode]
}

func (fs *FundamentalStore) GetTurnover(tsCode, date string) float64 {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if m, ok := fs.dailyBasics[tsCode]; ok {
		if d, ok2 := m[date]; ok2 {
			return d.TurnoverRate
		}
		if d := latestDailyBasicAsOf(m, date); d != nil {
			return d.TurnoverRate
		}
	}
	return 0
}

func (fs *FundamentalStore) MergeFrom(other *FundamentalStore) {
	if other == nil {
		return
	}

	other.mu.RLock()
	dailyBasicsCopy := make(map[string]map[string]*DailyBasic)
	for code, dateMap := range other.dailyBasics {
		m := make(map[string]*DailyBasic)
		for date, b := range dateMap {
			cp := *b
			m[date] = &cp
		}
		dailyBasicsCopy[code] = m
	}

	var allFina []FinaIndicator
	for _, list := range other.finaIndicators {
		allFina = append(allFina, list...)
	}

	hsCopy := make(map[string]bool)
	for code := range other.hs300Set {
		hsCopy[code] = true
	}
	hsEntriesCopy := make(map[string][]HsConst)
	for code, entries := range other.hs300Entries {
		hsEntriesCopy[code] = append([]HsConst(nil), entries...)
	}
	loaded := other.loaded
	other.mu.RUnlock()

	fs.mu.Lock()
	defer fs.mu.Unlock()

	for code, dateMap := range dailyBasicsCopy {
		if fs.dailyBasics[code] == nil {
			fs.dailyBasics[code] = make(map[string]*DailyBasic)
		}
		for date, b := range dateMap {
			fs.dailyBasics[code][date] = b
		}
	}

	for _, fi := range allFina {
		fs.finaIndicators[fi.TsCode] = append(fs.finaIndicators[fi.TsCode], fi)
	}
	for code := range hsCopy {
		fs.hs300Set[code] = true
	}
	for code, entries := range hsEntriesCopy {
		fs.hs300Entries[code] = append(fs.hs300Entries[code], entries...)
	}
	if loaded {
		fs.loaded = true
	}
}

func (fs *FundamentalStore) HasData() bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.loaded || len(fs.finaIndicators) > 0 || len(fs.hs300Set) > 0 || len(fs.hs300Entries) > 0
}

func latestDailyBasicAsOf(m map[string]*DailyBasic, date string) *DailyBasic {
	if len(m) == 0 {
		return nil
	}
	var latest string
	for d := range m {
		if d <= date && d > latest {
			latest = d
		}
	}
	if latest == "" {
		return nil
	}
	return m[latest]
}
