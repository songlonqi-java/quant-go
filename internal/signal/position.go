package signal

import (
	"fmt"
	"strings"

	"quant/internal/market"
	"quant/internal/strategy"
)

type PositionAction string

const (
	PositionActionCash   PositionAction = "空仓"
	PositionActionWatch  PositionAction = "观望"
	PositionActionProbe  PositionAction = "轻仓试错"
	PositionActionActive PositionAction = "正常买入"
)

type PositionDecision struct {
	Action         PositionAction
	CandidateBuys  int
	QualifiedBuys  int
	SuppressedBuys int
	Reasons        []string
	Advice         string
}

func ApplyPositionPolicy(results []SignalResult, marketStatus *market.MarketStatus) PositionDecision {
	ApplyRiskPolicy(results)
	decision := EvaluatePositionPolicy(results, marketStatus)
	suppressAllBuys := decision.Action == PositionActionCash || decision.Action == PositionActionWatch

	for i := range results {
		if rawRecommendation(results[i]) != "买入" {
			continue
		}
		issues := qualifyingBuyIssues(results[i])
		if suppressAllBuys || len(issues) > 0 {
			results[i].Suppressed = true
			results[i].SuppressionReason = suppressionReason(decision, issues)
			results[i].PositionPct = 0
			addUnique(&results[i].RiskLabels, "空仓过滤")
			if results[i].SuppressionReason != "" {
				results[i].Reasons = append(results[i].Reasons, "仓位策略: "+results[i].SuppressionReason)
			}
			continue
		}
		if decision.Action == PositionActionProbe && results[i].PositionPct > 3 {
			results[i].PositionPct = 3
			addUnique(&results[i].RiskLabels, "轻仓试错")
		}
	}
	RefreshRiskPolicy(results)

	return decision
}

func EvaluatePositionPolicy(results []SignalResult, marketStatus *market.MarketStatus) PositionDecision {
	decision := PositionDecision{}
	sentiment := ""
	if marketStatus != nil {
		sentiment = marketStatus.Sentiment
	}

	var qualifiedWithMoneyflow int
	for _, r := range results {
		if rawRecommendation(r) != "买入" {
			continue
		}
		decision.CandidateBuys++
		if issues := qualifyingBuyIssues(r); len(issues) == 0 {
			decision.QualifiedBuys++
			if hasMoneyflowConfirmation(r) {
				qualifiedWithMoneyflow++
			}
		}
	}
	decision.SuppressedBuys = decision.CandidateBuys - decision.QualifiedBuys

	if sentiment == "" {
		decision.Reasons = append(decision.Reasons, "市场情绪未知")
	} else if isStrongBearish(sentiment) || isBearish(sentiment) {
		decision.Reasons = append(decision.Reasons, "市场情绪"+sentiment)
	}
	if decision.CandidateBuys == 0 {
		decision.Reasons = append(decision.Reasons, "没有买入候选")
	} else if decision.QualifiedBuys == 0 {
		decision.Reasons = append(decision.Reasons, "没有通过风控的买入候选")
	} else if decision.SuppressedBuys > 0 {
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("%d个买入候选被盘中/风控过滤", decision.SuppressedBuys))
	}
	if decision.QualifiedBuys > 0 && qualifiedWithMoneyflow == 0 {
		decision.Reasons = append(decision.Reasons, "合格候选缺少资金确认")
	}

	switch {
	case isStrongBearish(sentiment):
		decision.Action = PositionActionCash
	case decision.CandidateBuys == 0 && !isBearish(sentiment) && !isNeutral(sentiment):
		decision.Action = PositionActionWatch
	case decision.CandidateBuys == 0:
		decision.Action = PositionActionCash
	case decision.QualifiedBuys == 0 && !isBearish(sentiment) && !isNeutral(sentiment):
		decision.Action = PositionActionWatch
	case decision.QualifiedBuys == 0:
		decision.Action = PositionActionCash
	case isBearish(sentiment) && qualifiedWithMoneyflow == 0:
		decision.Action = PositionActionCash
	case isBearish(sentiment):
		decision.Action = PositionActionProbe
	case isNeutral(sentiment) && decision.QualifiedBuys <= 1 && qualifiedWithMoneyflow == 0:
		decision.Action = PositionActionCash
	case isNeutral(sentiment):
		decision.Action = PositionActionProbe
	default:
		decision.Action = PositionActionActive
	}
	decision.Advice = adviceForAction(decision.Action)
	return decision
}

