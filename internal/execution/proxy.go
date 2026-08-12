package execution

import "quant/internal/data"

// MarketProxyPoint is the equal-weight market open and close on one trading
// date, averaged across every stock with quotes that day.
type MarketProxyPoint struct {
	Open  float64
	Close float64
}

// MarketProxyIndex builds the equal-weight market proxy for all trading dates
// in one pass over the dataset. MarketProxyReturn then answers any holding
// window with two map lookups, which keeps high-frequency replay loops cheap.
func MarketProxyIndex(barsMap map[string][]data.DailyBar) map[string]MarketProxyPoint {
	sumOpen := make(map[string]float64)
	sumClose := make(map[string]float64)
	count := make(map[string]int)
	for _, bars := range barsMap {
		for _, bar := range bars {
			open := bar.TradeOpen()
			close := bar.TradeClose()
			if open <= 0 || close <= 0 || bar.TradeDate == "" {
				continue
			}
			sumOpen[bar.TradeDate] += open
			sumClose[bar.TradeDate] += close
			count[bar.TradeDate]++
		}
	}
	index := make(map[string]MarketProxyPoint, len(count))
	for date, n := range count {
		index[date] = MarketProxyPoint{
			Open:  sumOpen[date] / float64(n),
			Close: sumClose[date] / float64(n),
		}
	}
	return index
}

// MarketProxyReturn computes the net round-trip return of the equal-weight
// market proxy between the opening of entryDate and the closing of exitDate.
func MarketProxyReturn(index map[string]MarketProxyPoint, entryDate, exitDate string, costModel CostModel) (ReturnBreakdown, bool) {
	entry, okEntry := index[entryDate]
	exit, okExit := index[exitDate]
	if !okEntry || !okExit || entry.Open <= 0 || exit.Close <= 0 {
		return ReturnBreakdown{}, false
	}
	return RoundTripReturn(entry.Open, exit.Close, costModel)
}

// EqualWeightMarketReturn computes the average net round-trip return of every
// stock with quotes on both dates, entering at the opening of entryDate and
// exiting at the closing of exitDate. It is the market proxy used to judge
// whether a strategy's historical return is genuine alpha or just market beta.
func EqualWeightMarketReturn(barsMap map[string][]data.DailyBar, entryDate, exitDate string, costModel CostModel) (ReturnBreakdown, bool) {
	return MarketProxyReturn(MarketProxyIndex(barsMap), entryDate, exitDate, costModel)
}

func indexByDate(bars []data.DailyBar, date string) int {
	for i, bar := range bars {
		if bar.TradeDate == date {
			return i
		}
	}
	return -1
}
