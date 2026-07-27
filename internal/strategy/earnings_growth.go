package strategy

import "quant/internal/data"

// EarningsGrowth 盈利增长趋势策略。
// 适合 A 股中长期趋势行情：业绩增速确认后，只在价格趋势没有破坏时参与。
type EarningsGrowth struct {
	ShortMA       int
	LongMA        int
	MaxPETTM      float64
	MinProfitYoY  float64
	MinRevenueYoY float64
	MinROE        float64
	fundStore     *data.FundamentalStore
}

func NewEarningsGrowth(shortMA, longMA int, maxPETTM, minProfitYoY, minRevenueYoY, minROE float64) *EarningsGrowth {
	return &EarningsGrowth{
		ShortMA:       shortMA,
		LongMA:        longMA,
		MaxPETTM:      maxPETTM,
		MinProfitYoY:  minProfitYoY,
		MinRevenueYoY: minRevenueYoY,
		MinROE:        minROE,
	}
}

func (e *EarningsGrowth) SetFundStore(fs interface{}) {
	if s, ok := fs.(*data.FundamentalStore); ok {
		e.fundStore = s
	}
}

func (e *EarningsGrowth) Name() string { return "earnings_growth" }
func (e *EarningsGrowth) Warmup() int  { return maxInt(e.ShortMA, e.LongMA) }

func (e *EarningsGrowth) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < e.Warmup() || e.fundStore == nil {
		return Hold
	}
	shortMA := sma(bars, idx, e.ShortMA)
	longMA := sma(bars, idx, e.LongMA)
	if shortMA <= 0 || longMA <= 0 {
		return Hold
	}
	if bars[idx].Close < longMA {
		return Sell
	}
	if !e.fundamentalsOK(bars[idx].TsCode, bars[idx].TradeDate) {
		return Hold
	}
	if bars[idx].Close > shortMA && shortMA > longMA {
		return Buy
	}
	return Hold
}

func (e *EarningsGrowth) Score(bars []data.DailyBar, idx int) float64 {
	if idx < e.Warmup() || e.fundStore == nil {
		return 0
	}
	db := e.fundStore.GetDailyBasicAsOf(bars[idx].TsCode, bars[idx].TradeDate)
	fi, ok := e.fundStore.GetFinaIndicatorAsOf(bars[idx].TsCode, bars[idx].TradeDate)
	if db == nil || !ok {
		return 0
	}
	pe := peValue(db)
	valuationPenalty := 0.0
	if pe > 0 {
		valuationPenalty = pe / e.MaxPETTM * 10
	}
	trendScore := 0.0
	longMA := sma(bars, idx, e.LongMA)
	if longMA > 0 {
		trendScore = (bars[idx].Close/longMA - 1) * 20
	}
	return fi.NIncomeYoY*0.35 + fi.RevenueYoY*0.25 + fi.Roe*0.4 + trendScore - valuationPenalty
}

func (e *EarningsGrowth) fundamentalsOK(code, date string) bool {
	db := e.fundStore.GetDailyBasicAsOf(code, date)
	fi, ok := e.fundStore.GetFinaIndicatorAsOf(code, date)
	if db == nil || !ok {
		return false
	}
	pe := peValue(db)
	if pe <= 0 || pe > e.MaxPETTM {
		return false
	}
	return fi.NIncomeYoY >= e.MinProfitYoY &&
		fi.RevenueYoY >= e.MinRevenueYoY &&
		fi.Roe >= e.MinROE
}
