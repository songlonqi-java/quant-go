package strategy

import "quant/internal/data"

// Parabolic SAR 抛物线止损反转
// SAR从上方翻到下方 → 买入；SAR从下方翻到上方 → 卖出
type ParabolicSAR struct {
	AFStep   float64 // 加速因子步长 0.02
	AFMax    float64 // 最大加速因子 0.2
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

	for i := 2; i <= idx; i++ {
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
	}
	return sar
}
