package strategy

import (
	"sync"

	"quant/internal/data"
)

type RSI struct {
	Period     int
	Oversold   float64
	Overbought float64

	cacheMu sync.RWMutex
	cache   map[rsiSeriesKey][]float64
}

type rsiSeriesKey struct {
	first  *data.DailyBar
	length int
}

func NewRSI(period int, oversold, overbought float64) *RSI {
	return &RSI{Period: period, Oversold: oversold, Overbought: overbought}
}

func (r *RSI) Name() string { return "rsi" }
func (r *RSI) Warmup() int  { return r.Period + 1 }

func (r *RSI) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < r.Warmup() || idx < 1 {
		return Hold
	}

	cur := r.value(bars, idx)
	prev := r.value(bars, idx-1)

	if prev <= r.Oversold && cur > r.Oversold {
		return Buy
	}
	if prev >= r.Overbought && cur < r.Overbought {
		return Sell
	}
	return Hold
}

func (r *RSI) Score(bars []data.DailyBar, idx int) float64 {
	if idx < r.Warmup() {
		return 0
	}
	val := r.value(bars, idx)
	return (50 - val)
}

func (r *RSI) value(bars []data.DailyBar, idx int) float64 {
	if idx < 0 || len(bars) == 0 {
		return 50
	}
	if idx >= len(bars) {
		idx = len(bars) - 1
	}
	return r.cachedSeries(bars)[idx]
}

func (r *RSI) cachedSeries(bars []data.DailyBar) []float64 {
	key := rsiSeriesKey{first: &bars[0], length: len(bars)}
	r.cacheMu.RLock()
	series := r.cache[key]
	r.cacheMu.RUnlock()
	if series != nil {
		return series
	}
	series = calculateRSISeries(bars, r.Period)
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if r.cache == nil {
		r.cache = make(map[rsiSeriesKey][]float64)
	}
	if existing := r.cache[key]; existing != nil {
		return existing
	}
	r.cache[key] = series
	return series
}

func rsiValue(bars []data.DailyBar, idx, period int) float64 {
	if idx < period {
		return 50
	}
	if idx >= len(bars) {
		idx = len(bars) - 1
	}
	if idx < period || len(bars) == 0 {
		return 50
	}
	return calculateRSISeries(bars[:idx+1], period)[idx]
}

func calculateRSISeries(bars []data.DailyBar, period int) []float64 {
	series := make([]float64, len(bars))
	for i := range series {
		series[i] = 50
	}
	if period <= 0 || len(bars) <= period {
		return series
	}

	var avgGain, avgLoss float64
	for i := 1; i <= period; i++ {
		chg := bars[i].Close - bars[i-1].Close
		if chg > 0 {
			avgGain += chg
		} else {
			avgLoss += -chg
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	series[period] = rsiFromAverages(avgGain, avgLoss)

	for i := period + 1; i < len(bars); i++ {
		chg := bars[i].Close - bars[i-1].Close
		gain := 0.0
		loss := 0.0
		if chg > 0 {
			gain = chg
		} else {
			loss = -chg
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
		series[i] = rsiFromAverages(avgGain, avgLoss)
	}
	return series
}

func rsiFromAverages(avgGain, avgLoss float64) float64 {
	if avgLoss == 0 {
		if avgGain == 0 {
			return 50
		}
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}
