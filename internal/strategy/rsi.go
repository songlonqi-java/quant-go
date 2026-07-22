package strategy

import "quant/internal/data"

type RSI struct {
	Period      int
	Oversold    float64
	Overbought  float64
}

func NewRSI(period int, oversold, overbought float64) *RSI {
	return &RSI{Period: period, Oversold: oversold, Overbought: overbought}
}

func (r *RSI) Name() string { return "rsi" }
func (r *RSI) Warmup() int  { return r.Period + 1 }

func (r *RSI) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < r.Warmup() || idx < 1 {
		return Hold
	}

	cur := rsiValue(bars, idx, r.Period)
	prev := rsiValue(bars, idx-1, r.Period)

	if prev <= r.Oversold && cur > r.Oversold {
		return Buy
	}
	if prev >= r.Overbought && cur < r.Overbought {
		return Sell
	}
	return Hold
}

func (r *RSI) Score(bars []data.DailyBar, idx int) float64 {
	if idx < r.Warmup() {
		return 0
	}
	val := rsiValue(bars, idx, r.Period)
	return (50 - val)
}

func rsiValue(bars []data.DailyBar, idx, period int) float64 {
	if idx < period {
		return 50
	}

	var avgGain, avgLoss float64
	for i := idx - period + 1; i <= idx; i++ {
		chg := bars[i].Close - bars[i-1].Close
		if chg > 0 {
			avgGain += chg
		} else {
			avgLoss += -chg
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}
