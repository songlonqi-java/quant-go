package strategy

import "quant/internal/data"

type MACD struct {
	Short        int
	Long         int
	SignalPeriod int
}

func NewMACD(short, long, signalPeriod int) *MACD {
	return &MACD{Short: short, Long: long, SignalPeriod: signalPeriod}
}

func (m *MACD) Name() string { return "macd" }
func (m *MACD) Warmup() int  { return m.Long + m.SignalPeriod }

func (m *MACD) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < m.Warmup() || idx < 1 {
		return Hold
	}

	_, _, histCur := m.macdAt(bars, idx)
	_, _, histPrev := m.macdAt(bars, idx-1)

	if histPrev < 0 && histCur > 0 {
		return Buy
	}
	if histPrev > 0 && histCur < 0 {
		return Sell
	}
	return Hold
}

func (m *MACD) Score(bars []data.DailyBar, idx int) float64 {
	if idx < m.Warmup() {
		return 0
	}
	if bars[idx].Close <= 0 {
		return 0
	}
	_, _, hist := m.macdAt(bars, idx)
	return hist / bars[idx].Close * 100
}

func (m *MACD) macdAt(bars []data.DailyBar, idx int) (dif, dea, hist float64) {
	if len(bars) == 0 || idx < 0 {
		return 0, 0, 0
	}
	if idx >= len(bars) {
		idx = len(bars) - 1
	}

	shortAlpha := 2.0 / float64(m.Short+1)
	longAlpha := 2.0 / float64(m.Long+1)
	signalAlpha := 2.0 / float64(m.SignalPeriod+1)

	shortEMA := bars[0].Close
	longEMA := bars[0].Close
	dea = 0
	for i := 0; i <= idx; i++ {
		if i > 0 {
			shortEMA = bars[i].Close*shortAlpha + shortEMA*(1-shortAlpha)
			longEMA = bars[i].Close*longAlpha + longEMA*(1-longAlpha)
		}
		dif = shortEMA - longEMA
		if i == 0 {
			dea = dif
		} else {
			dea = dif*signalAlpha + dea*(1-signalAlpha)
		}
	}

	hist = (dif - dea) * 2
	return
}
