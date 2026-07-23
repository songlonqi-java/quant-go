package strategy

import "quant/internal/data"

// KDJ 随机指标
// K值上穿D值且在低位(<20) → 买入；K值下穿D值且在高位(>80) → 卖出
type KDJ struct {
	N      int     // 周期 9
	KLow   float64 // 超卖线 20
	DHigh  float64 // 超买线 80
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
	if idx < k.N {
		return 0, 0
	}
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
		return 0, 0
	}
	rsv := (bars[idx].Close - low) / (high - low) * 100
	prevK, prevD := 50.0, 50.0
	if idx >= k.N+1 {
		prevK, prevD = k.kdValue(bars, idx-1)
	}
	curK := prevK*2/3 + rsv/3
	curD := prevD*2/3 + curK/3
	return curK, curD
}
