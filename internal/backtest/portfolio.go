package backtest

import (
	"fmt"
	"math"
	"sort"

	"quant/internal/data"
	"quant/internal/execution"
	"quant/internal/market"
	"quant/internal/signal"
	"quant/internal/strategy"
)

// PortfolioOptions replays the same multi-strategy aggregation and account
// risk budget used by the live signal workflow against one shared cash account.
type PortfolioOptions struct {
	CodeMap      map[string][]data.DailyBar
	StockNames   map[string]string
	Strategies   []strategy.Strategy
	Moneyflows   *data.MoneyflowStore
	StockInfos   map[string]data.StockInfo
	Fundamentals *data.FundamentalStore
	Liquidity    execution.LiquidityPolicy
	SectorName   func(code, date string) string
	StartDate    string
	EndDate      string
	TopN         int
	Config       Config
	MaxTotalPct  float64
	MaxSinglePct float64
	MaxSectorPct float64
}

type pendingOrder struct {
	signalDate       string
	allocationPct    float64
	horizon          strategy.Horizon
	reason           execution.ExitReason
	stopPrice        float64
	holdingDays      int
	delayDays        int
	impactRate       float64
	participationPct float64
}

type managedPosition struct {
	shares       float64
	averageEntry float64
	entryCost    float64
	exitState    *execution.ExitState
}

