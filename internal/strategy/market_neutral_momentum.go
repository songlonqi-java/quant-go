package strategy

import (
	"container/heap"
	"math"
	"sort"

	"quant/internal/data"
)

// MarketNeutralMomentum ranks stocks by beta-adjusted residual momentum. Its
// benchmark is an equal-weight proxy built from securities that traded on
// each historical market date, so every score uses only information available
// at that close and does not depend on today's index constituents.
type MarketNeutralMomentum struct {
	ShortPeriod int
	MidPeriod   int
	LongPeriod  int
	BetaPeriod  int
	MinBetaObs  int
	TopPct      float64

	latestDate         string
	selectedScores     map[string]float64
	historicalScores   map[string]map[string]float64
	historicalPrepared map[string]bool
}

func NewMarketNeutralMomentum(short, mid, long, betaPeriod, minBetaObs int, topPct float64) *MarketNeutralMomentum {
	return &MarketNeutralMomentum{
		ShortPeriod:    short,
		MidPeriod:      mid,
		LongPeriod:     long,
		BetaPeriod:     betaPeriod,
		MinBetaObs:     minBetaObs,
		TopPct:         topPct,
		selectedScores: make(map[string]float64),
	}
}

func (m *MarketNeutralMomentum) Name() string { return "market_neutral_momentum" }

func (m *MarketNeutralMomentum) Warmup() int {
	return maxInt(m.LongPeriod, m.BetaPeriod)
}

func (m *MarketNeutralMomentum) SetUniverse(barsMap map[string][]data.DailyBar) {
	universe := buildNeutralUniverse(barsMap)
	m.latestDate = ""
	m.selectedScores = make(map[string]float64)
	m.historicalScores = nil
	m.historicalPrepared = nil
	if len(universe.dates) == 0 {
		return
	}
	m.latestDate = universe.dates[len(universe.dates)-1]
	scores := make([]scored, 0, len(barsMap))
	for code, source := range barsMap {
		bars := sortedBars(source)
		if len(bars) == 0 || bars[len(bars)-1].TradeDate != m.latestDate {
			continue
		}
		series := neutralStockSeries(bars, universe)
		score, ok := m.scoreAt(series, len(bars)-1, universe)
		if ok {
			scores = append(scores, scored{code: code, score: score})
		}
	}
	m.selectedScores = selectPositiveScores(scores, m.TopPct)
}

func (m *MarketNeutralMomentum) SetHistoricalUniverse(barsMap map[string][]data.DailyBar) {
	universe := buildNeutralUniverse(barsMap)
	m.latestDate = ""
	m.selectedScores = make(map[string]float64)
	selectionLimit := topSelectionCount(len(barsMap), m.TopPct)
	byDate := make(map[string]*neutralTopAccumulator)
	for code, source := range barsMap {
		bars := sortedBars(source)
		series := neutralStockSeries(bars, universe)
		for idx := range bars {
			score, ok := m.scoreAt(series, idx, universe)
			if !ok {
				continue
			}
			date := bars[idx].TradeDate
			accumulator := byDate[date]
			if accumulator == nil {
				accumulator = &neutralTopAccumulator{limit: selectionLimit}
				byDate[date] = accumulator
			}
			accumulator.add(scored{code: code, score: score})
		}
	}

	m.historicalScores = make(map[string]map[string]float64, len(byDate))
	m.historicalPrepared = make(map[string]bool, len(byDate))
	for date, accumulator := range byDate {
		m.historicalPrepared[date] = true
		if selected := accumulator.selected(m.TopPct); len(selected) > 0 {
			m.historicalScores[date] = selected
		}
	}
}

func (m *MarketNeutralMomentum) Signal(bars []data.DailyBar, idx int) SignalType {
	if idx < m.Warmup() || idx >= len(bars) {
		return Hold
	}
	code := bars[idx].TsCode
	date := bars[idx].TradeDate
	score, selected, prepared := m.preparedScore(code, date)
	if !prepared {
		return Hold
	}
	ma60 := sma(bars, idx, 60)
	if ma60 > 0 && bars[idx].Close < ma60 {
		return Sell
	}
	if selected && score > 0 && ma60 > 0 && bars[idx].Close > ma60 {
		return Buy
	}
	return Hold
}

func (m *MarketNeutralMomentum) Score(bars []data.DailyBar, idx int) float64 {
	if idx < 0 || idx >= len(bars) {
		return 0
	}
	score, selected, _ := m.preparedScore(bars[idx].TsCode, bars[idx].TradeDate)
	if !selected {
		return 0
	}
	return score
}

func (m *MarketNeutralMomentum) preparedScore(code, date string) (float64, bool, bool) {
	if m.historicalPrepared[date] {
		score, selected := m.historicalScores[date][code]
		return score, selected, true
	}
	if date == m.latestDate {
		score, selected := m.selectedScores[code]
		return score, selected, true
	}
	return 0, false, false
}

