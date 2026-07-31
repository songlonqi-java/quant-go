package signal

import (
	"quant/internal/data"
	"quant/internal/market"
	"quant/internal/strategy"
)

// HistoricalEvaluator evaluates a stock at a known historical bar without
// looking past idx. Validation owns the chronological replay and then applies
// the same position policy that is used by the live workflow.
type HistoricalEvaluator struct {
	byHorizon map[strategy.Horizon][]strategy.Strategy
}

func NewHistoricalEvaluator(strategies []strategy.Strategy) *HistoricalEvaluator {
	return &HistoricalEvaluator{byHorizon: groupStrategiesByHorizon(strategies)}
}

// Evaluate returns one aggregated result per horizon that emitted at least one
// buy or sell signal at idx. The caller must pass only bars available as of idx.
func (e *HistoricalEvaluator) Evaluate(bars []data.DailyBar, idx int, name string, marketStatus *market.MarketStatus, moneyflows *data.MoneyflowStore) []SignalResult {
	if e == nil || idx < 0 || idx >= len(bars) {
		return nil
	}

	results := make([]SignalResult, 0, len(strategy.HorizonOrder()))
	for _, horizon := range strategy.HorizonOrder() {
		strategies := e.byHorizon[horizon]
		if len(strategies) == 0 {
			continue
		}
		r := SignalResult{
			Horizon:     horizon,
			Code:        bars[idx].TsCode,
			Name:        name,
			Date:        bars[idx].TradeDate,
			Close:       bars[idx].TradeClose(),
			Strategies:  make(map[string]SignalDetail),
			GroupScores: make(map[string]float64),
		}
		if marketStatus != nil {
			r.MarketSentiment = marketStatus.Sentiment
		}
		for _, s := range strategies {
			if idx < s.Warmup() {
				continue
			}
			sig := s.Signal(bars, idx)
			score := 0.0
			if sc, ok := s.(strategy.ScoreStrategy); ok {
				score = sc.Score(bars, idx)
			}
			r.Strategies[s.Name()] = SignalDetail{Signal: sig, Score: score}
			switch sig {
			case strategy.Buy:
				r.BuyCount++
				r.GroupScores[strategyGroup(s.Name())] += boundedContribution(score)
			case strategy.Sell:
				r.SellCount++
				r.GroupScores[strategyGroup(s.Name())] -= boundedContribution(score)
			}
		}
		if r.BuyCount == 0 && r.SellCount == 0 {
			continue
		}
		r.RawScore = cappedGroupScore(r.GroupScores)
		r.TotalScore = applyMarketAdjustment(r.RawScore, marketStatus)
		r.Confidence = confidenceScore(r)
		r.PositionPct = suggestedPositionPct(r, marketStatus)
		r.RiskLabels = riskLabels(r, bars, idx, marketStatus)
		r.Reasons = reasons(r)
		applyMoneyflowContext(&r, moneyflows)
		results = append(results, r)
	}
	return results
}
