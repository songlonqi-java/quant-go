package strategy

import "quant/internal/data"

type DividendDeviation struct {
	LongMAPeriod int
	BuyDiscount  float64
	SellPremium  float64
	fundStore    *data.FundamentalStore
}

func NewDividendDeviation(longMA int, buyDiscount, sellPremium float64) *DividendDeviation {
	return &DividendDeviation{
		LongMAPeriod: longMA,
		BuyDiscount:  buyDiscount,
		SellPremium:  sellPremium,
	}
}

func (d *DividendDeviation) SetFundStore(fs interface{}) {
	if s, ok := fs.(*data.FundamentalStore); ok {
		d.fundStore = s
	}
}

func (d *DividendDeviation) Name() string { return "dividend_deviation" }
func (d *DividendDeviation) Warmup() int  { return d.LongMAPeriod }

func (d *DividendDeviation) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < d.Warmup() || idx < 1 {
		return Hold
	}

	longMA := sma(bars, idx, d.LongMAPeriod)
	if longMA <= 0 {
		return Hold
	}

	curBar := bars[idx]
	prevBar := bars[idx-1]

	buyThreshold := longMA * d.BuyDiscount
	sellThreshold := longMA * d.SellPremium

	buySig := curBar.Close <= buyThreshold && curBar.Close > curBar.Open && prevBar.Close > prevBar.Open
	sellSig := curBar.Close >= sellThreshold && hasLongUpperShadow(curBar)

	if buySig {
		if d.fundStore == nil {
			return Hold
		}
		code := bars[idx].TsCode
		dv := d.fundStore.GetDailyBasicAsOf(code, bars[idx].TradeDate)
		if dv == nil || !isSensibleHighDividend(dividendYield(dv)) {
			return Hold
		}
		return Buy
	}
	if sellSig {
		return Sell
	}
	return Hold
}

func (d *DividendDeviation) Score(bars []data.DailyBar, idx int) float64 {
	if idx < d.Warmup() {
		return 0
	}
	longMA := sma(bars, idx, d.LongMAPeriod)
	if longMA <= 0 {
		return 0
	}
	score := (1 - bars[idx].Close/longMA) * 100

	if d.fundStore != nil {
		code := bars[idx].TsCode
		if dv := d.fundStore.GetDailyBasicAsOf(code, bars[idx].TradeDate); dv != nil && isSensibleHighDividend(dividendYield(dv)) {
			score += 10
		}
	}
	return score
}

func dividendYield(dv *data.DailyBasic) float64 {
	if dv == nil {
		return 0
	}
	if dv.DvTTM > 0 {
		return dv.DvTTM
	}
	return dv.DvRatio
}

func isSensibleHighDividend(yield float64) bool {
	return yield >= 3 && yield <= 15
}

func hasLongUpperShadow(bar data.DailyBar) bool {
	bodyHigh := bar.Close
	if bar.Open > bar.Close {
		bodyHigh = bar.Open
	}
	bodyLow := bar.Close
	if bar.Open < bar.Close {
		bodyLow = bar.Open
	}
	bodySize := bodyHigh - bodyLow
	upperShadow := bar.High - bodyHigh
	if bodySize <= 0 {
		return upperShadow > 0
	}
	return upperShadow > bodySize*0.5
}