func PrintPositionDecision(decision PositionDecision) {
	fmt.Println("\n========== 仓位策略 ==========")
	fmt.Printf("策略状态: %s\n", decision.Action)
	fmt.Printf("买入候选: %d, 合格候选: %d, 过滤: %d\n", decision.CandidateBuys, decision.QualifiedBuys, decision.SuppressedBuys)
	if len(decision.Reasons) > 0 {
		fmt.Printf("触发原因: %s\n", strings.Join(decision.Reasons, " / "))
	}
	fmt.Printf("执行建议: %s\n", decision.Advice)
	fmt.Println("==============================")
}

func qualifyingBuyIssues(r SignalResult) []string {
	var issues []string
	if r.SellCount > 0 {
		issues = append(issues, "有卖出冲突")
	}
	if r.VoteMetricsApplied {
		requiredGroups, requiredVotes := requiredIndependentConsensus(r.Horizon)
		if r.BuyGroupCount < requiredGroups {
			issues = append(issues, fmt.Sprintf("独立策略组不足(%d/%d)", r.BuyGroupCount, requiredGroups))
		}
		if r.EffectiveBuyVotes+1e-9 < requiredVotes {
			issues = append(issues, fmt.Sprintf("相关性调整后买入票数不足(%.2f/%.2f)", r.EffectiveBuyVotes, requiredVotes))
		}
	} else if r.BuyCount < minBuySignals(r.Horizon) {
		issues = append(issues, "买入信号不足")
	}
	if r.Confidence < 70 {
		issues = append(issues, "置信度不足")
	}
	if r.HistoricalEvidence != nil && r.HistoricalEvidence.Enforced && !r.HistoricalEvidence.Eligible {
		issues = append(issues, "历史验证不通过")
	}
	if r.LiquidityApplied && !r.LiquidityEligible {
		issues = append(issues, "流动性资格不通过")
	}
	issues = append(issues, hardRiskIssues(r)...)
	return uniqueStrings(issues)
}

func hasMoneyflowConfirmation(r SignalResult) bool {
	if !r.HasMoneyflow {
		return false
	}
	return r.MoneyflowNetAmount > 0 && r.LargeMoneyflowNetAmount > 0
}

func minBuySignals(h strategy.Horizon) int {
	switch h {
	case strategy.HorizonShort:
		return 3
	case strategy.HorizonMid:
		return 2
	case strategy.HorizonLong:
		return 1
	default:
		return 2
	}
}

func suppressionReason(decision PositionDecision, issues []string) string {
	if len(issues) > 0 {
		return strings.Join(issues, ",")
	}
	if len(decision.Reasons) > 0 {
		return strings.Join(decision.Reasons, ",")
	}
	return string(decision.Action)
}

func adviceForAction(action PositionAction) string {
	switch action {
	case PositionActionCash:
		return "不新增仓位，只处理持仓止盈止损；等待市场转强、资金确认或盘中风险解除"
	case PositionActionWatch:
		return "不强行买入，继续观察候选质量和市场方向"
	case PositionActionProbe:
		return "只允许轻仓低吸，单票仓位不超过3%，不追高"
	case PositionActionActive:
		return "可按信号建议仓位执行，但仍需避开高开、涨停和盘中走弱"
	default:
		return "维持观望"
	}
}

func isStrongBearish(sentiment string) bool {
	return strings.Contains(sentiment, "强烈看空")
}

func isBearish(sentiment string) bool {
	return strings.Contains(sentiment, "偏空") || isStrongBearish(sentiment)
}

func isNeutral(sentiment string) bool {
	return sentiment == "" || strings.Contains(sentiment, "中性") || strings.Contains(sentiment, "震荡") || strings.Contains(sentiment, "无法判断")
}

func addUnique(values *[]string, value string) {
	for _, v := range *values {
		if v == value {
			return
		}
	}
	*values = append(*values, value)
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
