package backtest

import (
	"math"

	"quant/internal/data"
	"quant/internal/execution"
	"quant/internal/strategy"
)

type Trade struct {
	Code              string
	Date              string
	SignalDate        string
	Action            string
	RawPrice          float64
	Price             float64
	Shares            float64
	Cash              float64
	Total             float64
	RawNotional       float64
	ExecutionNotional float64
	CommissionAmount  float64
	SlippageAmount    float64
	ImpactAmount      float64
	TotalCostAmount   float64
	Reason            string
	StopPrice         float64
	DelayDays         int
	HoldingDays       int
	GrossPnLAmount    float64
	NetPnLAmount      float64
	GrossReturnPct    float64
	ReturnPct         float64
	HasReturn         bool
	ImpactRate        float64
	ParticipationPct  float64
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
	Horizon        strategy.Horizon
	ManagedExits   bool
	Liquidity      execution.LiquidityPolicy
	ListDate       string
	StockName      string
	TurnoverRate   func(date string) (float64, bool)
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
	Trades              []Trade
	EquityCurve         []EquityPoint
	FinalEquity         float64
	TradeCount          int
	SkippedSignals      int
	ExitReasonCounts    map[string]int
	DelayedExitTrades   int
	ExitDelayDays       int
	MaxExitDelayDays    int
	TailLossTrades      int
	WorstTradeReturnPct float64
	ImpactedTrades      int
	TotalImpactRate     float64
	MaxImpactRate       float64
	MaxParticipationPct float64
}

type pendingExit struct {
	signalDate       string
	reason           execution.ExitReason
	stopPrice        float64
	holdingDays      int
	delayDays        int
	impactRate       float64
	participationPct float64
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
	pendingImpactRate := 0.0
	pendingParticipationPct := 0.0
	var exitState *execution.ExitState
	var sellOrder *pendingExit
	entryCost := 0.0
	rawEntryNotional := 0.0
	result := &Result{ExitReasonCounts: make(map[string]int)}

	for i := 0; i < len(bars); i++ {
		closePrice := bars[i].TradeClose()

		if i > 0 {
			prev := bars[i-1]
			if sellOrder != nil && holding {
				if canSellAtOpen(prev, bars[i], cfg.LimitPct) {
					soldShares := shares
					fill, ok := execution.ExitExecutionBreakdown(bars[i].TradeOpen(), soldShares,
						execution.CostModel{Commission: cfg.Commission, Slippage: cfg.Slippage}, sellOrder.impactRate)
					if !ok {
						skippedSignals++
						continue
					}
					netProceeds := fill.ExecutionNotional - fill.CommissionAmount
					cash += netProceeds
					netReturn := 0.0
					hasReturn := entryCost > 0
					if hasReturn {
						netReturn = (netProceeds/entryCost - 1) * 100
					}
					grossPnL := fill.RawNotional - rawEntryNotional
					netPnL := netProceeds - entryCost
					grossReturn := 0.0
					if rawEntryNotional > 0 {
						grossReturn = grossPnL / rawEntryNotional * 100
					}
					shares = 0
					holding = false
					tradeCount++
					trades = append(trades, Trade{
						Code: bars[i].TsCode, Date: bars[i].TradeDate, SignalDate: sellOrder.signalDate,
						Action: "SELL", RawPrice: fill.RawPrice, Price: fill.ExecutionPrice, Shares: soldShares, Cash: cash, Total: cash,
						RawNotional: fill.RawNotional, ExecutionNotional: fill.ExecutionNotional,
						CommissionAmount: fill.CommissionAmount, SlippageAmount: fill.SlippageAmount,
						ImpactAmount: fill.ImpactAmount, TotalCostAmount: fill.TotalCostAmount,
						Reason: string(sellOrder.reason), StopPrice: sellOrder.stopPrice, DelayDays: sellOrder.delayDays,
						HoldingDays: sellOrder.holdingDays, GrossPnLAmount: grossPnL, NetPnLAmount: netPnL,
						GrossReturnPct: grossReturn, ReturnPct: netReturn, HasReturn: hasReturn,
						ImpactRate: sellOrder.impactRate, ParticipationPct: sellOrder.participationPct,
					})
					recordTradeImpact(result, sellOrder.impactRate, sellOrder.participationPct)
					recordExitStats(result, sellOrder, netReturn, hasReturn, exitState)
					exitState = nil
					entryCost = 0
					rawEntryNotional = 0
					sellOrder = nil
				} else {
					skippedSignals++
					sellOrder.delayDays++
				}
			}
			switch pendingSignal {
			case strategy.Buy:
				if sellOrder == nil && !holding && canBuyAtOpen(prev, bars[i], cfg.LimitPct) {
					available := cash
					execPrice := execution.AdjustedEntryPrice(bars[i].TradeOpen(), execution.CostModel{Commission: cfg.Commission, Slippage: cfg.Slippage}, pendingImpactRate)
					if available > 0 && execPrice > 0 {
						buyShares := affordableLotShares(available, execPrice, cfg.Commission, cfg.LotSize)
						if buyShares > 0 {
							fill, ok := execution.EntryExecutionBreakdown(bars[i].TradeOpen(), buyShares,
								execution.CostModel{Commission: cfg.Commission, Slippage: cfg.Slippage}, pendingImpactRate)
							if !ok {
								skippedSignals++
								break
							}
							shares = buyShares
							cash = cash - fill.ExecutionNotional - fill.CommissionAmount
							entryCost = fill.ExecutionNotional + fill.CommissionAmount
							rawEntryNotional = fill.RawNotional
							holding = true
							if cfg.ManagedExits {
								exitState = execution.NewExitState(cfg.Horizon, bars[i].TradeDate, bars[i].TradeOpen())
							}
							tradeCount++
							trades = append(trades, Trade{
								Code:              bars[i].TsCode,
								Date:              bars[i].TradeDate,
								SignalDate:        pendingSignalDate,
								Action:            "BUY",
								RawPrice:          fill.RawPrice,
								Price:             fill.ExecutionPrice,
								Shares:            shares,
								Cash:              cash,
								Total:             cash + shares*closePrice,
								RawNotional:       fill.RawNotional,
								ExecutionNotional: fill.ExecutionNotional,
								CommissionAmount:  fill.CommissionAmount,
								SlippageAmount:    fill.SlippageAmount,
								ImpactAmount:      fill.ImpactAmount,
								TotalCostAmount:   fill.TotalCostAmount,
								ImpactRate:        pendingImpactRate,
								ParticipationPct:  pendingParticipationPct,
							})
							recordTradeImpact(result, pendingImpactRate, pendingParticipationPct)
						} else {
							skippedSignals++
						}
					} else {
						skippedSignals++
					}
				} else if !holding {
					skippedSignals++
				}
			}
		}

		totalValue := cash + shares*closePrice
		equity = append(equity, EquityPoint{Date: bars[i].TradeDate, Value: totalValue})

		currentSignal := signalFn(bars, i)
		pendingSignal = strategy.Hold
		pendingSignalDate = bars[i].TradeDate
		if holding {
			if sellOrder == nil && cfg.ManagedExits && exitState != nil {
				trigger, ok := exitState.ObserveSession(bars[i].TradeDate, closePrice, execution.ATR(bars, i, exitState.Policy.ATRPeriod), closePrice > 0)
				if ok {
					sellOrder = buildPendingExit(bars, i, shares, closePrice, cfg, trigger.Date, trigger.Reason, trigger.StopPrice, trigger.HoldingDays)
				}
			}
			if sellOrder == nil && currentSignal == strategy.Sell {
				holdingDays := 0
				if exitState != nil {
					holdingDays = exitState.HeldMarketDays
				}
				sellOrder = buildPendingExit(bars, i, shares, closePrice, cfg, bars[i].TradeDate, execution.ExitReasonStrategy, 0, holdingDays)
			}
		} else if currentSignal == strategy.Buy {
			turnover, hasTurnover := 0.0, false
			if cfg.TurnoverRate != nil {
				turnover, hasTurnover = cfg.TurnoverRate(bars[i].TradeDate)
			}
			assessment := execution.AssessLiquidity(execution.LiquidityInput{
				Bars: bars, Index: i, ListDate: cfg.ListDate, StockName: cfg.StockName,
				TurnoverRatePct: turnover, HasTurnover: hasTurnover, OrderValueCNY: cash,
			}, cfg.Liquidity)
			if assessment.Eligible {
				pendingSignal = strategy.Buy
				pendingImpactRate = assessment.EstimatedImpactRate
				pendingParticipationPct = assessment.ParticipationPct
			} else {
				skippedSignals++
			}
		}
	}

	finalEquity := cash + shares*bars[len(bars)-1].TradeClose()

	result.Trades = trades
	result.EquityCurve = equity
	result.FinalEquity = finalEquity
	result.TradeCount = tradeCount
	result.SkippedSignals = skippedSignals
	return result
}

