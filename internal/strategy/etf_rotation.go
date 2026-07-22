package strategy

import "quant/internal/data"

type ETFRotation struct {
	MomentumPeriod int
	ShortMA        int
	LongMA         int
	MinReturn      float64
	fundStore      *data.FundamentalStore
}

func NewETFRotation(momentumPeriod, shortMA, longMA int, minReturn float64) *ETFRotation {
	return &ETFRotation{
		MomentumPeriod: momentumPeriod,
		ShortMA:        shortMA,
		LongMA:         longMA,
		MinReturn:      minReturn,
	}
}

func (e *ETFRotation) SetFundStore(fs interface{}) {
	if s, ok := fs.(*data.FundamentalStore); ok {
		e.fundStore = s
	}
}

func (e *ETFRotation) Name() string { return "etf_rotation" }
func (e *ETFRotation) Warmup() int {
	m := e.MomentumPeriod
	if e.LongMA > m {
		m = e.LongMA
	}
	return m
}

func (e *ETFRotation) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < e.Warmup() || idx < 1 {
		return Hold
	}

	momentum := (bars[idx].Close - bars[idx-e.MomentumPeriod].Close) / bars[idx-e.MomentumPeriod].Close * 100
	shortMA := sma(bars, idx, e.ShortMA)
	longMA := sma(bars, idx, e.LongMA)
	prevShortMA := sma(bars, idx-1, e.ShortMA)
	prevLongMA := sma(bars, idx-1, e.LongMA)

	if longMA <= 0 || prevLongMA <= 0 {
		return Hold
	}

	if momentum > e.MinReturn && shortMA > longMA && prevShortMA <= prevLongMA {
		if e.fundStore != nil {
			code := bars[idx].TsCode
			mv := e.fundStore.GetMarketCap(code, bars[idx].TradeDate)
			if mv > 0 && mv < 500000 {
				return Hold
			}
		}
		return Buy
	}

	if shortMA < longMA && prevShortMA >= prevLongMA {
		return Sell
	}

	return Hold
}

func (e *ETFRotation) Score(bars []data.DailyBar, idx int) float64 {
	if idx < e.Warmup() {
		return 0
	}
	momentum := (bars[idx].Close - bars[idx-e.MomentumPeriod].Close) / bars[idx-e.MomentumPeriod].Close * 100
	if e.fundStore != nil {
		code := bars[idx].TsCode
		pe, _, ok := e.fundStore.GetLatestPE(code)
		if ok && pe > 0 && pe < 30 {
			momentum += 5
		}
	}
	return momentum
}
