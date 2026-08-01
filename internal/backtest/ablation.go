package backtest

import (
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
	BaselineTurnover  float64
	VariantTurnover   float64
	BaselinePeriods   []PeriodPerformance
	VariantPeriods    []PeriodPerformance
	ComparablePeriods int
	PositivePeriods   int
}

func CompareAblation(baseline, variant *Result, initialCapital, riskFreeRate float64, tradingDays int) AblationComparison {
	comparison := AblationComparison{
		BaselineMetrics:  CalculateMetrics(baseline, initialCapital, riskFreeRate, tradingDays),
		VariantMetrics:   CalculateMetrics(variant, initialCapital, riskFreeRate, tradingDays),
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
	return comparison
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
