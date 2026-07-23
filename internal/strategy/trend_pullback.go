package strategy

import (
	"math"

	"quant/internal/data"
)

// TrendPullback 强趋势回踩策略。
// 多头排列中回踩 MA20 附近并重新收阳企稳时买入，跌破 MA60 或趋势破坏时卖出。
type TrendPullback struct {
	PullbackPct float64
	VolumeRatio float64
}

func NewTrendPullback(pullbackPct, volumeRatio float64) *TrendPullback {
	return &TrendPullback{PullbackPct: pullbackPct, VolumeRatio: volumeRatio}
}

func (t *TrendPullback) Name() string { return "trend_pullback" }
func (t *TrendPullback) Warmup() int  { return 120 }

func (t *TrendPullback) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < t.Warmup() || idx < 1 {
		return Hold
	}
	ma20 := sma(bars, idx, 20)
	ma60 := sma(bars, idx, 60)
	ma120 := sma(bars, idx, 120)
	prevMA20 := sma(bars, idx-1, 20)
	prevMA60 := sma(bars, idx-1, 60)
	if ma20 <= 0 || ma60 <= 0 || ma120 <= 0 {
		return Hold
	}

	uptrend := ma20 > ma60 && ma60 > ma120 && bars[idx].Close > ma60
	touchedMA20 := bars[idx].Low <= ma20*(1+t.PullbackPct/100)
	reclaimedMA20 := bars[idx].Close > ma20 && bars[idx].Close > bars[idx].Open
	heldTrend := bars[idx].Close > bars[idx-1].Low
	avgVol := avgVolume(bars, idx-1, 20)
	volumeControlled := avgVol > 0 && bars[idx].Vol <= avgVol*t.VolumeRatio

	if uptrend && touchedMA20 && reclaimedMA20 && heldTrend && volumeControlled {
		return Buy
	}
	if (prevMA60 > 0 && bars[idx-1].Close >= prevMA60 && bars[idx].Close < ma60) ||
		(prevMA20 > 0 && prevMA60 > 0 && prevMA20 >= prevMA60 && ma20 < ma60) {
		return Sell
	}
	return Hold
}

func (t *TrendPullback) Score(bars []data.DailyBar, idx int) float64 {
	if idx < t.Warmup() {
		return 0
	}
	ma20 := sma(bars, idx, 20)
	ma60 := sma(bars, idx, 60)
	ma120 := sma(bars, idx, 120)
	if ma20 <= 0 || ma60 <= 0 || ma120 <= 0 {
		return 0
	}
	dist := math.Abs(bars[idx].Close/ma20-1) * 100
	trend := (ma20/ma60 - 1) * 100
	longTrend := (ma60/ma120 - 1) * 100
	return trend + longTrend - dist
}
