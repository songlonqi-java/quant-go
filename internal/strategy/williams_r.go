package strategy

import "quant/internal/data"

// Williams %R 威廉指标
// %R上穿-80 (超卖回升) → 买入；%R下穿-20 (超买回落) → 卖出
type WilliamsR struct {
	Period   int     // 14
	Oversold float64 // -80
	Overbought float64 // -20
}

func NewWilliamsR(period int, oversold, overbought float64) *WilliamsR {
	return &WilliamsR{Period: period, Oversold: oversold, Overbought: overbought}
}

func (w *WilliamsR) Name() string { return "williams_r" }
func (w *WilliamsR) Warmup() int  { return w.Period }

func (w *WilliamsR) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < w.Warmup() || idx < 1 {
		return Hold
	}
	cur := w.pctR(bars, idx)
	prev := w.pctR(bars, idx-1)
	if prev <= w.Oversold && cur > w.Oversold {
		return Buy
	}
	if prev >= w.Overbought && cur < w.Overbought {
		return Sell
	}
	return Hold
}

func (w *WilliamsR) Score(bars []data.DailyBar, idx int) float64 {
	if idx < w.Warmup() {
		return 0
	}
	return w.pctR(bars, idx) + 50
}

func (w *WilliamsR) pctR(bars []data.DailyBar, idx int) float64 {
	start := idx - w.Period + 1
	if start < 0 {
		start = 0
	}
	high := bars[start].High
	low := bars[start].Low
	for i := start; i <= idx; i++ {
		if bars[i].High > high {
			high = bars[i].High
		}
		if bars[i].Low < low {
			low = bars[i].Low
		}
	}
	if high == low {
		return -50
	}
	return (high - bars[idx].Close) / (high - low) * -100
}