func buildPendingExit(bars []data.DailyBar, idx int, shares, closePrice float64, cfg Config, signalDate string, reason execution.ExitReason, stopPrice float64, holdingDays int) *pendingExit {
	orderValue := shares * closePrice
	averageAmount := execution.AverageAmountCNY(bars, idx, cfg.Liquidity.AmountLookback)
	return &pendingExit{
		signalDate: signalDate, reason: reason, stopPrice: stopPrice, holdingDays: holdingDays,
		impactRate:       execution.EstimateImpactRate(orderValue, averageAmount, cfg.Liquidity),
		participationPct: execution.ParticipationPct(orderValue, averageAmount),
	}
}

func recordExitStats(result *Result, order *pendingExit, netReturn float64, hasReturn bool, state *execution.ExitState) {
	if result == nil || order == nil {
		return
	}
	previousExits := 0
	for _, count := range result.ExitReasonCounts {
		previousExits += count
	}
	result.ExitReasonCounts[string(order.reason)]++
	if order.delayDays > 0 {
		result.DelayedExitTrades++
		result.ExitDelayDays += order.delayDays
		if order.delayDays > result.MaxExitDelayDays {
			result.MaxExitDelayDays = order.delayDays
		}
	}
	if !hasReturn {
		return
	}
	if previousExits == 0 || netReturn < result.WorstTradeReturnPct {
		result.WorstTradeReturnPct = netReturn
	}
	if state != nil && netReturn <= state.Policy.TailLossPct {
		result.TailLossTrades++
	}
}

func recordTradeImpact(result *Result, impactRate, participationPct float64) {
	if result == nil || impactRate <= 0 {
		return
	}
	result.ImpactedTrades++
	result.TotalImpactRate += impactRate
	if impactRate > result.MaxImpactRate {
		result.MaxImpactRate = impactRate
	}
	if participationPct > result.MaxParticipationPct {
		result.MaxParticipationPct = participationPct
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
	return execution.CanBuyAtOpen(prev, cur, limitPct)
}

func canSellAtOpen(prev, cur data.DailyBar, limitPct float64) bool {
	return execution.CanSellAtOpen(prev, cur, limitPct)
}