// RunPortfolio performs a chronological, next-open replay. Buy orders expire
// after the next market session; sell orders remain pending through suspension
// or limit-down sessions so exit liquidity risk is represented.
func RunPortfolio(opts PortfolioOptions) (*Result, error) {
	if len(opts.CodeMap) == 0 {
		return nil, fmt.Errorf("组合回测缺少行情数据")
	}
	if len(opts.Strategies) == 0 {
		return nil, fmt.Errorf("组合回测缺少策略")
	}
	if opts.Config.InitialCapital <= 0 {
		return nil, fmt.Errorf("组合回测初始资金必须大于0")
	}
	if opts.Config.Commission < 0 || opts.Config.Slippage < 0 {
		return nil, fmt.Errorf("手续费和滑点不能为负")
	}
	if err := opts.Liquidity.Validate(); err != nil {
		return nil, fmt.Errorf("组合回测流动性配置无效: %w", err)
	}
	if opts.Config.LotSize <= 0 {
		opts.Config.LotSize = DefaultConfig().LotSize
	}
	if opts.MaxTotalPct <= 0 {
		opts.MaxTotalPct = 70
	}
	if opts.MaxSinglePct <= 0 {
		opts.MaxSinglePct = 15
	}
	if opts.MaxSectorPct <= 0 {
		opts.MaxSectorPct = 25
	}

	codeMap, indexes, dates := preparePortfolioData(opts.CodeMap)
	dates = portfolioDateRange(dates, opts.StartDate, opts.EndDate)
	if len(dates) == 0 {
		return &Result{FinalEquity: opts.Config.InitialCapital}, nil
	}
	preparePortfolioStrategies(opts.Strategies, codeMap, opts.Fundamentals)
	statuses := market.BuildHistoricalStatus(codeMap)
	evaluator := signal.NewHistoricalEvaluator(opts.Strategies)

	cash := opts.Config.InitialCapital
	holdings := make(map[string]float64)
	positions := make(map[string]*managedPosition)
	lastClose := make(map[string]float64)
	pendingBuys := make(map[string]pendingOrder)
	pendingSells := make(map[string]pendingOrder)
	result := &Result{ExitReasonCounts: make(map[string]int)}

	for _, date := range dates {
		// Sells are executed before buys and remain pending until tradable.
		for _, code := range sortedOrderCodes(pendingSells) {
			shares := holdings[code]
			order := pendingSells[code]
			if shares <= 0 {
				delete(pendingSells, code)
				continue
			}
			idx, ok := indexes[code][date]
			if !ok || idx == 0 || !canSellAtOpen(codeMap[code][idx-1], codeMap[code][idx], opts.Config.LimitPct) {
				result.SkippedSignals++
				order.delayDays++
				pendingSells[code] = order
				continue
			}
			bar := codeMap[code][idx]
			execPrice := execution.AdjustedExitPrice(bar.TradeOpen(), execution.CostModel{Commission: opts.Config.Commission, Slippage: opts.Config.Slippage}, order.impactRate)
			proceeds := shares * execPrice
			cash += proceeds - proceeds*opts.Config.Commission
			delete(holdings, code)
			position := positions[code]
			netReturn := 0.0
			hasReturn := position != nil && position.entryCost > 0
			if hasReturn {
				netReturn = ((proceeds-proceeds*opts.Config.Commission)/position.entryCost - 1) * 100
			}
			delete(pendingSells, code)
			delete(positions, code)
			result.TradeCount++
			result.Trades = append(result.Trades, Trade{
				Code: code, Date: date, SignalDate: order.signalDate, Action: "SELL",
				Price: execPrice, Shares: shares, Cash: cash,
				Reason: string(order.reason), StopPrice: order.stopPrice, DelayDays: order.delayDays, HoldingDays: order.holdingDays,
				ReturnPct: netReturn, HasReturn: hasReturn, ImpactRate: order.impactRate, ParticipationPct: order.participationPct,
			})
			recordTradeImpact(result, order.impactRate, order.participationPct)
			var exitState *execution.ExitState
			if position != nil {
				exitState = position.exitState
			}
			recordExitStats(result, &pendingExit{reason: order.reason, delayDays: order.delayDays}, netReturn, hasReturn, exitState)
		}

		equityAtOpen := portfolioEquity(cash, holdings, codeMap, indexes, date, lastClose, true)
		// Buy orders are day orders: attempt them once on the next market date.
		buysToExecute := pendingBuys
		pendingBuys = make(map[string]pendingOrder)
		for _, code := range sortedOrderCodes(buysToExecute) {
			order := buysToExecute[code]
			idx, ok := indexes[code][date]
			if !ok || idx == 0 || !canBuyAtOpen(codeMap[code][idx-1], codeMap[code][idx], opts.Config.LimitPct) {
				result.SkippedSignals++
				continue
			}
			bar := codeMap[code][idx]
			execPrice := execution.AdjustedEntryPrice(bar.TradeOpen(), execution.CostModel{Commission: opts.Config.Commission, Slippage: opts.Config.Slippage}, order.impactRate)
			allocationValue := equityAtOpen * order.allocationPct / 100
			available := math.Min(cash, allocationValue)
			shares := affordableLotShares(available, execPrice, opts.Config.Commission, opts.Config.LotSize)
			if shares <= 0 {
				result.SkippedSignals++
				continue
			}
			cost := shares * execPrice
			entryCost := cost + cost*opts.Config.Commission
			cash -= entryCost
			holdings[code] += shares
			position := positions[code]
			if position == nil {
				position = &managedPosition{
					shares: shares, averageEntry: bar.TradeOpen(), entryCost: entryCost,
					exitState: execution.NewExitState(order.horizon, date, bar.TradeOpen()),
				}
				positions[code] = position
			} else {
				totalShares := position.shares + shares
				position.averageEntry = (position.averageEntry*position.shares + bar.TradeOpen()*shares) / totalShares
				position.shares = totalShares
				position.entryCost += entryCost
				position.exitState.MergeEntry(order.horizon, position.averageEntry)
			}
			result.TradeCount++
			result.Trades = append(result.Trades, Trade{
				Code: code, Date: date, SignalDate: order.signalDate, Action: "BUY",
				Price: execPrice, Shares: shares, Cash: cash, ImpactRate: order.impactRate, ParticipationPct: order.participationPct,
			})
			recordTradeImpact(result, order.impactRate, order.participationPct)
		}

		for code, dateIndexes := range indexes {
			if idx, ok := dateIndexes[date]; ok {
				if closePrice := codeMap[code][idx].TradeClose(); closePrice > 0 {
					lastClose[code] = closePrice
				}
			}
		}
		closeEquity := portfolioEquity(cash, holdings, codeMap, indexes, date, lastClose, false)
		result.EquityCurve = append(result.EquityCurve, EquityPoint{Date: date, Value: closeEquity})

		// Stops are confirmed using information known at the close and become
		// persistent sell orders for the next market open.
		for _, code := range sortedPortfolioCodes(codeMap) {
			position := positions[code]
			if position == nil || position.exitState == nil || holdings[code] <= 0 {
				continue
			}
			if _, exists := pendingSells[code]; exists {
				continue
			}
			idx, hasBar := indexes[code][date]
			closePrice := 0.0
			atr := 0.0
			if hasBar {
				closePrice = codeMap[code][idx].TradeClose()
				atr = execution.ATR(codeMap[code], idx, position.exitState.Policy.ATRPeriod)
			} else {
				idx = sort.Search(len(codeMap[code]), func(i int) bool { return codeMap[code][i].TradeDate >= date }) - 1
				closePrice = lastClose[code]
			}
			trigger, ok := position.exitState.ObserveSession(date, closePrice, atr, hasBar && closePrice > 0)
			if ok {
				pendingSells[code] = portfolioExitOrder(codeMap[code], idx, holdings[code], closePrice, opts.Liquidity,
					trigger.Date, trigger.Reason, trigger.StopPrice, trigger.HoldingDays)
				delete(pendingBuys, code)
			}
		}

		allSignals := make([]signal.SignalResult, 0)
		for _, code := range sortedPortfolioCodes(codeMap) {
			idx, ok := indexes[code][date]
			if !ok {
				continue
			}
			name := opts.StockNames[code]
			if name == "" {
				name = code
			}
			rows := evaluator.Evaluate(codeMap[code], idx, name, statuses[date], opts.Moneyflows)
			if opts.SectorName != nil {
				for i := range rows {
					rows[i].SectorName = opts.SectorName(code, date)
				}
			}
			allSignals = append(allSignals, rows...)
		}
		signal.ApplyLiquidityPolicy(allSignals, codeMap, signal.LiquidityContext{
			Policy: opts.Liquidity, StockInfos: opts.StockInfos, Fundamentals: opts.Fundamentals,
			ReferenceEquity: closeEquity, ApplyCurrentST: false,
		})

		// Held securities are always allowed to generate an exit, even when
		// they rank below the Top-N recommendation list.
		for _, row := range allSignals {
			if holdings[row.Code] > 0 && row.Recommendation() == "卖出" {
				if _, exists := pendingSells[row.Code]; !exists {
					holdingDays := 0
					if position := positions[row.Code]; position != nil && position.exitState != nil {
						holdingDays = position.exitState.HeldMarketDays
					}
					idx := indexes[row.Code][date]
					pendingSells[row.Code] = portfolioExitOrder(codeMap[row.Code], idx, holdings[row.Code], row.Close, opts.Liquidity,
						date, execution.ExitReasonStrategy, 0, holdingDays)
				}
				delete(pendingBuys, row.Code)
			}
		}

		candidatePool := signal.SelectCandidatePool(allSignals, opts.TopN)
		decision := signal.ApplyPositionPolicy(candidatePool, statuses[date])
		budget := portfolioBudgetAtClose(holdings, lastClose, closeEquity, date, opts, decision)
		signal.ApplyPortfolioBudget(candidatePool, budget)
		for _, row := range signal.LimitByRecommendation(candidatePool, opts.TopN) {
			if row.Recommendation() != "买入" || row.PositionPct <= 0 || pendingSells[row.Code].signalDate != "" {
				continue
			}
			order := pendingBuys[row.Code]
			order.signalDate = date
			order.allocationPct += row.PositionPct
			order.horizon = execution.ShorterHorizon(order.horizon, row.Horizon)
			pendingBuys[row.Code] = order
		}
		for code, order := range pendingBuys {
			idx, ok := indexes[code][date]
			if !ok {
				continue
			}
			orderValue := closeEquity * order.allocationPct / 100
			averageAmount := execution.AverageAmountCNY(codeMap[code], idx, opts.Liquidity.AmountLookback)
			order.participationPct = execution.ParticipationPct(orderValue, averageAmount)
			if opts.Liquidity.Enabled && opts.Liquidity.MaxParticipationPct > 0 && order.participationPct > opts.Liquidity.MaxParticipationPct {
				delete(pendingBuys, code)
				result.SkippedSignals++
				continue
			}
			order.impactRate = execution.EstimateImpactRate(orderValue, averageAmount, opts.Liquidity)
			pendingBuys[code] = order
		}
	}

	result.FinalEquity = portfolioEquity(cash, holdings, codeMap, indexes, dates[len(dates)-1], lastClose, false)
	return result, nil
}

