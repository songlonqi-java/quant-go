package data

import (
	"fmt"
	"path/filepath"
	"sort"
)

type StkLimitStore struct {
	limits map[string]StkLimit
}

func NewStkLimitStore(limits []StkLimit) *StkLimitStore {
	store := &StkLimitStore{limits: make(map[string]StkLimit, len(limits))}
	for _, l := range limits {
		store.limits[limitKey(l.TsCode, l.TradeDate)] = l
	}
	return store
}

func (s *StkLimitStore) Get(tsCode, tradeDate string) (StkLimit, bool) {
	if s == nil {
		return StkLimit{}, false
	}
	limit, ok := s.limits[limitKey(tsCode, tradeDate)]
	return limit, ok
}

// ApplyStkLimits overwrites the limit prices on each bar in place. All callers
// pass a freshly decoded slice, so mutating avoids a second full copy of the
// daily dataset (tens of GB of peak heap across a load + replay).
func ApplyStkLimits(bars []DailyBar, store *StkLimitStore) []DailyBar {
	if store == nil {
		return bars
	}
	for i := range bars {
		if limit, ok := store.Get(bars[i].TsCode, bars[i].TradeDate); ok {
			bars[i].UpLimit = limit.UpLimit
			bars[i].DownLimit = limit.DownLimit
		}
	}
	return bars
}

type MoneyflowStore struct {
	flows map[string]Moneyflow
}

func NewMoneyflowStore(flows []Moneyflow) *MoneyflowStore {
	store := &MoneyflowStore{flows: make(map[string]Moneyflow, len(flows))}
	for _, f := range flows {
		store.flows[limitKey(f.TsCode, f.TradeDate)] = f
	}
	return store
}

func (s *MoneyflowStore) Get(tsCode, tradeDate string) (Moneyflow, bool) {
	if s == nil {
		return Moneyflow{}, false
	}
	flow, ok := s.flows[limitKey(tsCode, tradeDate)]
	return flow, ok
}

func (m Moneyflow) LargeNetAmount() float64 {
	return (m.BuyLgAmount + m.BuyElgAmount) - (m.SellLgAmount + m.SellElgAmount)
}

func (m Moneyflow) SmallNetAmount() float64 {
	return m.BuySmAmount - m.SellSmAmount
}

func limitKey(tsCode, tradeDate string) string {
	return tsCode + "|" + tradeDate
}

func (f *Fetcher) LoadStkLimitStore() (*StkLimitStore, error) {
	limits, err := readParquetFiles[StkLimit](filepath.Join(f.rawDir, "stk_limit"))
	if err != nil {
		return nil, err
	}
	return NewStkLimitStore(limits), nil
}

func (f *Fetcher) LoadMoneyflowStore() (*MoneyflowStore, error) {
	flows, err := readParquetFiles[Moneyflow](filepath.Join(f.rawDir, "moneyflow"))
	if err != nil {
		return nil, err
	}
	return NewMoneyflowStore(flows), nil
}

func readParquetFiles[T any](dir string) ([]T, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.parquet"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	sort.Strings(files)

	var out []T
	for _, file := range files {
		rows, err := readGenericParquet[T](file)
		if err != nil {
			return out, fmt.Errorf("读取 %s 失败: %w", file, err)
		}
		out = append(out, rows...)
	}
	return out, nil
}
