package strategy

import (
	"math"
	"quant/internal/data"
)

// MASticky 均线粘合突破
// MA5/10/20/60 四条均线收敛(间距<2%) + 放量 + 突破 → 买入
// 均线发散向下 → 卖出
type MASticky struct {
	ConvergeThreshold float64 // 收敛阈值 2%
	VolumeRatio       float64 // 放量倍数 1.5
}

func NewMASticky(convergeThreshold, volumeRatio float64) *MASticky {
	return &MASticky{ConvergeThreshold: convergeThreshold, VolumeRatio: volumeRatio}
}

func (m *MASticky) Name() string { return "ma_sticky" }
func (m *MASticky) Warmup() int  { return 60 }

func (m *MASticky) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < m.Warmup() || idx < 1 {
		return Hold
	}

	ma5 := sma(bars, idx, 5)
	ma10 := sma(bars, idx, 10)
	ma20 := sma(bars, idx, 20)
	ma60 := sma(bars, idx, 60)

	prevMa5 := sma(bars, idx-1, 5)
	prevMa10 := sma(bars, idx-1, 10)
	prevMa20 := sma(bars, idx-1, 20)

	if ma5 <= 0 || ma60 <= 0 {
		return Hold
	}

	maxMA := math.Max(ma5, math.Max(ma10, math.Max(ma20, ma60)))
	minMA := math.Min(ma5, math.Min(ma10, math.Min(ma20, ma60)))

	spread := (maxMA - minMA) / ma60 * 100
	isConverged := spread < m.ConvergeThreshold

	avgVol := avgVolume(bars, idx-1, 20)
	isVolumeUp := avgVol > 0 && bars[idx].Vol > avgVol*m.VolumeRatio

	curPrice := bars[idx].Close
	brokeUp := curPrice > maxMA && bars[idx-1].Close <= maxMA

	prevConverged := false
	if idx > 1 {
		prevMax := math.Max(prevMa5, math.Max(prevMa10, math.Max(prevMa20, ma60)))
		prevMin := math.Min(prevMa5, math.Min(prevMa10, math.Min(prevMa20, ma60)))
		prevSpread := (prevMax - prevMin) / ma60 * 100
		prevConverged = prevSpread < m.ConvergeThreshold
	}

	if isConverged && isVolumeUp && brokeUp {
		return Buy
	}

	isDiverging := ma5 < ma10 && ma10 < ma20 && curPrice < ma5 && prevConverged
	if isDiverging {
		return Sell
	}

	return Hold
}

func (m *MASticky) Score(bars []data.DailyBar, idx int) float64 {
	if idx < m.Warmup() {
		return 0
	}
	ma5 := sma(bars, idx, 5)
	ma20 := sma(bars, idx, 20)
	ma60 := sma(bars, idx, 60)
	if ma60 <= 0 {
		return 0
	}
	spread := (math.Max(ma5, ma20) - math.Min(ma5, ma20)) / ma60 * 100
	return (1/spread - 0.5) * 10
}
