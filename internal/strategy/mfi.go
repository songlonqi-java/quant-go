package strategy

import "quant/internal/data"

// MFI 资金流量指标 (Money Flow Index)
// 成交量加权RSI，超卖区(20)回升 → 买入；超买区(80)回落 → 卖出
type MFI struct {
	Period     int     // 14
	Oversold   float64 // 20
	Overbought float64 // 80
}

func NewMFI(period int, oversold, overbought float64) *MFI {
	return &MFI{Period: period, Oversold: oversold, Overbought: overbought}
}

func (m *MFI) Name() string { return "mfi" }
func (m *MFI) Warmup() int  { return m.Period + 1 }

func (m *MFI) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < m.Warmup() || idx < 1 {
		return Hold
	}
	cur := mfiValue(bars, idx, m.Period)
	prev := mfiValue(bars, idx-1, m.Period)
	if prev <= m.Oversold && cur > m.Oversold {
		return Buy
	}
	if prev >= m.Overbought && cur < m.Overbought {
		return Sell
	}
	return Hold
}

func (m *MFI) Score(bars []data.DailyBar, idx int) float64 {
	if idx < m.Warmup() {
		return 0
	}
	return 50 - mfiValue(bars, idx, m.Period)
}

func mfiValue(bars []data.DailyBar, idx, period int) float64 {
	if idx < period {
		return 50
	}
	var posFlow, negFlow float64
	for i := idx - period + 1; i <= idx; i++ {
		typ := (bars[i].High + bars[i].Low + bars[i].Close) / 3
		prevTyp := (bars[i-1].High + bars[i-1].Low + bars[i-1].Close) / 3
		mf := typ * bars[i].Vol
		if typ > prevTyp {
			posFlow += mf
		} else {
			negFlow += mf
		}
	}
	if negFlow == 0 {
		return 100
	}
	mfr := posFlow / negFlow
	return 100 - (100 / (1 + mfr))
}
