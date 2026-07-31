package strategy

import (
	"math"
	"quant/internal/data"
)

type BullFlag struct {
	MAPeriod     int
	VolumePeriod int
	VolumeRatio  float64
}

func NewBullFlag(maPeriod, volPeriod int, volRatio float64) *BullFlag {
	return &BullFlag{
		MAPeriod:     maPeriod,
		VolumePeriod: volPeriod,
		VolumeRatio:  volRatio,
	}
}

func (b *BullFlag) Name() string { return "bull_flag" }
func (b *BullFlag) Warmup() int {
	if b.VolumePeriod > b.MAPeriod {
		return b.VolumePeriod
	}
	return b.MAPeriod
}

func (b *BullFlag) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < b.Warmup() || idx < 1 {
		return Hold
	}

	ma10 := sma(bars, idx, b.MAPeriod)
	curClose := bars[idx].Close
	prevClose := bars[idx-1].Close
	ma10Prev := sma(bars, idx-1, b.MAPeriod)

	if ma10 <= 0 {
		return Hold
	}

	maxVol := maxVolume(bars, idx, b.VolumePeriod)
	curVol := bars[idx].Vol
	nearMA := math.Abs(curClose/ma10-1) <= 0.02
	volShrink := maxVol > 0 && curVol < maxVol*b.VolumeRatio
	aboveMA := curClose >= ma10 && prevClose >= ma10Prev

	if nearMA && volShrink && aboveMA {
		return Buy
	}

	if prevClose >= ma10Prev && curClose < ma10 {
		return Sell
	}
	if curClose < bars[idx-1].Low {
		return Sell
	}

	return Hold
}

func (b *BullFlag) Score(bars []data.DailyBar, idx int) float64 {
	if idx < b.Warmup() {
		return 0
	}
	maxVol := maxVolume(bars, idx, b.VolumePeriod)
	curVol := bars[idx].Vol
	ma10 := sma(bars, idx, b.MAPeriod)
	if maxVol <= 0 || ma10 <= 0 {
		return 0
	}
	distFromMA := (bars[idx].Close/ma10 - 1) * 100
	volScore := (1 - curVol/maxVol) * 10
	score := volScore - math.Abs(distFromMA)

	return score
}

func maxVolume(bars []data.DailyBar, idx, period int) float64 {
	start := idx - period + 1
	if start < 0 {
		start = 0
	}
	var maxV float64
	for i := start; i <= idx; i++ {
		if bars[i].Vol > maxV {
			maxV = bars[i].Vol
		}
	}
	return maxV
}
