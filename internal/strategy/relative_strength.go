package strategy

import (
	"math"
	"sort"

	"quant/internal/data"
)

// RelativeStrength 横截面相对强弱策略。
// 对全市场股票按 20/60/120 日动量加权排名，只选择前若干分位且位于 MA60 上方的强势股。
type RelativeStrength struct {
	ShortPeriod        int
	MidPeriod          int
	LongPeriod         int
	TopPct             float64
	ranks              map[string]float64
	selected           map[string]bool
	historicalRanks    map[string]map[string]float64
	historicalSelected map[string]map[string]bool
	latestDate         string
}

func NewRelativeStrength(short, mid, long int, topPct float64) *RelativeStrength {
	return &RelativeStrength{
		ShortPeriod: short,
		MidPeriod:   mid,
		LongPeriod:  long,
		TopPct:      topPct,
		ranks:       make(map[string]float64),
		selected:    make(map[string]bool),
	}
}

func (r *RelativeStrength) Name() string { return "relative_strength" }
func (r *RelativeStrength) Warmup() int {
	return maxInt(r.ShortPeriod, maxInt(r.MidPeriod, r.LongPeriod))
}

func (r *RelativeStrength) SetUniverse(barsMap map[string][]data.DailyBar) {
	var scores []scored
	r.ranks = make(map[string]float64)
	r.selected = make(map[string]bool)
	r.latestDate = ""

	for _, bars := range barsMap {
		if len(bars) == 0 {
			continue
		}
		last := bars[len(bars)-1].TradeDate
		if last > r.latestDate {
			r.latestDate = last
		}
	}
	for code, bars := range barsMap {
		if len(bars) <= r.Warmup() {
			continue
		}
		bars = sortedBars(bars)
		idx := len(bars) - 1
		if bars[idx].TradeDate != r.latestDate {
			continue
		}
		score := r.scoreAt(bars, idx)
		if score <= 0 {
			continue
		}
		scores = append(scores, scored{code: code, score: score})
	}
	r.ranks, r.selected = selectTop(scores, r.TopPct)
}

func (r *RelativeStrength) SetHistoricalUniverse(barsMap map[string][]data.DailyBar) {
	byDate := make(map[string][]scored)
	for code, bars := range barsMap {
		if len(bars) <= r.Warmup() {
			continue
		}
		bars = sortedBars(bars)
		for idx := r.Warmup(); idx < len(bars); idx++ {
			score := r.scoreAt(bars, idx)
			if score <= 0 {
				continue
			}
			date := bars[idx].TradeDate
			byDate[date] = append(byDate[date], scored{code: code, score: score})
		}
	}

	r.historicalRanks = make(map[string]map[string]float64, len(byDate))
	r.historicalSelected = make(map[string]map[string]bool, len(byDate))
	for date, scores := range byDate {
		r.historicalRanks[date], r.historicalSelected[date] = selectTop(scores, r.TopPct)
	}
}

func (r *RelativeStrength) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < r.Warmup() {
		return Hold
	}
	code := bars[idx].TsCode
	date := bars[idx].TradeDate
	ma60 := sma(bars, idx, 60)
	if selected, ok := r.historicalSelected[date]; ok {
		if ma60 > 0 && bars[idx].Close < ma60 {
			return Sell
		}
		if selected[code] && ma60 > 0 && bars[idx].Close > ma60 {
			return Buy
		}
		return Hold
	}
	if len(r.selected) > 0 && date == r.latestDate {
		if ma60 > 0 && bars[idx].Close < ma60 {
			return Sell
		}
		if r.selected[code] && ma60 > 0 && bars[idx].Close > ma60 {
			return Buy
		}
		return Hold
	}
	// 历史日期但没有横截面预计算（例如 analyze 报表路径漏调
	// SetHistoricalUniverse）：返回 Hold 而不是静默降级为单股动量，
	// 保证 live 与回放的信号语义一致，不会用非横截面信号冒充排名信号。
	return Hold
}

func (r *RelativeStrength) Score(bars []data.DailyBar, idx int) float64 {
	code := bars[idx].TsCode
	date := bars[idx].TradeDate
	if ranks, ok := r.historicalRanks[date]; ok {
		return ranks[code]
	}
	if date == r.latestDate {
		if score, ok := r.ranks[code]; ok {
			return score
		}
	}
	if idx < r.Warmup() {
		return 0
	}
	return r.scoreAt(bars, idx)
}

func (r *RelativeStrength) scoreAt(bars []data.DailyBar, idx int) float64 {
	retShort := pctReturn(bars, idx, r.ShortPeriod)
	retMid := pctReturn(bars, idx, r.MidPeriod)
	retLong := pctReturn(bars, idx, r.LongPeriod)
	ma60 := sma(bars, idx, 60)
	trendBonus := 0.0
	if ma60 > 0 && bars[idx].Close > ma60 {
		trendBonus = 10
	}
	return retShort*0.5 + retMid*0.3 + retLong*0.2 + trendBonus
}

func pctReturn(bars []data.DailyBar, idx, period int) float64 {
	if idx < period || bars[idx-period].Close <= 0 {
		return 0
	}
	return (bars[idx].Close/bars[idx-period].Close - 1) * 100
}

func maxInt(a, b int) int {
	return int(math.Max(float64(a), float64(b)))
}

type scored struct {
	code  string
	score float64
}

func sortedBars(bars []data.DailyBar) []data.DailyBar {
	cp := append([]data.DailyBar(nil), bars...)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].TradeDate == cp[j].TradeDate {
			return cp[i].TsCode < cp[j].TsCode
		}
		return cp[i].TradeDate < cp[j].TradeDate
	})
	return cp
}

func selectTop(scores []scored, topPct float64) (map[string]float64, map[string]bool) {
	ranks := make(map[string]float64, len(scores))
	selected := make(map[string]bool)
	if len(scores) == 0 || topPct <= 0 {
		return ranks, selected
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].score == scores[j].score {
			return scores[i].code < scores[j].code
		}
		return scores[i].score > scores[j].score
	})
	cutoff := int(math.Ceil(float64(len(scores)) * topPct / 100))
	if cutoff < 1 {
		cutoff = 1
	}
	for i, s := range scores {
		ranks[s.code] = s.score
		if i < cutoff {
			selected[s.code] = true
		}
	}
	return ranks, selected
}
