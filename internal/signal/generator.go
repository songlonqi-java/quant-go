package signal

import (
	"fmt"
	"sort"
	"strings"

	"quant/internal/data"
	"quant/internal/strategy"
)

type SignalResult struct {
	Code      string
	Name      string
	Date      string
	Close     float64
	Strategies map[string]SignalDetail
	BuyCount  int
	SellCount int
	TotalScore float64
}

type SignalDetail struct {
	Signal strategy.SignalType
	Score  float64
}

func Generate(barsMap map[string][]data.DailyBar, strategies []strategy.Strategy, topN int, names map[string]string) []SignalResult {
	var results []SignalResult

	for code, bars := range barsMap {
		if len(bars) < 2 {
			continue
		}
		lastIdx := len(bars) - 1

		name := code
		if n, ok := names[code]; ok && n != "" {
			name = n
		}

		r := SignalResult{
			Code:       code,
			Name:       name,
			Date:       bars[lastIdx].TradeDate,
			Close:      bars[lastIdx].Close,
			Strategies: make(map[string]SignalDetail),
		}

		for _, s := range strategies {
			if lastIdx < s.Warmup() {
				continue
			}
			sig := s.Signal(bars, lastIdx)
			var score float64
			if sc, ok := s.(strategy.ScoreStrategy); ok {
				score = sc.Score(bars, lastIdx)
			}
			r.Strategies[s.Name()] = SignalDetail{Signal: sig, Score: score}

			if sig == strategy.Buy {
				r.BuyCount++
				r.TotalScore += 1 + score*0.01
			} else if sig == strategy.Sell {
				r.SellCount++
				r.TotalScore -= 1 + score*0.01
			}
		}

		if r.BuyCount > 0 || r.SellCount > 0 {
			results = append(results, r)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].TotalScore > results[j].TotalScore
	})

	if topN > 0 && len(results) > topN {
		return results[:topN]
	}
	return results
}

func (r SignalResult) SignalSummary() string {
	parts := []string{}
	if r.BuyCount > 0 {
		parts = append(parts, fmt.Sprintf("买入×%d", r.BuyCount))
	}
	if r.SellCount > 0 {
		parts = append(parts, fmt.Sprintf("卖出×%d", r.SellCount))
	}
	if len(parts) == 0 {
		return "观望"
	}
	return strings.Join(parts, " ")
}

func (r SignalResult) Recommendation() string {
	if r.BuyCount > r.SellCount && r.TotalScore > 0 {
		return "买入"
	} else if r.SellCount > r.BuyCount && r.TotalScore < 0 {
		return "卖出"
	}
	return "观望"
}
