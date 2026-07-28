package strategy

import (
	"math"

	"quant/internal/data"
)

// ATRBreakout 低波动放量突破策略。
// 价格突破前 N 日高点，ATR% 不过高，且成交量放大时买入；跌破 MA20 时卖出。
type ATRBreakout struct {
	BreakoutPeriod int
	ATRPeriod      int
	VolumePeriod   int
	MaxATRPct      float64
	VolumeRatio    float64
}

func NewATRBreakout(breakoutPeriod, atrPeriod, volumePeriod int, maxATRPct, volumeRatio float64) *ATRBreakout {
	return &ATRBreakout{
		BreakoutPeriod: breakoutPeriod,
		ATRPeriod:      atrPeriod,
		VolumePeriod:   volumePeriod,
		MaxATRPct:      maxATRPct,
		VolumeRatio:    volumeRatio,
	}
}

func (a *ATRBreakout) Name() string { return "atr_breakout" }
func (a *ATRBreakout) Warmup() int {
	return maxInt(60, maxInt(a.BreakoutPeriod, maxInt(a.ATRPeriod, a.VolumePeriod))) + 1
}

func (a *ATRBreakout) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < a.Warmup() {
		return Hold
	}
	prevHigh := highestHigh(bars, idx-1, a.BreakoutPeriod)
	atr := atrValue(bars, idx, a.ATRPeriod)
	avgVol := avgVolume(bars, idx-1, a.VolumePeriod)
	ma60 := sma(bars, idx, 60)
	if prevHigh <= 0 || atr <= 0 || avgVol <= 0 || ma60 <= 0 {
		return Hold
	}
	atrPct := atr / bars[idx].Close * 100
	volumeOK := bars[idx].Vol > avgVol*a.VolumeRatio
	breakout := bars[idx].Close > prevHigh && bars[idx-1].Close <= prevHigh
	if breakout && volumeOK && atrPct <= a.MaxATRPct && bars[idx].Close > ma60 {
		return Buy
	}
	ma20 := sma(bars, idx, 20)
	prevMA20 := sma(bars, idx-1, 20)
	if ma20 > 0 && prevMA20 > 0 && bars[idx-1].Close >= prevMA20 && bars[idx].Close < ma20 {
		return Sell
	}
	return Hold
}

func (a *ATRBreakout) Score(bars []data.DailyBar, idx int) float64 {
	if idx < a.Warmup() {
		return 0
	}
	atr := atrValue(bars, idx, a.ATRPeriod)
	avgVol := avgVolume(bars, idx-1, a.VolumePeriod)
	if atr <= 0 || avgVol <= 0 || bars[idx].Close <= 0 {
		return 0
	}
	prevHigh := highestHigh(bars, idx-1, a.BreakoutPeriod)
	if prevHigh <= 0 {
		return 0
	}
	atrScore := math.Max(0, a.MaxATRPct-atr/bars[idx].Close*100)
	volScore := bars[idx].Vol / avgVol
	breakScore := (bars[idx].Close/prevHigh - 1) * 100
	return atrScore + volScore*3 + breakScore
}

func highestHigh(bars []data.DailyBar, idx, period int) float64 {
	start := idx - period + 1
	if start < 0 {
		start = 0
	}
	high := bars[start].High
	for i := start; i <= idx; i++ {
		if bars[i].High > high {
			high = bars[i].High
		}
	}
	return high
}

func atrValue(bars []data.DailyBar, idx, period int) float64 {
	if idx < period {
		return 0
	}
	var sum float64
	for i := idx - period + 1; i <= idx; i++ {
		highLow := bars[i].High - bars[i].Low
		highPrevClose := math.Abs(bars[i].High - bars[i-1].Close)
		lowPrevClose := math.Abs(bars[i].Low - bars[i-1].Close)
		sum += math.Max(highLow, math.Max(highPrevClose, lowPrevClose))
	}
	return sum / float64(period)
}
