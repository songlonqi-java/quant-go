package data

import (
	"sort"
	"sync"
)

type FundamentalStore struct {
	mu             sync.RWMutex
	dailyBasics    map[string]map[string]*DailyBasic // ts_code → trade_date → *DailyBasic
	finaIndicators map[string][]FinaIndicator         // ts_code → sorted by end_date desc
	hs300Set       map[string]bool                    // 当前沪深300成分股
	loaded         bool
}

func NewFundamentalStore() *FundamentalStore {
	return &FundamentalStore{
		dailyBasics:    make(map[string]map[string]*DailyBasic),
		finaIndicators: make(map[string][]FinaIndicator),
		hs300Set:       make(map[string]bool),
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
			return fs.finaIndicators[code][i].EndDate > fs.finaIndicators[code][j].EndDate
		})
	}
}

func (fs *FundamentalStore) LoadHsConst(consts []HsConst) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, c := range consts {
		if c.IsNew == "1" && c.OutDate == "" {
			fs.hs300Set[c.TsCode] = true
		}
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

func (fs *FundamentalStore) GetLatestPE(tsCode string) (pe, peTTM float64, ok bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if m, ok2 := fs.dailyBasics[tsCode]; ok2 {
		var latest string
		for date := range m {
			if date > latest {
				latest = date
			}
		}
		if latest != "" && m[latest] != nil {
			return m[latest].Pe, m[latest].PeTTM, true
		}
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
	}
	return 0
}

func (fs *FundamentalStore) GetLatestROE(tsCode string) (float64, bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if list, ok := fs.finaIndicators[tsCode]; ok && len(list) > 0 {
		return list[0].Roe, true
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
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.hs300Set[tsCode]
}

func (fs *FundamentalStore) GetTurnover(tsCode, date string) float64 {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if m, ok := fs.dailyBasics[tsCode]; ok {
		if d, ok2 := m[date]; ok2 {
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
	if loaded {
		fs.loaded = true
	}
}

func (fs *FundamentalStore) HasData() bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.loaded || len(fs.finaIndicators) > 0 || len(fs.hs300Set) > 0
}
