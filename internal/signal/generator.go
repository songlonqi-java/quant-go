package signal

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"quant/internal/data"
	"quant/internal/market"
	"quant/internal/strategy"
)

type SignalResult struct {
	Code        string
	Name        string
	Date        string
	Close       float64
	Strategies  map[string]SignalDetail
	GroupScores map[string]float64
	BuyCount    int
	SellCount   int
	RawScore    float64
	TotalScore  float64
	Confidence  float64
	PositionPct float64
	RiskLabels  []string
	Reasons     []string
}

type SignalDetail struct {
	Signal strategy.SignalType
	Score  float64
}

func Generate(barsMap map[string][]data.DailyBar, strategies []strategy.Strategy, topN int, names map[string]string) []SignalResult {
	return GenerateWithContext(barsMap, strategies, topN, names, nil)
}

func GenerateWithContext(barsMap map[string][]data.DailyBar, strategies []strategy.Strategy, topN int, names map[string]string, marketStatus *market.MarketStatus) []SignalResult {
	var results []SignalResult
	for _, s := range strategies {
		if u, ok := s.(strategy.UniverseUser); ok {
			u.SetUniverse(barsMap)
		}
	}

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
			Code:        code,
			Name:        name,
			Date:        bars[lastIdx].TradeDate,
			Close:       bars[lastIdx].TradeClose(),
			Strategies:  make(map[string]SignalDetail),
			GroupScores: make(map[string]float64),
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
				r.GroupScores[strategyGroup(s.Name())] += boundedContribution(score)
			} else if sig == strategy.Sell {
				r.SellCount++
				r.GroupScores[strategyGroup(s.Name())] -= boundedContribution(score)
			}
		}

		if r.BuyCount > 0 || r.SellCount > 0 {
			r.RawScore = cappedGroupScore(r.GroupScores)
			r.TotalScore = applyMarketAdjustment(r.RawScore, marketStatus)
			r.Confidence = confidenceScore(r)
			r.PositionPct = suggestedPositionPct(r, marketStatus)
			r.RiskLabels = riskLabels(r, bars, lastIdx, marketStatus)
			r.Reasons = reasons(r)
			results = append(results, r)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].TotalScore == results[j].TotalScore {
			return results[i].Code < results[j].Code
		}
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

func strategyGroup(name string) string {
	switch name {
	case "ma_crossover", "macd", "sar", "roc", "donchian", "etf_rotation", "relative_strength":
		return "trend"
	case "volume_breakout", "limit_up", "ma_sticky", "atr_breakout":
		return "volume"
	case "rsi", "kdj", "williams_r", "mfi", "bollinger", "bottom_reversal":
		return "reversal"
	case "value_ma60", "dividend_deviation":
		return "value"
	case "bull_flag", "trend_pullback":
		return "pattern"
	default:
		return "other"
	}
}

func groupCap(group string) float64 {
	switch group {
	case "trend":
		return 2.2
	case "volume":
		return 1.5
	case "reversal":
		return 1.5
	case "value":
		return 1.2
	case "pattern":
		return 1.2
	default:
		return 1.0
	}
}

func boundedContribution(score float64) float64 {
	score = math.Max(-30, math.Min(30, score))
	contribution := 1 + score*0.01
	if contribution < 0.5 {
		return 0.5
	}
	if contribution > 1.3 {
		return 1.3
	}
	return contribution
}

func cappedGroupScore(groupScores map[string]float64) float64 {
	var total float64
	for group, score := range groupScores {
		cap := groupCap(group)
		if score > cap {
			score = cap
		}
		if score < -cap {
			score = -cap
		}
		total += score
	}
	return total
}

func applyMarketAdjustment(score float64, marketStatus *market.MarketStatus) float64 {
	if marketStatus == nil || score <= 0 {
		return score
	}
	switch {
	case strings.Contains(marketStatus.Sentiment, "强烈看空"):
		return score * 0.4
	case strings.Contains(marketStatus.Sentiment, "偏空"):
		return score * 0.65
	case strings.Contains(marketStatus.Sentiment, "中性"):
		return score * 0.85
	case strings.Contains(marketStatus.Sentiment, "强烈看多"):
		return score * 1.15
	default:
		return score
	}
}

func confidenceScore(r SignalResult) float64 {
	conf := 45 + r.TotalScore*10 + float64(r.BuyCount-r.SellCount)*3
	if r.SellCount > 0 {
		conf -= float64(r.SellCount) * 15
	}
	if conf < 0 {
		return 0
	}
	if conf > 95 {
		return 95
	}
	return conf
}

func suggestedPositionPct(r SignalResult, marketStatus *market.MarketStatus) float64 {
	if r.Recommendation() != "买入" {
		return 0
	}
	pos := 3 + r.Confidence/15
	cap := 10.0
	if marketStatus != nil {
		switch {
		case strings.Contains(marketStatus.Sentiment, "强烈看空"):
			cap = 2
		case strings.Contains(marketStatus.Sentiment, "偏空"):
			cap = 5
		case strings.Contains(marketStatus.Sentiment, "中性"):
			cap = 7
		case strings.Contains(marketStatus.Sentiment, "强烈看多"):
			cap = 15
		}
	}
	if pos > cap {
		return cap
	}
	return pos
}

func riskLabels(r SignalResult, bars []data.DailyBar, idx int, marketStatus *market.MarketStatus) []string {
	labels := make([]string, 0, 4)
	if r.SellCount > 0 {
		labels = append(labels, "有卖出冲突")
	}
	if r.BuyCount <= 2 {
		labels = append(labels, "信号较少")
	}
	if idx >= 5 && bars[idx-5].Close > 0 {
		chg5 := (bars[idx].Close/bars[idx-5].Close - 1) * 100
		if chg5 > 25 {
			labels = append(labels, "5日涨幅过高")
		}
	}
	if idx >= 20 && bars[idx-20].Close > 0 {
		chg20 := (bars[idx].Close/bars[idx-20].Close - 1) * 100
		if chg20 > 40 {
			labels = append(labels, "20日涨幅过高")
		}
	}
	if marketStatus != nil && (strings.Contains(marketStatus.Sentiment, "偏空") || strings.Contains(marketStatus.Sentiment, "强烈看空")) {
		labels = append(labels, "市场偏弱")
	}
	return labels
}

func reasons(r SignalResult) []string {
	var buyNames []string
	var sellNames []string
	for name, detail := range r.Strategies {
		switch detail.Signal {
		case strategy.Buy:
			buyNames = append(buyNames, name)
		case strategy.Sell:
			sellNames = append(sellNames, name)
		}
	}
	sort.Strings(buyNames)
	sort.Strings(sellNames)

	var out []string
	if len(buyNames) > 0 {
		out = append(out, "买入: "+strings.Join(buyNames, ","))
	}
	if len(sellNames) > 0 {
		out = append(out, "卖出冲突: "+strings.Join(sellNames, ","))
	}
	return out
}