func portfolioExitOrder(bars []data.DailyBar, idx int, shares, closePrice float64, policy execution.LiquidityPolicy, signalDate string, reason execution.ExitReason, stopPrice float64, holdingDays int) pendingOrder {
	orderValue := shares * closePrice
	averageAmount := execution.AverageAmountCNY(bars, idx, policy.AmountLookback)
	return pendingOrder{
		signalDate: signalDate, reason: reason, stopPrice: stopPrice, holdingDays: holdingDays,
		impactRate:       execution.EstimateImpactRate(orderValue, averageAmount, policy),
		participationPct: execution.ParticipationPct(orderValue, averageAmount),
	}
}

func portfolioDateRange(dates []string, start, end string) []string {
	filtered := make([]string, 0, len(dates))
	for _, date := range dates {
		if start != "" && date < start {
			continue
		}
		if end != "" && date > end {
			continue
		}
		filtered = append(filtered, date)
	}
	return filtered
}

func preparePortfolioData(source map[string][]data.DailyBar) (map[string][]data.DailyBar, map[string]map[string]int, []string) {
	codeMap := make(map[string][]data.DailyBar, len(source))
	indexes := make(map[string]map[string]int, len(source))
	dateSet := make(map[string]bool)
	for code, sourceBars := range source {
		bars := append([]data.DailyBar(nil), sourceBars...)
		sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
		codeMap[code] = bars
		indexes[code] = make(map[string]int, len(bars))
		for idx, bar := range bars {
			indexes[code][bar.TradeDate] = idx
			if bar.TradeDate != "" {
				dateSet[bar.TradeDate] = true
			}
		}
	}
	dates := make([]string, 0, len(dateSet))
	for date := range dateSet {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	return codeMap, indexes, dates
}

func preparePortfolioStrategies(strategies []strategy.Strategy, codeMap map[string][]data.DailyBar, fundamentals *data.FundamentalStore) {
	for _, current := range strategies {
		if user, ok := current.(strategy.FundStoreUser); ok {
			user.SetFundStore(fundamentals)
		}
		if user, ok := current.(strategy.HistoricalUniverseUser); ok {
			user.SetHistoricalUniverse(codeMap)
		} else if user, ok := current.(strategy.UniverseUser); ok {
			user.SetUniverse(codeMap)
		}
	}
}

func portfolioBudgetAtClose(holdings map[string]float64, prices map[string]float64, equity float64, date string, opts PortfolioOptions, decision signal.PositionDecision) signal.PortfolioBudget {
	budget := signal.PortfolioBudget{
		MaxTotalPct:       signal.DeployablePositionCap(decision, opts.MaxTotalPct),
		MaxSinglePct:      opts.MaxSinglePct,
		MaxSectorPct:      opts.MaxSectorPct,
		MaxBuysPerHorizon: opts.TopN,
		ExistingCodePct:   make(map[string]float64),
		ExistingSectorPct: make(map[string]float64),
	}
	if equity <= 0 {
		return budget
	}
	for code, shares := range holdings {
		value := shares * prices[code]
		if value <= 0 {
			continue
		}
		pct := value / equity * 100
		budget.ExistingTotalPct += pct
		budget.ExistingCodePct[code] += pct
		if opts.SectorName != nil {
			if sectorName := opts.SectorName(code, date); sectorName != "" {
				budget.ExistingSectorPct[sectorName] += pct
			}
		}
	}
	return budget
}

func portfolioEquity(cash float64, holdings map[string]float64, codeMap map[string][]data.DailyBar, indexes map[string]map[string]int, date string, lastClose map[string]float64, useOpen bool) float64 {
	total := cash
	for code, shares := range holdings {
		price := lastClose[code]
		if idx, ok := indexes[code][date]; ok {
			bar := codeMap[code][idx]
			if useOpen && bar.TradeOpen() > 0 {
				price = bar.TradeOpen()
			} else if !useOpen && bar.TradeClose() > 0 {
				price = bar.TradeClose()
			}
		}
		total += shares * price
	}
	return total
}

func sortedOrderCodes[T any](orders map[string]T) []string {
	codes := make([]string, 0, len(orders))
	for code := range orders {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func sortedPortfolioCodes(codeMap map[string][]data.DailyBar) []string {
	codes := make([]string, 0, len(codeMap))
	for code := range codeMap {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}
