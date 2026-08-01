package execution

import (
	"fmt"
	"math"
	"strings"
	"time"

	"quant/internal/data"
)

const (
	LiquidityModelVersion  = "liquidity_v1"
	ImpactCostModelVersion = "round_trip_v2_impact"
)

// LiquidityPolicy uses CNY for monetary thresholds, percent for turnover and
// participation thresholds, and decimal rates for market impact.
type LiquidityPolicy struct {
	Enabled             bool    `json:"enabled"`
	MinListingDays      int     `json:"min_listing_days"`
	AmountLookback      int     `json:"amount_lookback"`
	MinAverageAmountCNY float64 `json:"min_average_amount_cny"`
	MinTurnoverRatePct  float64 `json:"min_turnover_rate_pct"`
	RequireTurnoverData bool    `json:"require_turnover_data"`
	MaxParticipationPct float64 `json:"max_participation_pct"`
	ImpactCoefficient   float64 `json:"impact_coefficient"`
	MaxImpactRate       float64 `json:"max_impact_rate"`
}

func DefaultLiquidityPolicy() LiquidityPolicy {
	return LiquidityPolicy{
		Enabled:             true,
		MinListingDays:      60,
		AmountLookback:      20,
		MinAverageAmountCNY: 20_000_000,
		MinTurnoverRatePct:  0.5,
		// Historical daily_basic may not have been downloaded yet. Missing
		// turnover remains visible, while amount still fails closed.
		RequireTurnoverData: false,
		MaxParticipationPct: 5,
		ImpactCoefficient:   0.005,
		MaxImpactRate:       0.02,
	}
}

