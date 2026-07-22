package strategy

import "quant/internal/data"

type MACrossover struct {
	Short int
	Long  int
}

func NewMACrossover(short, long int) *MACrossover {
	return &MACrossover{Short: short, Long: long}
}

func (m *MACrossover) Name() string { return "ma_crossover" }
func (m *MACrossover) Warmup() int {
	if m.Long > m.Short {
		return m.Long
	}
	return m.Short
}

func (m *MACrossover) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < m.Warmup() || idx < 1 {
		return Hold
	}

	curShort := sma(bars, idx, m.Short)
	curLong := sma(bars, idx, m.Long)
	prevShort := sma(bars, idx-1, m.Short)
	prevLong := sma(bars, idx-1, m.Long)

	if curShort <= 0 || curLong <= 0 || prevShort <= 0 || prevLong <= 0 {
		return Hold
	}

	if prevShort <= prevLong && curShort > curLong {
		return Buy
	}
	if prevShort >= prevLong && curShort < curLong {
		return Sell
	}
	return Hold
}

func (m *MACrossover) Score(bars []data.DailyBar, idx int) float64 {
	if idx < m.Warmup() {
		return 0
	}
	curShort := sma(bars, idx, m.Short)
	curLong := sma(bars, idx, m.Long)
	if curLong <= 0 {
		return 0
	}
	return (curShort/curLong - 1) * 100
}

func sma(bars []data.DailyBar, idx, window int) float64 {
	if idx < window-1 {
		return 0
	}
	var sum float64
	for i := idx - window + 1; i <= idx; i++ {
		sum += bars[i].Close
	}
	return sum / float64(window)
}
