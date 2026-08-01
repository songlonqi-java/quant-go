package signal

import (
	"fmt"
	"math"
	"strings"
)

// RiskClass describes how a label affects a buy candidate. Strategy direction
// and TotalScore remain research outputs; this policy only controls execution
// qualification and sizing.
type RiskClass string

const (
	RiskClassHardFilter RiskClass = "hard_filter"
	RiskClassPenalty    RiskClass = "penalty"
	RiskClassNotice     RiskClass = "notice"
)

// RiskEffect is the auditable execution meaning of one risk label.
type RiskEffect struct {
	Label              string    `json:"label"`
	Class              RiskClass `json:"class"`
	ConfidencePenalty  float64   `json:"confidence_penalty,omitempty"`
	PositionMultiplier float64   `json:"position_multiplier,omitempty"`
	Description        string    `json:"description"`
}

type riskRule struct {
	class              RiskClass
	confidencePenalty  float64
	positionMultiplier float64
	description        string
}

// riskRules is the single source of truth for labels emitted by the signal,
// realtime, position and portfolio-budget layers. Unknown labels fail safe as
// notices instead of silently becoming new hard filters.
var riskRules = map[string]riskRule{
	"有卖出冲突":   {class: RiskClassHardFilter, description: "同周期策略同时给出卖出意见"},
	"信号较少":    {class: RiskClassNotice, description: "有效票和独立策略组由资格规则单独校验"},
	"5日涨幅过高":  {class: RiskClassHardFilter, description: "五日涨幅超过25%，短期追高风险过大"},
	"20日涨幅过高": {class: RiskClassPenalty, confidencePenalty: 6, positionMultiplier: 0.75, description: "二十日涨幅超过40%，降低执行置信度和仓位"},
	"市场偏弱":    {class: RiskClassNotice, description: "已由市场评分和整体仓位策略处理，避免重复扣分"},
	"亏钱效应":    {class: RiskClassHardFilter, description: "全市场下跌扩散，暂停新增买入"},
	"跌停扩散":    {class: RiskClassHardFilter, description: "全市场跌停扩散，暂停新增买入"},
	"涨停退潮":    {class: RiskClassHardFilter, description: "涨停生态退潮，暂停新增买入"},

	"资金确认": {class: RiskClassNotice, description: "个股总资金和大单同时净流入，作为正向确认"},
	"资金背离": {class: RiskClassHardFilter, description: "个股总资金和大单同时净流出"},
	"资金分歧": {class: RiskClassPenalty, confidencePenalty: 4, positionMultiplier: 0.85, description: "个股总资金与大单方向相反，降低执行置信度和仓位"},
	"资金流出": {class: RiskClassNotice, description: "卖出信号获得资金流出确认"},

	"板块共振":   {class: RiskClassNotice, description: "所属板块强度对买入信号形成正向确认"},
	"板块资金确认": {class: RiskClassNotice, description: "所属板块资金方向形成正向确认"},
	"板块资金背离": {class: RiskClassPenalty, confidencePenalty: 6, positionMultiplier: 0.75, description: "所属板块上涨但资金净流出，降低执行置信度和仓位"},
	"板块退潮":   {class: RiskClassHardFilter, description: "所属板块下跌且跌停或资金流出，暂停新增买入"},
	"板块风险":   {class: RiskClassNotice, description: "卖出信号同时出现板块退潮或资金背离"},

	"涨停风险":  {class: RiskClassHardFilter, description: "盘中接近或触及涨停，不追价买入"},
	"跌停风险":  {class: RiskClassHardFilter, description: "盘中接近或触及跌停，禁止新增买入且卖出可能无法成交"},
	"高开>3%": {class: RiskClassHardFilter, description: "相对昨收高开超过3%，不追高买入"},
	"涨幅偏高":  {class: RiskClassHardFilter, description: "盘中涨幅超过5%，不追高买入"},
	"盘中走弱":  {class: RiskClassHardFilter, description: "盘中跌幅超过2%，等待重新确认"},
	"卖压确认":  {class: RiskClassNotice, description: "盘中下跌确认卖出压力"},
	"卖出缓和":  {class: RiskClassNotice, description: "盘中上涨表明卖出压力暂时缓和"},

	"ST股票":     {class: RiskClassHardFilter, description: "ST或退市风险警示股票不新增仓位"},
	"上市时间不足":   {class: RiskClassHardFilter, description: "上市时间不足，价格与流动性历史尚不稳定"},
	"上市日期缺失":   {class: RiskClassNotice, description: "缺少上市日期，无法执行上市时长校验"},
	"停牌或无成交":   {class: RiskClassHardFilter, description: "信号日停牌或没有有效成交"},
	"成交额数据缺失":  {class: RiskClassHardFilter, description: "缺少成交额，无法验证容量与冲击成本"},
	"成交额不足":    {class: RiskClassHardFilter, description: "近期平均成交额低于流动性门槛"},
	"换手率不足":    {class: RiskClassHardFilter, description: "当日换手率低于配置门槛"},
	"换手数据缺失":   {class: RiskClassNotice, description: "缺少daily_basic换手率；当前配置仅提示"},
	"必需换手数据缺失": {class: RiskClassHardFilter, description: "当前配置要求换手率数据，缺失时禁止新增仓位"},
	"订单金额缺失":   {class: RiskClassHardFilter, description: "缺少参考权益或建议仓位，无法估算容量与冲击成本"},
	"成交占比过高":   {class: RiskClassHardFilter, description: "预计下单金额占近期日均成交额比例过高"},
	"流动性数据无效":  {class: RiskClassHardFilter, description: "流动性配置或行情索引无效"},

	"空仓过滤":   {class: RiskClassNotice, description: "仓位策略或候选资格已经过滤该买入"},
	"轻仓试错":   {class: RiskClassNotice, description: "整体仓位策略已将单票仓位限制为3%"},
	"组合名额过滤": {class: RiskClassNotice, description: "每周期正式买入名额已用完"},
	"组合预算过滤": {class: RiskClassNotice, description: "组合、单票或行业剩余预算不足"},
	"组合预算收缩": {class: RiskClassNotice, description: "建议仓位已按组合剩余预算缩小"},
}