func (p LiquidityPolicy) Validate() error {
	if !p.Enabled {
		return nil
	}
	if p.MinListingDays < 0 || p.AmountLookback < 0 || !finiteNonNegative(p.MinAverageAmountCNY) ||
		!finiteNonNegative(p.MinTurnoverRatePct) || !finiteNonNegative(p.MaxParticipationPct) ||
		!finiteNonNegative(p.ImpactCoefficient) || !finiteNonNegative(p.MaxImpactRate) || p.MaxImpactRate >= 1 {
		return fmt.Errorf("流动性配置包含负数、非有限数或无效冲击上限")
	}
	return nil
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

type LiquidityInput struct {
	Bars            []data.DailyBar
	Index           int
	ListDate        string
	StockName       string
	TurnoverRatePct float64
	HasTurnover     bool
	OrderValueCNY   float64
}

type LiquidityAssessment struct {
	Eligible            bool
	HardLabels          []string
	NoticeLabels        []string
	ListingDays         int
	AverageAmountCNY    float64
	TurnoverRatePct     float64
	HasTurnover         bool
	OrderValueCNY       float64
	ParticipationPct    float64
	EstimatedImpactRate float64
}

func AssessLiquidity(input LiquidityInput, policy LiquidityPolicy) LiquidityAssessment {
	assessment := LiquidityAssessment{
		Eligible:        true,
		HasTurnover:     input.HasTurnover,
		TurnoverRatePct: input.TurnoverRatePct,
		OrderValueCNY:   input.OrderValueCNY,
	}
	if !policy.Enabled {
		return assessment
	}
	if policy.Validate() != nil || input.Index < 0 || input.Index >= len(input.Bars) {
		assessment.Eligible = false
		assessment.HardLabels = append(assessment.HardLabels, "流动性数据无效")
		return assessment
	}
	bar := input.Bars[input.Index]
	if IsSTName(input.StockName) {
		assessment.HardLabels = append(assessment.HardLabels, "ST股票")
	}
	if bar.Vol <= 0 || bar.TradeClose() <= 0 {
		assessment.HardLabels = append(assessment.HardLabels, "停牌或无成交")
	}
	assessment.ListingDays = listingDays(input.ListDate, bar.TradeDate)
	if input.ListDate == "" || assessment.ListingDays < 0 {
		assessment.NoticeLabels = append(assessment.NoticeLabels, "上市日期缺失")
	} else if policy.MinListingDays > 0 && assessment.ListingDays < policy.MinListingDays {
		assessment.HardLabels = append(assessment.HardLabels, "上市时间不足")
	}
	assessment.AverageAmountCNY = AverageAmountCNY(input.Bars, input.Index, policy.AmountLookback)
	if policy.MinAverageAmountCNY > 0 {
		switch {
		case assessment.AverageAmountCNY <= 0:
			assessment.HardLabels = append(assessment.HardLabels, "成交额数据缺失")
		case assessment.AverageAmountCNY < policy.MinAverageAmountCNY:
			assessment.HardLabels = append(assessment.HardLabels, "成交额不足")
		}
	}
	if policy.MinTurnoverRatePct > 0 {
		switch {
		case input.HasTurnover && input.TurnoverRatePct < policy.MinTurnoverRatePct:
			assessment.HardLabels = append(assessment.HardLabels, "换手率不足")
		case !input.HasTurnover && policy.RequireTurnoverData:
			assessment.HardLabels = append(assessment.HardLabels, "必需换手数据缺失")
		case !input.HasTurnover:
			assessment.NoticeLabels = append(assessment.NoticeLabels, "换手数据缺失")
		}
	}
	if (policy.MaxParticipationPct > 0 || policy.ImpactCoefficient > 0) && input.OrderValueCNY <= 0 {
		assessment.HardLabels = append(assessment.HardLabels, "订单金额缺失")
	}
	assessment.ParticipationPct = ParticipationPct(input.OrderValueCNY, assessment.AverageAmountCNY)
	assessment.EstimatedImpactRate = EstimateImpactRate(input.OrderValueCNY, assessment.AverageAmountCNY, policy)
	if policy.MaxParticipationPct > 0 && assessment.ParticipationPct > policy.MaxParticipationPct {
		assessment.HardLabels = append(assessment.HardLabels, "成交占比过高")
	}
	assessment.Eligible = len(assessment.HardLabels) == 0
	return assessment
}

// AverageAmountCNY converts Tushare daily.amount (thousand CNY) into CNY.
// Only positive observations are averaged so a missing legacy field cannot be
// mistaken for a genuine zero-turnover session.
func AverageAmountCNY(bars []data.DailyBar, idx, lookback int) float64 {
	if idx < 0 || idx >= len(bars) {
		return 0
	}
	if lookback <= 0 {
		lookback = 1
	}
	start := idx - lookback + 1
	if start < 0 {
		start = 0
	}
	var sum float64
	count := 0
	for i := start; i <= idx; i++ {
		if bars[i].Amount > 0 {
			sum += bars[i].Amount * 1000
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func ParticipationPct(orderValueCNY, averageAmountCNY float64) float64 {
	if orderValueCNY <= 0 || averageAmountCNY <= 0 {
		return 0
	}
	return orderValueCNY / averageAmountCNY * 100
}

// EstimateImpactRate uses a capped square-root participation model. The rate
// is applied adversely on each side independently and is additional to the
// configured fixed slippage.
func EstimateImpactRate(orderValueCNY, averageAmountCNY float64, policy LiquidityPolicy) float64 {
	if !policy.Enabled || orderValueCNY <= 0 || averageAmountCNY <= 0 || policy.ImpactCoefficient <= 0 {
		return 0
	}
	participation := orderValueCNY / averageAmountCNY
	impact := policy.ImpactCoefficient * math.Sqrt(participation)
	if policy.MaxImpactRate > 0 && impact > policy.MaxImpactRate {
		impact = policy.MaxImpactRate
	}
	return impact
}

func AdjustedEntryPrice(rawPrice float64, costs CostModel, impactRate float64) float64 {
	if rawPrice <= 0 || costs.Validate() != nil || !finiteNonNegative(impactRate) || impactRate >= 1 {
		return 0
	}
	return rawPrice * (1 + costs.Slippage) * (1 + impactRate)
}

func AdjustedExitPrice(rawPrice float64, costs CostModel, impactRate float64) float64 {
	if rawPrice <= 0 || costs.Validate() != nil || !finiteNonNegative(impactRate) || impactRate >= 1 {
		return 0
	}
	return rawPrice * (1 - costs.Slippage) * (1 - impactRate)
}

func RoundTripReturnWithImpact(entry, exit float64, costs CostModel, entryImpactRate, exitImpactRate float64) (ReturnBreakdown, bool) {
	entryPrice := AdjustedEntryPrice(entry, costs, entryImpactRate)
	exitPrice := AdjustedExitPrice(exit, costs, exitImpactRate)
	if entryPrice <= 0 || exitPrice <= 0 {
		return ReturnBreakdown{}, false
	}
	entryCost := entryPrice * (1 + costs.Commission)
	exitValue := exitPrice * (1 - costs.Commission)
	gross := (exit/entry - 1) * 100
	net := (exitValue/entryCost - 1) * 100
	return ReturnBreakdown{
		GrossReturnPct: gross,
		CostImpactPct:  gross - net,
		NetReturnPct:   net,
		EntryCost:      entryCost,
		ExitValue:      exitValue,
	}, true
}

func IsSTName(name string) bool {
	return strings.Contains(strings.ToUpper(strings.TrimSpace(name)), "ST")
}

func listingDays(listDate, tradeDate string) int {
	listed, listedErr := time.Parse("20060102", listDate)
	traded, tradedErr := time.Parse("20060102", tradeDate)
	if listedErr != nil || tradedErr != nil || traded.Before(listed) {
		return -1
	}
	return int(traded.Sub(listed).Hours() / 24)
}