func selectPositiveScores(scores []scored, topPct float64) map[string]float64 {
	selected := make(map[string]float64)
	if len(scores) == 0 || topPct <= 0 {
		return selected
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].score == scores[j].score {
			return scores[i].code < scores[j].code
		}
		return scores[i].score > scores[j].score
	})
	cutoff := topSelectionCount(len(scores), topPct)
	for i := 0; i < cutoff && i < len(scores); i++ {
		if scores[i].score > 0 {
			selected[scores[i].code] = scores[i].score
		}
	}
	return selected
}

func topSelectionCount(total int, topPct float64) int {
	if total <= 0 || topPct <= 0 {
		return 0
	}
	count := int(math.Ceil(float64(total) * topPct / 100))
	if count < 1 {
		return 1
	}
	return count
}

type neutralScoreHeap []scored

func (h neutralScoreHeap) Len() int { return len(h) }
func (h neutralScoreHeap) Less(i, j int) bool {
	if h[i].score == h[j].score {
		return h[i].code > h[j].code
	}
	return h[i].score < h[j].score
}
func (h neutralScoreHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *neutralScoreHeap) Push(value interface{}) {
	*h = append(*h, value.(scored))
}
func (h *neutralScoreHeap) Pop() interface{} {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

type neutralTopAccumulator struct {
	eligible int
	limit    int
	top      neutralScoreHeap
}

func (a *neutralTopAccumulator) add(score scored) {
	a.eligible++
	if a.limit <= 0 {
		return
	}
	if len(a.top) < a.limit {
		heap.Push(&a.top, score)
		return
	}
	worst := a.top[0]
	if score.score > worst.score || (score.score == worst.score && score.code < worst.code) {
		a.top[0] = score
		heap.Fix(&a.top, 0)
	}
}

func (a *neutralTopAccumulator) selected(topPct float64) map[string]float64 {
	scores := append([]scored(nil), a.top...)
	cutoff := topSelectionCount(a.eligible, topPct)
	if cutoff < len(scores) {
		sort.Slice(scores, func(i, j int) bool {
			if scores[i].score == scores[j].score {
				return scores[i].code < scores[j].code
			}
			return scores[i].score > scores[j].score
		})
		scores = scores[:cutoff]
	}
	return selectPositiveScores(scores, 100)
}

func (m *MarketNeutralMomentum) scoreAt(stock neutralSeries, barIdx int, universe neutralUniverse) (float64, bool) {
	if barIdx < 0 || barIdx >= len(stock.bars) {
		return 0, false
	}
	marketIdx, ok := universe.dateIndex[stock.bars[barIdx].TradeDate]
	if !ok || marketIdx < m.Warmup() {
		return 0, false
	}
	beta, ok := rollingMarketBeta(stock, marketIdx, m.BetaPeriod, m.MinBetaObs)
	if !ok {
		return 0, false
	}
	short, ok := residualMomentum(stock, marketIdx, m.ShortPeriod, beta, universe)
	if !ok {
		return 0, false
	}
	mid, ok := residualMomentum(stock, marketIdx, m.MidPeriod, beta, universe)
	if !ok {
		return 0, false
	}
	long, ok := residualMomentum(stock, marketIdx, m.LongPeriod, beta, universe)
	if !ok {
		return 0, false
	}
	return short*0.5 + mid*0.3 + long*0.2, true
}

type neutralUniverse struct {
	dates            []string
	dateIndex        map[string]int
	marketReturns    []float64
	marketReturnOK   []bool
	marketLogPrefix  []float64
	marketMissPrefix []int
}

type neutralSeries struct {
	bars          []data.DailyBar
	barByMarketID map[int]int
	betaCount     []int
	betaSumX      []float64
	betaSumY      []float64
	betaSumXX     []float64
	betaSumXY     []float64
}

func buildNeutralUniverse(barsMap map[string][]data.DailyBar) neutralUniverse {
	seen := make(map[string]bool)
	for _, bars := range barsMap {
		for _, bar := range bars {
			if bar.TradeDate != "" {
				seen[bar.TradeDate] = true
			}
		}
	}
	dates := make([]string, 0, len(seen))
	for date := range seen {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	dateIndex := make(map[string]int, len(dates))
	for i, date := range dates {
		dateIndex[date] = i
	}
	sums := make([]float64, len(dates))
	counts := make([]int, len(dates))
	for _, source := range barsMap {
		bars := sortedBars(source)
		for i := 1; i < len(bars); i++ {
			currentID, currentOK := dateIndex[bars[i].TradeDate]
			previousID, previousOK := dateIndex[bars[i-1].TradeDate]
			// Factor returns use adjusted closes. Raw execution prices contain
			// dividend/split gaps that are tradable-price facts but not momentum.
			previousClose := bars[i-1].Close
			currentClose := bars[i].Close
			if !currentOK || !previousOK || currentID != previousID+1 || previousClose <= 0 || currentClose <= 0 {
				continue
			}
			ret := currentClose/previousClose - 1
			if math.IsNaN(ret) || math.IsInf(ret, 0) || ret <= -1 {
				continue
			}
			sums[currentID] += ret
			counts[currentID]++
		}
	}
	returns := make([]float64, len(dates))
	returnOK := make([]bool, len(dates))
	logPrefix := make([]float64, len(dates)+1)
	missPrefix := make([]int, len(dates)+1)
	for i := range dates {
		if counts[i] > 0 {
			returns[i] = sums[i] / float64(counts[i])
			returnOK[i] = returns[i] > -1
		}
		logPrefix[i+1] = logPrefix[i]
		missPrefix[i+1] = missPrefix[i]
		if returnOK[i] {
			logPrefix[i+1] += math.Log1p(returns[i])
		} else {
			missPrefix[i+1]++
		}
	}
	return neutralUniverse{
		dates: dates, dateIndex: dateIndex, marketReturns: returns, marketReturnOK: returnOK,
		marketLogPrefix: logPrefix, marketMissPrefix: missPrefix,
	}
}

func neutralStockSeries(bars []data.DailyBar, universe neutralUniverse) neutralSeries {
	series := neutralSeries{
		bars: bars, barByMarketID: make(map[int]int, len(bars)),
		betaCount: make([]int, len(universe.dates)+1), betaSumX: make([]float64, len(universe.dates)+1),
		betaSumY: make([]float64, len(universe.dates)+1), betaSumXX: make([]float64, len(universe.dates)+1),
		betaSumXY: make([]float64, len(universe.dates)+1),
	}
	for i, bar := range bars {
		if marketID, ok := universe.dateIndex[bar.TradeDate]; ok {
			series.barByMarketID[marketID] = i
		}
	}
	for marketID := range universe.dates {
		to := marketID + 1
		series.betaCount[to] = series.betaCount[marketID]
		series.betaSumX[to] = series.betaSumX[marketID]
		series.betaSumY[to] = series.betaSumY[marketID]
		series.betaSumXX[to] = series.betaSumXX[marketID]
		series.betaSumXY[to] = series.betaSumXY[marketID]
		if marketID == 0 || !universe.marketReturnOK[marketID] {
			continue
		}
		currentBar, currentOK := series.barByMarketID[marketID]
		previousBar, previousOK := series.barByMarketID[marketID-1]
		if !currentOK || !previousOK {
			continue
		}
		previousClose := bars[previousBar].Close
		currentClose := bars[currentBar].Close
		if previousClose <= 0 || currentClose <= 0 {
			continue
		}
		x := universe.marketReturns[marketID]
		y := currentClose/previousClose - 1
		if math.IsNaN(y) || math.IsInf(y, 0) || y <= -1 {
			continue
		}
		series.betaCount[to]++
		series.betaSumX[to] += x
		series.betaSumY[to] += y
		series.betaSumXX[to] += x * x
		series.betaSumXY[to] += x * y
	}
	return series
}

func rollingMarketBeta(stock neutralSeries, marketIdx, period, minObs int) (float64, bool) {
	if period <= 1 || marketIdx <= 0 {
		return 0, false
	}
	if minObs <= 1 {
		minObs = period * 2 / 3
	}
	start := marketIdx - period + 1
	if start < 1 {
		start = 1
	}
	to := marketIdx + 1
	count := stock.betaCount[to] - stock.betaCount[start]
	if count < minObs {
		return 0, false
	}
	sumX := stock.betaSumX[to] - stock.betaSumX[start]
	sumY := stock.betaSumY[to] - stock.betaSumY[start]
	sumXX := stock.betaSumXX[to] - stock.betaSumXX[start]
	sumXY := stock.betaSumXY[to] - stock.betaSumXY[start]
	n := float64(count)
	variance := sumXX - sumX*sumX/n
	beta := 1.0
	if variance > 1e-12 {
		beta = (sumXY - sumX*sumY/n) / variance
	}
	if beta < -1 {
		beta = -1
	} else if beta > 3 {
		beta = 3
	}
	return beta, true
}

func residualMomentum(stock neutralSeries, marketIdx, period int, beta float64, universe neutralUniverse) (float64, bool) {
	if period <= 0 || marketIdx-period < 0 {
		return 0, false
	}
	currentBar, currentOK := stock.barByMarketID[marketIdx]
	startBar, startOK := stock.barByMarketID[marketIdx-period]
	if !currentOK || !startOK {
		return 0, false
	}
	startPrice := stock.bars[startBar].Close
	currentPrice := stock.bars[currentBar].Close
	if startPrice <= 0 || currentPrice <= 0 {
		return 0, false
	}
	from := marketIdx - period + 1
	to := marketIdx + 1
	if universe.marketMissPrefix[to]-universe.marketMissPrefix[from] != 0 {
		return 0, false
	}
	stockReturnPct := (currentPrice/startPrice - 1) * 100
	marketReturnPct := math.Expm1(universe.marketLogPrefix[to]-universe.marketLogPrefix[from]) * 100
	return stockReturnPct - beta*marketReturnPct, true
}
