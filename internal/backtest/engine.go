package backtest

import (
	"math"

	"quant/internal/data"
	"quant/internal/strategy"
)

type Trade struct {
	Code       string
	Date       string
	SignalDate string
	Action     string
	Price      float64
	Shares     float64
	Cash       float64
	Total      float64
}

type EquityPoint struct {
	Date  string
	Value float64
}

type Config struct {
	InitialCapital float64
	Commission     float64
	Slippage       float64
	LimitPct       float64
	LotSize        float64
}

func DefaultConfig() Config {
	return Config{
		InitialCapital: 100000.0,
		Commission:     0.0003,
		Slippage:       0.0001,
		LimitPct:       0,
		LotSize:        100,
	}
}

type Result struct {
	Trades         []Trade
	EquityCurve    []EquityPoint
	FinalEquity    float64
	TradeCount     int
	SkippedSignals int
}

func Run(bars []data.DailyBar, signalFn func(bars []data.DailyBar, idx int) strategy.SignalType, cfg Config) *Result {
	if len(bars) < 2 {
		return &Result{FinalEquity: cfg.InitialCapital}
	}
	if cfg.LotSize <= 0 {
		cfg.LotSize = DefaultConfig().LotSize
	}

	cash := cfg.InitialCapital
	shares := 0.0
	holding := false
	var trades []Trade
	var equity []EquityPoint
	tradeCount := 0
	skippedSignals := 0
	pendingSignal := strategy.Hold
	pendingSignalDate := ""

	for i := 0; i < len(bars); i++ {
		closePrice := bars[i].TradeClose()

		if i > 0 {
			prev := bars[i-1]
			switch pendingSignal {
			case strategy.Buy:
				if !holding && canBuyAtOpen(prev, bars[i], cfg.LimitPct) {
					execPrice := bars[i].TradeOpen() * (1 + cfg.Slippage)
					available := cash
					if available > 0 && execPrice > 0 {
						buyShares := affordableLotShares(available, execPrice, cfg.Commission, cfg.LotSize)
						if buyShares > 0 {
							shares = buyShares
							cost := shares * execPrice * cfg.Commission
							cash = cash - shares*execPrice - cost
							holding = true
							tradeCount++
							trades = append(trades, Trade{
								Code:       bars[i].TsCode,
								Date:       bars[i].TradeDate,
								SignalDate: pendingSignalDate,
								Action:     "BUY",
								Price:      execPrice,
								Shares:     shares,
								Cash:       cash,
								Total:      cash + shares*closePrice,
							})
						} else {
							skippedSignals++
						}
					} else {
						skippedSignals++
					}
				} else if !holding {
					skippedSignals++
				}
			case strategy.Sell:
				if holding && canSellAtOpen(prev, bars[i], cfg.LimitPct) {
					execPrice := bars[i].TradeOpen() * (1 - cfg.Slippage)
					soldShares := shares
					proceeds := soldShares * execPrice
					cost := proceeds * cfg.Commission
					cash += proceeds - cost
					shares = 0
					holding = false
					tradeCount++
					trades = append(trades, Trade{
						Code:       bars[i].TsCode,
						Date:       bars[i].TradeDate,
						SignalDate: pendingSignalDate,
						Action:     "SELL",
						Price:      execPrice,
						Shares:     soldShares,
						Cash:       cash,
						Total:      cash,
					})
				} else if holding {
					skippedSignals++
				}
			}
		}

		totalValue := cash + shares*closePrice
		equity = append(equity, EquityPoint{Date: bars[i].TradeDate, Value: totalValue})

		pendingSignal = signalFn(bars, i)
		pendingSignalDate = bars[i].TradeDate
	}

	finalEquity := cash + shares*bars[len(bars)-1].TradeClose()

	return &Result{
		Trades:         trades,
		EquityCurve:    equity,
		FinalEquity:    finalEquity,
		TradeCount:     tradeCount,
		SkippedSignals: skippedSignals,
	}
}

func affordableLotShares(available, execPrice, commission, lotSize float64) float64 {
	if available <= 0 || execPrice <= 0 || lotSize <= 0 {
		return 0
	}
	maxShares := available / (execPrice * (1 + commission))
	if maxShares <= 0 {
		return 0
	}
	return math.Floor(maxShares/lotSize) * lotSize
}

func canBuyAtOpen(prev, cur data.DailyBar, limitPct float64) bool {
	open := cur.TradeOpen()
	if open <= 0 || cur.Vol <= 0 {
		return false
	}
	prevClose := prev.TradeClose()
	if cur.UpLimit > 0 {
		return !cur.IsLimitUpOpen()
	}
	if limitPct > 0 {
		return !data.IsApproxLimitUpWithThreshold(open, prevClose, limitPct)
	}
	return !data.IsApproxLimitUp(cur.TsCode, open, prevClose)
}

func canSellAtOpen(prev, cur data.DailyBar, limitPct float64) bool {
	open := cur.TradeOpen()
	if open <= 0 || cur.Vol <= 0 {
		return false
	}
	prevClose := prev.TradeClose()
	if cur.DownLimit > 0 {
		return !cur.IsLimitDownOpen()
	}
	if limitPct > 0 {
		return !data.IsApproxLimitDownWithThreshold(open, prevClose, limitPct)
	}
	return !data.IsApproxLimitDown(cur.TsCode, open, prevClose)
}
