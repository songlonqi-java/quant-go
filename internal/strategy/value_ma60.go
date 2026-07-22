package strategy

import (
	"math"
	"quant/internal/data"
)

type ValueMA60 struct {
	MAPeriod  int
	BBPeriod  int
	BBMult    float64
	fundStore *data.FundamentalStore
}

func NewValueMA60(maPeriod, bbPeriod int, bbMult float64) *ValueMA60 {
	return &ValueMA60{MAPeriod: maPeriod, BBPeriod: bbPeriod, BBMult: bbMult}
}

func (v *ValueMA60) SetFundStore(fs interface{}) {
	if s, ok := fs.(*data.FundamentalStore); ok {
		v.fundStore = s
	}
}

func (v *ValueMA60) Name() string { return "value_ma60" }
func (v *ValueMA60) Warmup() int {
	if v.MAPeriod > v.BBPeriod {
		return v.MAPeriod
	}
	return v.BBPeriod
}

func (v *ValueMA60) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < v.Warmup() || idx < 1 {
		return Hold
	}

	sig := v.technicalSignal(bars, idx)
	if sig == Hold {
		return Hold
	}
	if sig == Buy && v.fundStore != nil {
		code := bars[idx].TsCode
		mv := v.fundStore.GetMarketCap(code, bars[idx].TradeDate)
		roe, hasROE := v.fundStore.GetLatestROE(code)
		isHS := v.fundStore.IsHs300(code)
		if isHS && mv > 0 && mv < 5000000 && hasROE && roe >= 15 {
			return Buy
		}
		if !isHS && hasROE && roe >= 20 && mv > 0 && mv < 5000000 {
			return Buy
		}
		return Hold
	}
	return sig
}

func (v *ValueMA60) technicalSignal(bars []data.DailyBar, idx int) SignalType {
	ma60 := sma(bars, idx, v.MAPeriod)
	prevMA60 := sma(bars, idx-1, v.MAPeriod)
	bollU := calcBollUpper(bars, idx, v.BBPeriod, v.BBMult)
	prevBollU := calcBollUpper(bars, idx-1, v.BBPeriod, v.BBMult)
	curClose := bars[idx].Close
	prevClose := bars[idx-1].Close

	if prevClose <= prevMA60 && curClose > ma60 {
		return Buy
	}
	if (prevClose >= prevBollU && curClose < bollU) ||
		(prevClose >= prevMA60 && curClose < ma60) {
		return Sell
	}
	return Hold
}

func (v *ValueMA60) Score(bars []data.DailyBar, idx int) float64 {
	if idx < v.Warmup() {
		return 0
	}
	ma60 := sma(bars, idx, v.MAPeriod)
	bollU := calcBollUpper(bars, idx, v.BBPeriod, v.BBMult)
	price := bars[idx].Close
	if ma60 <= 0 {
		return 0
	}
	trendScore := (price/ma60 - 1) * 100
	upsideScore := (bollU/price - 1) * 100
	score := trendScore*0.6 + upsideScore*0.4

	if v.fundStore != nil {
		code := bars[idx].TsCode
		if roe, ok := v.fundStore.GetLatestROE(code); ok && roe > 0 {
			score += roe * 0.2
		}
		if v.fundStore.IsHs300(code) {
			score += 5
		}
	}
	return score
}

func calcBollUpper(bars []data.DailyBar, idx, period int, mult float64) float64 {
	mid := sma(bars, idx, period)
	if mid <= 0 || idx < period-1 {
		return mid
	}
	var variance float64
	count := 0
	for i := idx - period + 1; i <= idx; i++ {
		diff := bars[i].Close - mid
		variance += diff * diff
		count++
	}
	if count == 0 {
		return mid
	}
	stddev := math.Sqrt(variance / float64(count))
	return mid + mult*stddev
}
