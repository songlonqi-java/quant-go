package strategy

import (
	"fmt"
	"sync"

	"quant/internal/data"
)

// KDJ 随机指标
// K值上穿D值且在低位(<20) → 买入；K值下穿D值且在高位(>80) → 卖出
type KDJ struct {
	N     int     // 周期 9
	KLow  float64 // 超卖线 20
	DHigh float64 // 超买线 80
	mu    sync.Mutex
	cache kdjCache
}

func NewKDJ(n int, kLow, dHigh float64) *KDJ {
	return &KDJ{N: n, KLow: kLow, DHigh: dHigh}
}

func (k *KDJ) Name() string { return "kdj" }
func (k *KDJ) Warmup() int  { return k.N + 1 }

func (k *KDJ) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < k.Warmup() || idx < 1 {
		return Hold
	}
	curK, curD := k.kdValue(bars, idx)
	prevK, prevD := k.kdValue(bars, idx-1)
	if curK <= 0 || curD <= 0 {
		return Hold
	}
	if prevK <= prevD && curK > curD && curK < k.KLow {
		return Buy
	}
	if prevK >= prevD && curK < curD && curK > k.DHigh {
		return Sell
	}
	return Hold
}

func (k *KDJ) Score(bars []data.DailyBar, idx int) float64 {
	if idx < k.Warmup() {
		return 0
	}
	curK, _ := k.kdValue(bars, idx)
	return 50 - curK
}

func (k *KDJ) kdValue(bars []data.DailyBar, idx int) (float64, float64) {
	if idx < k.N || idx < 0 || len(bars) == 0 {
		return 0, 0
	}
	if idx >= len(bars) {
		idx = len(bars) - 1
	}
	series := k.kdSeries(bars)
	if idx >= len(series) {
		return 0, 0
	}
	point := series[idx]
	return point.k, point.d
}

type kdjPoint struct {
	k float64
	d float64
}

type kdjCache struct {
	key    string
	series []kdjPoint
}

func (k *KDJ) kdSeries(bars []data.DailyBar) []kdjPoint {
	key := k.kdCacheKey(bars)

	k.mu.Lock()
	defer k.mu.Unlock()
	if k.cache.key == key && len(k.cache.series) == len(bars) {
		return k.cache.series
	}

	series := make([]kdjPoint, len(bars))
	prevK, prevD := 50.0, 50.0
	for idx := k.N; idx < len(bars); idx++ {
		high := bars[idx].High
		low := bars[idx].Low
		for i := idx - k.N + 1; i <= idx; i++ {
			if bars[i].High > high {
				high = bars[i].High
			}
			if bars[i].Low < low {
				low = bars[i].Low
			}
		}
		if high == low {
			prevK, prevD = 0, 0
			continue
		}
		rsv := (bars[idx].Close - low) / (high - low) * 100
		curK := prevK*2/3 + rsv/3
		curD := prevD*2/3 + curK/3
		series[idx] = kdjPoint{k: curK, d: curD}
		prevK, prevD = curK, curD
	}

	k.cache = kdjCache{key: key, series: series}
	return series
}

func (k *KDJ) kdCacheKey(bars []data.DailyBar) string {
	if len(bars) == 0 {
		return fmt.Sprintf("%d|empty", k.N)
	}
	first := &bars[0]
	last := bars[len(bars)-1]
	return fmt.Sprintf("%d|%p|%d|%s|%s", k.N, first, len(bars), first.TradeDate, last.TradeDate)
}
