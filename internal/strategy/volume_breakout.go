package strategy

import "quant/internal/data"

type VolumeBreakout struct {
	Period          int
	VolumeThreshold float64
}

func NewVolumeBreakout(period int, threshold float64) *VolumeBreakout {
	return &VolumeBreakout{Period: period, VolumeThreshold: threshold}
}

func (v *VolumeBreakout) Name() string { return "volume_breakout" }
func (v *VolumeBreakout) Warmup() int  { return v.Period + 1 }

func (v *VolumeBreakout) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < v.Warmup() || idx < 1 {
		return Hold
	}

	avgVol := avgVolume(bars, idx-1, v.Period)
	curVol := bars[idx].Vol
	curClose := bars[idx].Close
	prevClose := bars[idx-1].Close
	ma20 := sma(bars, idx, 20)

	if avgVol <= 0 {
		return Hold
	}

	volRatio := curVol / avgVol

	if volRatio > v.VolumeThreshold && curClose > prevClose && (ma20 <= 0 || curClose > ma20) {
		return Buy
	}
	if volRatio > v.VolumeThreshold && curClose < prevClose {
		return Sell
	}
	return Hold
}

func (v *VolumeBreakout) Score(bars []data.DailyBar, idx int) float64 {
	if idx < v.Warmup() {
		return 0
	}
	avgVol := avgVolume(bars, idx, v.Period)
	if avgVol <= 0 {
		return 0
	}
	volRatio := bars[idx].Vol / avgVol
	priceChg := (bars[idx].Close/bars[idx-1].Close - 1) * 100
	return volRatio * priceChg
}

func avgVolume(bars []data.DailyBar, idx, period int) float64 {
	if idx < period-1 {
		return 0
	}
	var sum float64
	count := 0
	for i := idx - period + 1; i <= idx; i++ {
		sum += bars[i].Vol
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}
