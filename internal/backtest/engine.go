package backtest

import "quant/internal/data"

type Trade struct {
	Date   string
	Action string
	Price  float64
	Shares float64
	Cash   float64
	Total  float64
}

type EquityPoint struct {
	Date  string
	Value float64
}

type Config struct {
	InitialCapital float64
	Commission     float64
	Slippage       float64
}

func DefaultConfig() Config {
	return Config{
		InitialCapital: 100000.0,
		Commission:     0.0003,
		Slippage:       0.0001,
	}
}

type Result struct {
	Trades      []Trade
	EquityCurve []EquityPoint
	FinalEquity float64
	TradeCount  int
}

func Run(bars []data.DailyBar, signalFn func(bars []data.DailyBar, idx int) int, cfg Config) *Result {
	if len(bars) < 2 {
		return &Result{FinalEquity: cfg.InitialCapital}
	}

	cash := cfg.InitialCapital
	shares := 0.0
	holding := false
	var trades []Trade
	var equity []EquityPoint
	tradeCount := 0

	for i := 0; i < len(bars); i++ {
		closePrice := bars[i].Close
		sig := signalFn(bars, i)

		if !holding && sig == 1 {
			execPrice := closePrice * (1 + cfg.Slippage)
			available := cash
			if available > 0 && execPrice > 0 {
				shares = available / execPrice
				cost := shares * execPrice * cfg.Commission
				cash = cash - shares*execPrice - cost
				holding = true
				tradeCount++
				trades = append(trades, Trade{
					Date:   bars[i].TradeDate,
					Action: "BUY",
					Price:  execPrice,
					Shares: shares,
					Cash:   cash,
					Total:  cash + shares*closePrice,
				})
			}
		} else if holding && sig == -1 {
			execPrice := closePrice * (1 - cfg.Slippage)
			proceeds := shares * execPrice
			cost := proceeds * cfg.Commission
			cash += proceeds - cost
			shares = 0
			holding = false
			tradeCount++
			trades = append(trades, Trade{
				Date:   bars[i].TradeDate,
				Action: "SELL",
				Price:  execPrice,
				Shares: 0,
				Cash:   cash,
				Total:  cash,
			})
		}

		totalValue := cash + shares*closePrice
		action := "HOLD"
		if !holding {
			action = "EMPTY"
		}
		equity = append(equity, EquityPoint{Date: bars[i].TradeDate, Value: totalValue})

		if len(trades) > 0 && trades[len(trades)-1].Date == bars[i].TradeDate {
			continue
		}
		_ = action
	}

	finalEquity := cash + shares*bars[len(bars)-1].Close

	return &Result{
		Trades:      trades,
		EquityCurve: equity,
		FinalEquity: finalEquity,
		TradeCount:  tradeCount,
	}
}
