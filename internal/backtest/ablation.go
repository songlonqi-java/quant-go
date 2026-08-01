package backtest

import (
	"fmt"
	"sort"
)

type PeriodPerformance struct {
	Period      string
	StartDate   string
	EndDate     string
	ReturnPct   float64
	MaxDrawdown float64
	TurnoverPct float64
	ExitTrades  int
}

type AblationComparison struct {
	BaselineMetrics   PerformanceMetrics
	VariantMetrics    PerformanceMetrics
	BaselineCosts     CostAttribution
	VariantCosts      CostAttribution
	BaselineTurnover  float64
	VariantTurnover   float64
	BaselinePeriods   []PeriodPerformance
	VariantPeriods    []PeriodPerformance
	ComparablePeriods int
	PositivePeriods   int
	Admission         AblationAdmission
}

type AblationAdmission struct {
	Passed  bool
	Reasons []string
}

func CompareAblation(baseline, variant *Result, initialCapital, riskFreeRate float64, tradingDays int) AblationComparison {
	comparison := AblationComparison{
		BaselineMetrics:  CalculateMetrics(baseline, initialCapital, riskFreeRate, tradingDays),
		VariantMetrics:   CalculateMetrics(variant, initialCapital, riskFreeRate, tradingDays),
		BaselineCosts:    CalculateCostAttribution(baseline, initialCapital),
		VariantCosts:     CalculateCostAttribution(variant, initialCapital),
		BaselineTurnover: portfolioTurnoverPct(baseline, initialCapital),
		VariantTurnover:  portfolioTurnoverPct(variant, initialCapital),
		BaselinePeriods:  performanceByYear(baseline, initialCapital),
		VariantPeriods:   performanceByYear(variant, initialCapital),
	}
	baselineByPeriod := make(map[string]PeriodPerformance, len(comparison.BaselinePeriods))
	for _, period := range comparison.BaselinePeriods {
		baselineByPeriod[period.Period] = period
	}
	for _, period := range comparison.VariantPeriods {
		baselinePeriod, ok := baselineByPeriod[period.Period]
		if !ok {
			continue
		}
		comparison.ComparablePeriods++
		if period.ReturnPct > baselinePeriod.ReturnPct {
			comparison.PositivePeriods++
		}
	}
	comparison.Admission = assessAblationAdmission(comparison)
	return comparison
}

// assessAblationAdmission is deliberately conservative: an experimental
// strategy cannot enter the default portfolio merely by losing slightly less.
// The resulting portfolio must be profitable, improve the baseline, remain
// stable across time, and avoid worsening drawdown or turnover materially.
func assessAblationAdmission(comparison AblationComparison) AblationAdmission {
	base := comparison.BaselineMetrics
	variant := comparison.VariantMetrics
	reasons := make([]string, 0)
	if variant.AnnualizedReturn <= 0 {
		reasons = append(reasons, fmt.Sprintf("实验组年化收益 %.2f%% 不为正", variant.AnnualizedReturn))
	}
	if variant.AnnualizedReturn <= base.AnnualizedReturn {
		reasons = append(reasons, "实验组年化收益未改善")
	}
	if variant.MaxDrawdown > base.MaxDrawdown {
		reasons = append(reasons, fmt.Sprintf("最大回撤由 %.2f%% 恶化到 %.2f%%", base.MaxDrawdown, variant.MaxDrawdown))
	}
	turnoverLimit := comparison.BaselineTurnover * 1.05
	if comparison.BaselineTurnover == 0 {
		turnoverLimit = 0
	}
	if comparison.VariantTurnover > turnoverLimit {
		reasons = append(reasons, fmt.Sprintf("半边换手率 %.2f%% 超过基线 5%% 容忍上限", comparison.VariantTurnover))
	}
	if comparison.ComparablePeriods < 3 {
		reasons = append(reasons, fmt.Sprintf("只有 %d 个可比年份，少于 3 年", comparison.ComparablePeriods))
	} else if comparison.PositivePeriods*3 < comparison.ComparablePeriods*2 {
		reasons = append(reasons, fmt.Sprintf("只有 %d/%d 个年份收益改善，低于 2/3",
			comparison.PositivePeriods, comparison.ComparablePeriods))
	}
	return AblationAdmission{Passed: len(reasons) == 0, Reasons: reasons}
}

func performanceByYear(result *Result, initialCapital float64) []PeriodPerformance {
	if result == nil || len(result.EquityCurve) == 0 {
		return nil
	}
	type bucket struct {
		base   float64
		points []EquityPoint
	}
	buckets := make(map[string]*bucket)
	periodOrder := make([]string, 0)
	previousEquity := initialCapital
	for _, point := range result.EquityCurve {
		if len(point.Date) < 4 || point.Value <= 0 {
			continue
		}
		period := point.Date[:4]
		current := buckets[period]
		if current == nil {
			current = &bucket{base: previousEquity}
			buckets[period] = current
			periodOrder = append(periodOrder, period)
		}
		current.points = append(current.points, point)
		previousEquity = point.Value
	}
	sort.Strings(periodOrder)
	exits := make(map[string]int)
	turnover := make(map[string]float64)
	for _, trade := range result.Trades {
		if len(trade.Date) < 4 || trade.Price <= 0 || trade.Shares <= 0 {
			continue
		}
		period := trade.Date[:4]
		turnover[period] += trade.Price * trade.Shares
		if trade.Action == "SELL" {
			exits[period]++
		}
	}
	out := make([]PeriodPerformance, 0, len(periodOrder))
	for _, period := range periodOrder {
		current := buckets[period]
		if len(current.points) == 0 {
			continue
		}
		base := current.base
		if base <= 0 {
			base = current.points[0].Value
		}
		end := current.points[len(current.points)-1].Value
		curve := make([]EquityPoint, 0, len(current.points)+1)
		curve = append(curve, EquityPoint{Date: current.points[0].Date, Value: base})
		curve = append(curve, current.points...)
		averageEquity := meanEquity(current.points, base)
		periodTurnover := 0.0
		if averageEquity > 0 {
			periodTurnover = turnover[period] * 0.5 / averageEquity * 100
		}
		out = append(out, PeriodPerformance{
			Period: period, StartDate: current.points[0].Date, EndDate: current.points[len(current.points)-1].Date,
			ReturnPct: (end/base - 1) * 100, MaxDrawdown: calcMaxDrawdown(curve),
			TurnoverPct: periodTurnover, ExitTrades: exits[period],
		})
	}
	return out
}

func portfolioTurnoverPct(result *Result, initialCapital float64) float64 {
	if result == nil {
		return 0
	}
	var notional float64
	for _, trade := range result.Trades {
		if trade.Price > 0 && trade.Shares > 0 {
			notional += trade.Price * trade.Shares
		}
	}
	averageEquity := meanEquity(result.EquityCurve, initialCapital)
	if averageEquity <= 0 {
		return 0
	}
	// Half-turnover counts a complete buy/sell round trip once rather than
	// counting both sides as two independent portfolio rotations.
	return notional * 0.5 / averageEquity * 100
}

func meanEquity(curve []EquityPoint, fallback float64) float64 {
	if len(curve) == 0 {
		return fallback
	}
	var total float64
	count := 0
	for _, point := range curve {
		if point.Value > 0 {
			total += point.Value
			count++
		}
	}
	if count == 0 {
		return fallback
	}
	return total / float64(count)
}