// ApplyRiskPolicy applies execution penalties exactly once from the original
// confidence and position. It is safe to call again before downstream
// allocation; the result is recomputed rather than compounded.
func ApplyRiskPolicy(results []SignalResult) {
	for i := range results {
		r := &results[i]
		if !r.riskPolicyApplied {
			r.riskBaseConfidence = r.Confidence
			r.riskBasePositionPct = r.PositionPct
			r.riskPolicyApplied = true
		}
		r.RiskEffects = AssessRiskPolicy(*r)
		if rawRecommendation(*r) != "买入" {
			continue
		}

		confidencePenalty := 0.0
		positionMultiplier := 1.0
		var penaltyLabels []string
		for _, effect := range r.RiskEffects {
			if effect.Class != RiskClassPenalty {
				continue
			}
			confidencePenalty += effect.ConfidencePenalty
			if effect.PositionMultiplier > 0 {
				positionMultiplier *= effect.PositionMultiplier
			}
			penaltyLabels = append(penaltyLabels, fmt.Sprintf("%s(-%.0f置信度,仓位×%.0f%%)",
				effect.Label, effect.ConfidencePenalty, effect.PositionMultiplier*100))
		}
		r.Confidence = math.Max(0, r.riskBaseConfidence-confidencePenalty)
		r.PositionPct = math.Max(0, r.riskBasePositionPct*positionMultiplier)
		if r.riskPenaltyReason != "" {
			r.Reasons = removeExactString(r.Reasons, r.riskPenaltyReason)
			r.riskPenaltyReason = ""
		}
		if len(penaltyLabels) > 0 {
			r.riskPenaltyReason = "风险扣分: " + strings.Join(penaltyLabels, ",")
			addUnique(&r.Reasons, r.riskPenaltyReason)
		}
	}
}

// RefreshRiskPolicy updates audit metadata after realtime, position or budget
// labels are appended without changing confidence or position.
func RefreshRiskPolicy(results []SignalResult) {
	for i := range results {
		results[i].RiskEffects = AssessRiskPolicy(results[i])
	}
}

// AssessRiskPolicy returns effects in label order, de-duplicated across daily
// and intraday labels.
func AssessRiskPolicy(r SignalResult) []RiskEffect {
	labels := append(append([]string(nil), r.RiskLabels...), r.IntradayLabels...)
	seen := make(map[string]bool, len(labels))
	effects := make([]RiskEffect, 0, len(labels))
	for _, label := range labels {
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		rule, ok := riskRules[label]
		if !ok {
			rule = riskRule{class: RiskClassNotice, description: "未登记执行规则，仅作提示"}
		}
		effects = append(effects, RiskEffect{
			Label:              label,
			Class:              rule.class,
			ConfidencePenalty:  rule.confidencePenalty,
			PositionMultiplier: rule.positionMultiplier,
			Description:        rule.description,
		})
	}
	return effects
}

// RiskPolicySummary renders the actual execution effect, rather than showing
// a warning label whose consequence is ambiguous.
func RiskPolicySummary(r SignalResult) string {
	effects := AssessRiskPolicy(r)
	if len(effects) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(effects))
	for _, effect := range effects {
		switch effect.Class {
		case RiskClassHardFilter:
			parts = append(parts, effect.Label+"[硬过滤]")
		case RiskClassPenalty:
			parts = append(parts, fmt.Sprintf("%s[扣%.0f分/仓位×%.0f%%]",
				effect.Label, effect.ConfidencePenalty, effect.PositionMultiplier*100))
		default:
			parts = append(parts, effect.Label+"[提示]")
		}
	}
	return strings.Join(parts, ";")
}

func hardRiskIssues(r SignalResult) []string {
	var issues []string
	for _, effect := range AssessRiskPolicy(r) {
		if effect.Class == RiskClassHardFilter {
			issues = append(issues, effect.Label)
		}
	}
	return issues
}

func removeExactString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}
