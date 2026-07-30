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
	if l.buySignalAt(bars, idx) {
		return Buy
	}
	// The strategy has no portfolio state. Limit exits to stocks whose entry
	// condition was confirmed on the preceding day, instead of emitting SELL
	// for every ordinary down day in the market.
	if idx >= 3 && l.buySignalAt(bars, idx-1) && !l.isLimitUpDay(bars, idx) {
		return Sell
	}
	return Hold
}

func (l *LimitUp) buySignalAt(bars []data.DailyBar, idx int) bool {
	if idx < l.Warmup() {
		return false
	}

	prevClose2 := bars[idx-2].TradeClose()
	prevClose := bars[idx-1].TradeClose()
	if prevClose2 <= 0 {
		return false
	}
	isLimitUp := l.isLimitUpDay(bars, idx-1)

	if isLimitUp {
		curOpen := bars[idx].TradeOpen()
		prevHigh := bars[idx-1].TradeHigh()
		avgVol := avgVolume(bars, idx-2, 10)

		isGapUp := curOpen > prevClose*1.005
		isVolumeUp := avgVol > 0 && bars[idx].Vol > avgVol*l.VolumeRatio
		isHoldingUp := bars[idx].TradeLow() > prevClose*0.98
		isNotOpenLimit := curOpen < prevHigh*1.05
		if bars[idx].HasLimitPrices() {
			isNotOpenLimit = !bars[idx].IsLimitUpOpen()
		}

		if isGapUp && isVolumeUp && isHoldingUp && isNotOpenLimit {
			return true
		}
	}
	return false
}

func (l *LimitUp) isLimitUpDay(bars []data.DailyBar, idx int) bool {
	if idx < 1 || idx >= len(bars) {
		return false
	}
	prevClose := bars[idx-1].TradeClose()
	if prevClose <= 0 {
		return false
	}
	bar := bars[idx]
	if bar.HasLimitPrices() {
		return bar.IsLimitUpClose() || bar.IsLimitUpPrice(bar.TradeHigh())
	}
	changePct := (bar.TradeClose()/prevClose - 1) * 100
	highPct := (bar.TradeHigh()/prevClose - 1) * 100
	return changePct >= l.LimitPct || highPct >= l.LimitPct
}

func (l *LimitUp) Score(bars []data.DailyBar, idx int) float64 {
	if idx < l.Warmup() {
		return 0
	}
	prevClose2 := bars[idx-2].TradeClose()
	if prevClose2 <= 0 || bars[idx].TradeOpen() <= 0 {
		return 0
	}
	prevChg := (bars[idx-1].TradeClose()/prevClose2 - 1) * 100
	curMomentum := (bars[idx].TradeClose()/bars[idx].TradeOpen() - 1) * 100
	volBonus := 0.0
	if avgVolume(bars, idx-2, 10) > 0 {
		volBonus = bars[idx].Vol / avgVolume(bars, idx-2, 10)
	}
	return prevChg*0.6 + curMomentum*0.3 + volBonus*0.1
}
