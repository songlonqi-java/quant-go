package market

import (
	"sort"

	"quant/internal/data"
)

// BuildHistoricalStatus derives a no-look-ahead market status for every
// trading date. It intentionally uses the stocks that traded on each date,
// rather than today's universe, so a replay cannot use future prices. The
// synthetic index trend follows the same cross-sectional rules as
// computeCompositeIndex; live reporting continues to prefer the real index.
func BuildHistoricalStatus(barsMap map[string][]data.DailyBar) map[string]*MarketStatus {
	byDate := make(map[string][]data.DailyBar)
	for _, bars := range barsMap {
		for _, bar := range bars {
			if bar.TradeDate != "" {
				byDate[bar.TradeDate] = append(byDate[bar.TradeDate], bar)
			}
		}
	}
	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	type rolling struct {
		bars  []data.DailyBar
		sum20 float64
		sum60 float64
	}
	states := make(map[string]*rolling, len(barsMap))
	result := make(map[string]*MarketStatus, len(dates))

	for _, date := range dates {
		dayBars := byDate[date]
		sort.Slice(dayBars, func(i, j int) bool { return dayBars[i].TsCode < dayBars[j].TsCode })
		ms := &MarketStatus{IndexCode: "全市场(历史合成)", IndexClose: 1}
		tradingTotal := 0
		breadthTotal := 0
		aboveMA20 := 0
		trendTotal := 0
		trendScore := 0
		var avgChange float64

		for _, bar := range dayBars {
			state := states[bar.TsCode]
			if state == nil {
				state = &rolling{}
				states[bar.TsCode] = state
			}
			state.bars = append(state.bars, bar)
			state.sum20 += bar.Close
			state.sum60 += bar.Close
			if len(state.bars) > 20 {
				state.sum20 -= state.bars[len(state.bars)-21].Close
			}
			if len(state.bars) > 60 {
				state.sum60 -= state.bars[len(state.bars)-61].Close
			}

			idx := len(state.bars) - 1
			if idx >= 1 {
				prev := state.bars[idx-1]
				prevClose := prev.TradeClose()
				closePrice := bar.TradeClose()
				if prevClose > 0 && closePrice > 0 {
					tradingTotal++
					avgChange += (closePrice/prevClose - 1) * 100
					switch {
					case closePrice > prevClose:
						ms.RisingCount++
					case closePrice < prevClose:
						ms.FallingCount++
					default:
						ms.FlatCount++
					}
					if isLimitUpClose(bar.TsCode, bar, prevClose) {
						ms.LimitUpCount++
					}
					if isLimitDownClose(bar.TsCode, bar, prevClose) {
						ms.LimitDownCount++
					}
					ms.TurnoverAmount += bar.Amount
				}
			}

			if len(state.bars) >= 20 {
				ma20 := state.sum20 / 20
				if ma20 > 0 {
					breadthTotal++
					if bar.Close > ma20 {
						aboveMA20++
					}
				}
			}
			if len(state.bars) >= 60 {
				ma20 := state.sum20 / 20
				ma60 := state.sum60 / 60
				trendTotal++
				switch {
				case bar.Close > ma20 && bar.Close > ma60 && ma20 > ma60:
					trendScore += 2
				case bar.Close > ma20:
					trendScore++
				case bar.Close < ma60 && ma20 < ma60:
					trendScore -= 2
				case bar.Close < ma20:
					trendScore--
				}
			}
		}

		if tradingTotal > 0 {
			ms.ProfitEffect = float64(ms.RisingCount) / float64(tradingTotal) * 100
			ms.IndexChg = avgChange / float64(tradingTotal)
		}
		ms.UpCount = aboveMA20
		ms.DownCount = breadthTotal - aboveMA20
		if breadthTotal > 0 {
			ms.Breadth = float64(aboveMA20) / float64(breadthTotal) * 100
		}
		if trendTotal > 0 {
			ratio := float64(trendScore) / float64(trendTotal*2)
			switch {
			case ratio > 0.3:
				ms.MATrend = "多头排列 ↑"
			case ratio > 0.1:
				ms.MATrend = "偏多"
			case ratio < -0.3:
				ms.MATrend = "空头排列 ↓"
			case ratio < -0.1:
				ms.MATrend = "偏空"
			default:
				ms.MATrend = "震荡"
			}
		}
		if tradingTotal >= 20 {
			if ms.ProfitEffect <= 30 {
				ms.RiskFlags = append(ms.RiskFlags, "亏钱效应")
			}
			if ms.LimitDownCount >= 10 && ms.LimitDownCount > ms.LimitUpCount {
				ms.RiskFlags = append(ms.RiskFlags, "跌停扩散")
			}
			if ms.LimitUpCount <= 5 && ms.ProfitEffect < 45 {
				ms.RiskFlags = append(ms.RiskFlags, "涨停退潮")
			}
		}
		ms.determineSentiment()
		result[date] = ms
	}
	return result
}
