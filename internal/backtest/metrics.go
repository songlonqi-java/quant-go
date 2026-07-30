package backtest

import (
	"fmt"
	"math"
)

type PerformanceMetrics struct {
	TotalReturn      float64
	AnnualizedReturn float64
	SharpeRatio      float64
	MaxDrawdown      float64
	WinRate          float64
	ProfitFactor     float64
	TotalTrades      int
	WinningTrades    int
	LosingTrades     int
	AvgWin           float64
	AvgLoss          float64
	CalmarRatio      float64
	Volatility       float64
	FinalEquity      float64
	InitialCapital   float64
}

func CalculateMetrics(result *Result, initialCapital float64, riskFreeRate float64, tradingDays int) PerformanceMetrics {
	if tradingDays <= 0 {
		tradingDays = 252
	}

	m := PerformanceMetrics{
		InitialCapital: initialCapital,
		FinalEquity:    result.FinalEquity,
		TotalTrades:    result.TradeCount,
	}

	if initialCapital > 0 {
		m.TotalReturn = (result.FinalEquity - initialCapital) / initialCapital * 100
	}

	if len(result.EquityCurve) > 1 {
		days := len(result.EquityCurve)
		years := float64(days) / float64(tradingDays)
		if years > 0 && initialCapital > 0 {
			totalReturn := result.FinalEquity / initialCapital
			m.AnnualizedReturn = (math.Pow(totalReturn, 1.0/years) - 1) * 100
		}
	}

	m.MaxDrawdown = calcMaxDrawdown(result.EquityCurve)

	dailyReturns := calcDailyReturns(result.EquityCurve)
	if len(dailyReturns) > 0 {
		m.Volatility = calcStdDev(dailyReturns) * math.Sqrt(float64(tradingDays)) * 100
	}

	if len(dailyReturns) > 1 && tradingDays > 0 {
		dailyRiskFree := math.Pow(1+riskFreeRate, 1/float64(tradingDays)) - 1
		excessReturns := make([]float64, len(dailyReturns))
		for i, ret := range dailyReturns {
			excessReturns[i] = ret - dailyRiskFree
		}
		stdDev := calcStdDev(excessReturns)
		if stdDev > 0 {
			m.SharpeRatio = mean(excessReturns) / stdDev * math.Sqrt(float64(tradingDays))
		}
	}
	if m.MaxDrawdown > 0 {
		m.CalmarRatio = m.AnnualizedReturn / m.MaxDrawdown
	}

	var buys, sells []Trade
	for _, t := range result.Trades {
		if t.Action == "BUY" {
			buys = append(buys, t)
		} else if t.Action == "SELL" {
			sells = append(sells, t)
		}
	}

	var profits []float64
	var totalProfit, totalLoss float64
	minLen := len(buys)
	if len(sells) < minLen {
		minLen = len(sells)
	}
	for i := 0; i < minLen; i++ {
		pct := (sells[i].Price - buys[i].Price) / buys[i].Price * 100
		profits = append(profits, pct)
		if pct > 0 {
			m.WinningTrades++
			totalProfit += pct
		} else {
			m.LosingTrades++
			totalLoss += math.Abs(pct)
		}
	}

	if m.WinningTrades+m.LosingTrades > 0 {
		m.WinRate = float64(m.WinningTrades) / float64(m.WinningTrades+m.LosingTrades) * 100
	}
	if m.WinningTrades > 0 {
		m.AvgWin = totalProfit / float64(m.WinningTrades)
	}
	if m.LosingTrades > 0 {
		m.AvgLoss = -totalLoss / float64(m.LosingTrades)
	}
	if totalLoss > 0 {
		m.ProfitFactor = totalProfit / totalLoss
	}

	return m
}

func (m PerformanceMetrics) Print() {
	fmt.Println("\n========== 回测绩效报告 ==========")
	fmt.Printf("初始本金:     %12.2f\n", m.InitialCapital)
	fmt.Printf("最终资产:     %12.2f\n", m.FinalEquity)
	fmt.Printf("总收益率:     %11.2f%%\n", m.TotalReturn)
	fmt.Printf("年化收益:     %11.2f%%\n", m.AnnualizedReturn)
	fmt.Printf("最大回撤:     %11.2f%%\n", m.MaxDrawdown)
	fmt.Printf("年化波动:     %11.2f%%\n", m.Volatility)
	fmt.Printf("夏普比率:     %11.2f\n", m.SharpeRatio)
	fmt.Printf("卡玛比率:     %11.2f\n", m.CalmarRatio)
	fmt.Println("-----------------------------------")
	fmt.Printf("总交易次数:   %d (盈 %d / 亏 %d)\n", m.TotalTrades, m.WinningTrades, m.LosingTrades)
	fmt.Printf("胜率:         %11.2f%%\n", m.WinRate)
	fmt.Printf("平均盈利:     %11.2f%%\n", m.AvgWin)
	fmt.Printf("平均亏损:     %11.2f%%\n", m.AvgLoss)
	fmt.Printf("盈亏比:       %11.2f\n", m.ProfitFactor)
	fmt.Println("===================================")
}

func calcMaxDrawdown(equity []EquityPoint) float64 {
	if len(equity) == 0 {
		return 0
	}
	peak := equity[0].Value
	var maxDD float64
	for _, p := range equity {
		if p.Value > peak {
			peak = p.Value
		}
		dd := (peak - p.Value) / peak * 100
		if dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}

func calcDailyReturns(equity []EquityPoint) []float64 {
	if len(equity) < 2 {
		return nil
	}
	var rets []float64
	for i := 1; i < len(equity); i++ {
		if equity[i-1].Value > 0 {
			r := (equity[i].Value/equity[i-1].Value - 1)
			rets = append(rets, r)
		}
	}
	return rets
}

func calcStdDev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))
	var variance float64
	for _, v := range values {
		d := v - mean
		variance += d * d
	}
	variance /= float64(len(values))
	return math.Sqrt(variance)
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}
