package signal

import (
	"fmt"
	"sort"

	"quant/internal/data"
	"quant/internal/execution"
)

type LiquidityContext struct {
	Policy          execution.LiquidityPolicy
	StockInfos      map[string]data.StockInfo
	Fundamentals    *data.FundamentalStore
	ReferenceEquity float64
	ApplyCurrentST  bool
}

// ApplyLiquidityPolicy enriches every buy signal with capacity metrics and
// adds auditable risk labels. Hard-filter semantics remain centralized in
// risk_policy.go.
func ApplyLiquidityPolicy(results []SignalResult, barsMap map[string][]data.DailyBar, context LiquidityContext) {
	if !context.Policy.Enabled {
		return
	}
	for i := range results {
		r := &results[i]
		if rawRecommendation(*r) != "买入" {
			continue
		}
		bars := barsMap[r.Code]
		idx := sort.Search(len(bars), func(idx int) bool { return bars[idx].TradeDate >= r.Date })
		if idx >= len(bars) || bars[idx].TradeDate != r.Date {
			idx = -1
		}
		info := context.StockInfos[r.Code]
		stockName := ""
		if context.ApplyCurrentST {
			stockName = r.Name
			if stockName == "" {
				stockName = info.Name
			}
		}
		turnover := 0.0
		hasTurnover := false
		if context.Fundamentals != nil {
			if basic := context.Fundamentals.GetDailyBasic(r.Code, r.Date); basic != nil {
				turnover = basic.TurnoverRate
				hasTurnover = true
			}
		}
		orderValue := 0.0
		if context.ReferenceEquity > 0 && r.PositionPct > 0 {
			orderValue = context.ReferenceEquity * r.PositionPct / 100
		}
		assessment := execution.AssessLiquidity(execution.LiquidityInput{
			Bars: bars, Index: idx, ListDate: info.ListDate, StockName: stockName,
			TurnoverRatePct: turnover, HasTurnover: hasTurnover, OrderValueCNY: orderValue,
		}, context.Policy)
		r.LiquidityModel = execution.LiquidityModelVersion
		r.LiquidityApplied = true
		r.LiquidityEligible = assessment.Eligible
		r.ListingDays = assessment.ListingDays
		r.AverageAmountCNY = assessment.AverageAmountCNY
		r.TurnoverRatePct = assessment.TurnoverRatePct
		r.HasTurnover = assessment.HasTurnover
		r.EstimatedOrderValueCNY = assessment.OrderValueCNY
		r.ParticipationPct = assessment.ParticipationPct
		r.EstimatedImpactPct = assessment.EstimatedImpactRate * 100
		for _, label := range assessment.HardLabels {
			addUnique(&r.RiskLabels, label)
		}
		for _, label := range assessment.NoticeLabels {
			addUnique(&r.RiskLabels, label)
		}
		addUnique(&r.Reasons, fmt.Sprintf("流动性: 日均成交额%.0f万,换手%s,预计占比%.2f%%,冲击%.3f%%",
			assessment.AverageAmountCNY/10_000, formatTurnover(assessment),
			assessment.ParticipationPct, assessment.EstimatedImpactRate*100))
	}
	ApplyRiskPolicy(results)
}

func formatTurnover(assessment execution.LiquidityAssessment) string {
	if !assessment.HasTurnover {
		return "未知"
	}
	return fmt.Sprintf("%.2f%%", assessment.TurnoverRatePct)
}
