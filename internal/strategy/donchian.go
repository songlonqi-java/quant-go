package strategy

import "quant/internal/data"

// Donchian Channel 唐奇安通道突破
// 价格突破N日最高价 → 买入；跌破N日最低价 → 卖出
type Donchian struct {
	Period int // 20
}

func NewDonchian(period int) *Donchian {
	return &Donchian{Period: period}
}

func (d *Donchian) Name() string { return "donchian" }
func (d *Donchian) Warmup() int  { return d.Period }

func (d *Donchian) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < d.Warmup() || idx < 1 {
		return Hold
	}
	upper, lower := d.channel(bars, idx)
	prevUpper, prevLower := d.channel(bars, idx-1)
	curClose := bars[idx].Close
	prevClose := bars[idx-1].Close

	if prevClose <= prevUpper && curClose > upper {
		return Buy
	}
	if prevClose >= prevLower && curClose < lower {
		return Sell
	}
	return Hold
}

func (d *Donchian) Score(bars []data.DailyBar, idx int) float64 {
	if idx < d.Warmup() {
		return 0
	}
	upper, lower := d.channel(bars, idx)
	if upper == lower {
		return 0
	}
	return (bars[idx].Close - lower) / (upper - lower) * 100 - 50
}

func (d *Donchian) channel(bars []data.DailyBar, idx int) (upper, lower float64) {
	start := idx - d.Period + 1
	if start < 0 {
		start = 0
	}
	upper = bars[start].High
	lower = bars[start].Low
	for i := start; i <= idx; i++ {
		if bars[i].High > upper {
			upper = bars[i].High
		}
		if bars[i].Low < lower {
			lower = bars[i].Low
		}
	}
	return
}
