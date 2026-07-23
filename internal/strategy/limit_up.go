package strategy

import "quant/internal/data"

// LimitUp 涨停板策略
// 涨停(涨幅>=9.5%)次日放量高开不破 → 追涨买入
// 买入后次日不涨停 → 卖出
type LimitUp struct {
	LimitPct    float64 // 涨停阈值 9.5%
	VolumeRatio float64 // 次日放量倍数 1.2
}

func NewLimitUp(limitPct, volumeRatio float64) *LimitUp {
	return &LimitUp{LimitPct: limitPct, VolumeRatio: volumeRatio}
}

func (l *LimitUp) Name() string { return "limit_up" }
func (l *LimitUp) Warmup() int  { return 2 }

func (l *LimitUp) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < l.Warmup() || idx < 1 {
		return Hold
	}

	prevChg := (bars[idx-1].Close/bars[idx-2].Close - 1) * 100
	isLimitUp := prevChg >= l.LimitPct || (bars[idx-1].High-bars[idx-2].Close)/bars[idx-2].Close*100 >= l.LimitPct

	if isLimitUp {
		curOpen := bars[idx].Open
		prevClose := bars[idx-1].Close
		prevHigh := bars[idx-1].High
		avgVol := avgVolume(bars, idx-2, 10)

		isGapUp := curOpen > prevClose*1.005
		isVolumeUp := avgVol > 0 && bars[idx].Vol > avgVol*l.VolumeRatio
		isHoldingUp := bars[idx].Low > prevClose*0.98
		isNotOpenLimit := curOpen < prevHigh*1.05

		if isGapUp && isVolumeUp && isHoldingUp && isNotOpenLimit {
			return Buy
		}
	}

	prevDayChg := (bars[idx].Close/bars[idx-1].Close - 1) * 100
	if prevDayChg < l.LimitPct*0.5 && bars[idx].Close < bars[idx-1].Close {
		return Sell
	}

	return Hold
}

func (l *LimitUp) Score(bars []data.DailyBar, idx int) float64 {
	if idx < l.Warmup() {
		return 0
	}
	prevChg := (bars[idx-1].Close/bars[idx-2].Close - 1) * 100
	curMomentum := (bars[idx].Close/bars[idx].Open - 1) * 100
	volBonus := 0.0
	if avgVolume(bars, idx-2, 10) > 0 {
		volBonus = bars[idx].Vol / avgVolume(bars, idx-2, 10)
	}
	return prevChg*0.6 + curMomentum*0.3 + volBonus*0.1
}
