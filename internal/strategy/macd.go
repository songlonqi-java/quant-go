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
	_, _, hist := m.macdAt(bars, idx)
	return hist / bars[idx].Close * 100
}

func (m *MACD) macdAt(bars []data.DailyBar, idx int) (dif, dea, hist float64) {
	shortEMA := ema(bars, idx, m.Short)
	longEMA := ema(bars, idx, m.Long)
	dif = shortEMA - longEMA

	if idx < m.Long+m.SignalPeriod-1 {
		return dif, 0, 0
	}

	var difSum float64
	count := 0
	for i := idx - m.SignalPeriod + 1; i <= idx; i++ {
		s := ema(bars, i, m.Short)
		l := ema(bars, i, m.Long)
		difSum += s - l
		count++
	}
	if count > 0 {
		dea = difSum / float64(count)
	}
	hist = (dif - dea) * 2
	return
}

func ema(bars []data.DailyBar, idx, period int) float64 {
	if idx < period-1 {
		return 0
	}
	alpha := 2.0 / float64(period+1)

	var emaVal float64
	start := idx - period + 1
	for i := start; i <= idx; i++ {
		if i == start {
			emaVal = bars[i].Close
		} else {
			emaVal = bars[i].Close*alpha + emaVal*(1-alpha)
		}
	}
	return emaVal
}
