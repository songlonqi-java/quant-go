package strategy

import (
	"sync"

	"quant/internal/data"
)

// Parabolic SAR 抛物线止损反转
// SAR从上方翻到下方 → 买入；SAR从下方翻到上方 → 卖出
type ParabolicSAR struct {
	AFStep float64 // 加速因子步长 0.02
	AFMax  float64 // 最大加速因子 0.2

	cacheMu sync.RWMutex
	cache   map[sarSeriesKey][]float64
}

type sarSeriesKey struct {
	first  *data.DailyBar
	length int
}

func NewParabolicSAR(afStep, afMax float64) *ParabolicSAR {
	return &ParabolicSAR{AFStep: afStep, AFMax: afMax}
}

func (p *ParabolicSAR) Name() string { return "sar" }
func (p *ParabolicSAR) Warmup() int  { return 5 }

func (p *ParabolicSAR) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < p.Warmup() || idx < 1 {
		return Hold
	}
	curSAR := p.calcSAR(bars, idx)
	prevSAR := p.calcSAR(bars, idx-1)
	curClose := bars[idx].Close
	prevClose := bars[idx-1].Close

	if curSAR <= 0 || prevSAR <= 0 {
		return Hold
	}

	if prevClose <= prevSAR && curClose > curSAR {
		return Buy
	}
	if prevClose >= prevSAR && curClose < curSAR {
		return Sell
	}
	return Hold
}

func (p *ParabolicSAR) Score(bars []data.DailyBar, idx int) float64 {
	if idx < p.Warmup() {
		return 0
	}
	sar := p.calcSAR(bars, idx)
	if sar <= 0 {
		return 0
	}
	return (bars[idx].Close/sar - 1) * 100
}

func (p *ParabolicSAR) calcSAR(bars []data.DailyBar, idx int) float64 {
	if idx < 2 {
		return 0
	}
	if idx >= len(bars) {
		idx = len(bars) - 1
	}
	if idx < 2 {
		return 0
	}
	series := p.cachedSARSeries(bars)
	return series[idx]
}

// cachedSARSeries calculates every prefix once. Historical portfolio replay
// asks for the same immutable stock series at consecutive indexes; rebuilding
// from day zero for every Signal and Score call makes that replay quadratic.
func (p *ParabolicSAR) cachedSARSeries(bars []data.DailyBar) []float64 {
	if len(bars) == 0 {
		return nil
	}
	key := sarSeriesKey{first: &bars[0], length: len(bars)}
	p.cacheMu.RLock()
	series := p.cache[key]
	p.cacheMu.RUnlock()
	if series != nil {
		return series
	}

	series = make([]float64, len(bars))
	if len(bars) < 3 {
		return p.storeSARSeries(key, series)
	}

	var sar, ep, af float64
	var isLong bool

	if bars[1].Close > bars[0].Close {
		isLong = true
		sar = bars[0].Low
		ep = bars[1].High
	} else {
		isLong = false
		sar = bars[0].High
		ep = bars[1].Low
	}
	af = p.AFStep

	for i := 2; i < len(bars); i++ {
		if isLong {
			if bars[i].Low < sar {
				isLong = false
				af = p.AFStep
				sar = ep
				ep = bars[i].Low
			} else {
				if bars[i].High > ep {
					ep = bars[i].High
					af += p.AFStep
					if af > p.AFMax {
						af = p.AFMax
					}
				}
			}
		} else {
			if bars[i].High > sar {
				isLong = true
				af = p.AFStep
				sar = ep
				ep = bars[i].High
			} else {
				if bars[i].Low < ep {
					ep = bars[i].Low
					af += p.AFStep
					if af > p.AFMax {
						af = p.AFMax
					}
				}
			}
		}

		if isLong {
			sar = sar + af*(ep-sar)
			if sar > bars[i].Low {
				sar = bars[i].Low
			}
			if i > 1 && sar > bars[i-1].Low {
				sar = bars[i-1].Low
			}
		} else {
			sar = sar + af*(ep-sar)
			if sar < bars[i].High {
				sar = bars[i].High
			}
			if i > 1 && sar < bars[i-1].High {
				sar = bars[i-1].High
			}
		}
		series[i] = sar
	}
	return p.storeSARSeries(key, series)
}

func (p *ParabolicSAR) storeSARSeries(key sarSeriesKey, calculated []float64) []float64 {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	if p.cache == nil {
		p.cache = make(map[sarSeriesKey][]float64)
	}
	if existing := p.cache[key]; existing != nil {
		return existing
	}
	p.cache[key] = calculated
	return calculated
}
