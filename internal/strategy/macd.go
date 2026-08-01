package strategy

import (
	"sync"

	"quant/internal/data"
)

type MACD struct {
	Short        int
	Long         int
	SignalPeriod int

	cacheMu sync.RWMutex
	cache   map[macdSeriesKey][]macdPoint
}

type macdSeriesKey struct {
	first  *data.DailyBar
	length int
}

type macdPoint struct {
	dif  float64
	dea  float64
	hist float64
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
	point := m.cachedMACDSeries(bars)[idx]
	return point.dif, point.dea, point.hist
}

// cachedMACDSeries preserves the recursive EMA result for every prefix. The
// input bars are immutable during one signal/backtest run, so later lookups
// remain O(1) without changing any historical value.
func (m *MACD) cachedMACDSeries(bars []data.DailyBar) []macdPoint {
	if len(bars) == 0 {
		return nil
	}
	key := macdSeriesKey{first: &bars[0], length: len(bars)}
	m.cacheMu.RLock()
	series := m.cache[key]
	m.cacheMu.RUnlock()
	if series != nil {
		return series
	}

	series = make([]macdPoint, len(bars))

	shortAlpha := 2.0 / float64(m.Short+1)
	longAlpha := 2.0 / float64(m.Long+1)
	signalAlpha := 2.0 / float64(m.SignalPeriod+1)

	shortEMA := bars[0].Close
	longEMA := bars[0].Close
	dea := 0.0
	for i := range bars {
		if i > 0 {
			shortEMA = bars[i].Close*shortAlpha + shortEMA*(1-shortAlpha)
			longEMA = bars[i].Close*longAlpha + longEMA*(1-longAlpha)
		}
		dif := shortEMA - longEMA
		if i == 0 {
			dea = dif
		} else {
			dea = dif*signalAlpha + dea*(1-signalAlpha)
		}
		series[i] = macdPoint{dif: dif, dea: dea, hist: (dif - dea) * 2}
	}
	return m.storeMACDSeries(key, series)
}

func (m *MACD) storeMACDSeries(key macdSeriesKey, calculated []macdPoint) []macdPoint {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if m.cache == nil {
		m.cache = make(map[macdSeriesKey][]macdPoint)
	}
	if existing := m.cache[key]; existing != nil {
		return existing
	}
	m.cache[key] = calculated
	return calculated
}
