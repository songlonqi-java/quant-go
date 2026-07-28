package strategy

import (
	"math"
	"quant/internal/data"
)

type Bollinger struct {
	Period     int
	Multiplier float64
}

func NewBollinger(period int, multiplier float64) *Bollinger {
	return &Bollinger{Period: period, Multiplier: multiplier}
}

func (b *Bollinger) Name() string { return "bollinger" }
func (b *Bollinger) Warmup() int  { return b.Period }

func (b *Bollinger) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < b.Warmup() || idx < 1 {
		return Hold
	}

	_, lowerCur, upperCur, _ := b.bandsAt(bars, idx)
	_, lowerPrev, upperPrev, _ := b.bandsAt(bars, idx-1)

	curPrice := bars[idx].Close
	prevPrice := bars[idx-1].Close

	if prevPrice <= lowerPrev && curPrice > lowerCur {
		return Buy
	}

	if prevPrice >= upperPrev && curPrice < upperCur {
		return Sell
	}
	return Hold
}

func (b *Bollinger) Score(bars []data.DailyBar, idx int) float64 {
	if idx < b.Warmup() {
		return 0
	}
	mid, lower, upper, _ := b.bandsAt(bars, idx)
	price := bars[idx].Close
	if upper == lower {
		return 0
	}
	return (mid - price) / (upper - lower) * 50
}

func (b *Bollinger) bandsAt(bars []data.DailyBar, idx int) (mid, lower, upper, width float64) {
	mid = sma(bars, idx, b.Period)
	if mid <= 0 {
		return 0, 0, 0, 0
	}

	var variance float64
	count := 0
	for i := idx - b.Period + 1; i <= idx; i++ {
		diff := bars[i].Close - mid
		variance += diff * diff
		count++
	}
	if count == 0 {
		return mid, mid, mid, 0
	}
	stddev := math.Sqrt(variance / float64(count))
	lower = mid - b.Multiplier*stddev
	upper = mid + b.Multiplier*stddev
	width = (upper - lower) / mid * 100
	return
}
