package execution

import (
	"sort"

	"quant/internal/data"
	"quant/internal/strategy"
)

type ManagedExitOutcome struct {
	EntryFeasible         bool
	EntryDate             string
	EntryPrice            float64
	EntryExecutionPrice   float64
	EntryParticipationPct float64
	EntryImpactRate       float64
	Triggered             bool
	TriggerDate           string
	Reason                ExitReason
	StopPrice             float64
	HoldingDays           int
	Completed             bool
	ExitDate              string
	ExitPrice             float64
	ExitExecutionPrice    float64
	ExitParticipationPct  float64
	ExitImpactRate        float64
	DelayDays             int
	TailLoss              bool
	Returns               ReturnBreakdown
}

type SimulationOptions struct {
	Costs          CostModel
	LimitPct       float64
	MaxEntryGapPct float64
	Liquidity      LiquidityPolicy
	OrderValueCNY  float64
}

// SimulateManagedExit applies a next-market-session entry, close-confirmed
// stateful exits, and next-open execution. Missing quotes and limit-down opens
// after a trigger count as delayed exit sessions.
func SimulateManagedExit(source []data.DailyBar, marketDates []string, signalDate string, horizon strategy.Horizon, options SimulationOptions) ManagedExitOutcome {
	outcome := ManagedExitOutcome{}
	if len(source) < 2 || len(marketDates) < 2 || options.Costs.Validate() != nil || options.Liquidity.Validate() != nil || signalDate == "" {
		return outcome
	}
	calendar := normalizedMarketDates(marketDates)
	if len(calendar) < 2 {
		return outcome
	}
	bars := append([]data.DailyBar(nil), source...)
	sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
	barIndex := make(map[string]int, len(bars))
	for i, bar := range bars {
		barIndex[bar.TradeDate] = i
	}
	calendarIndex := sort.SearchStrings(calendar, signalDate)
	if calendarIndex >= len(calendar) || calendar[calendarIndex] != signalDate || calendarIndex+1 >= len(calendar) {
		return outcome
	}
	signalBarIdx, signalOK := barIndex[signalDate]
	entryDate := calendar[calendarIndex+1]
	entryIdx, entryOK := barIndex[entryDate]
	if !signalOK || !entryOK || !CanBuyAtOpen(bars[signalBarIdx], bars[entryIdx], options.LimitPct) {
		return outcome
	}
	entryPrice := bars[entryIdx].TradeOpen()
	prevClose := bars[signalBarIdx].TradeClose()
	if options.MaxEntryGapPct > 0 && prevClose > 0 && (entryPrice/prevClose-1)*100 > options.MaxEntryGapPct {
		return outcome
	}
	entryAverageAmount := AverageAmountCNY(bars, signalBarIdx, options.Liquidity.AmountLookback)
	entryParticipation := ParticipationPct(options.OrderValueCNY, entryAverageAmount)
	if options.Liquidity.Enabled && options.Liquidity.MaxParticipationPct > 0 && entryParticipation > options.Liquidity.MaxParticipationPct {
		return outcome
	}
	entryImpact := EstimateImpactRate(options.OrderValueCNY, entryAverageAmount, options.Liquidity)
	outcome.EntryFeasible = true
	outcome.EntryDate = entryDate
	outcome.EntryPrice = entryPrice
	outcome.EntryParticipationPct = entryParticipation
	outcome.EntryImpactRate = entryImpact
	outcome.EntryExecutionPrice = AdjustedEntryPrice(entryPrice, options.Costs, entryImpact)

	state := NewExitState(horizon, entryDate, entryPrice)
	lastKnownBarIdx := entryIdx
	for marketIdx := calendarIndex + 1; marketIdx < len(calendar); marketIdx++ {
		date := calendar[marketIdx]
		idx, hasBar := barIndex[date]
		close := 0.0
		atr := 0.0
		if hasBar {
			lastKnownBarIdx = idx
			close = bars[idx].TradeClose()
			atr = ATR(bars, idx, state.Policy.ATRPeriod)
		}
		trigger, triggered := state.ObserveSession(date, close, atr, hasBar && close > 0)
		if !triggered {
			continue
		}
		outcome.Triggered = true
		outcome.TriggerDate = trigger.Date
		outcome.Reason = trigger.Reason
		outcome.StopPrice = trigger.StopPrice
		outcome.HoldingDays = trigger.HoldingDays

		for exitMarketIdx := marketIdx + 1; exitMarketIdx < len(calendar); exitMarketIdx++ {
			exitDate := calendar[exitMarketIdx]
			exitIdx, hasExitBar := barIndex[exitDate]
			if !hasExitBar || exitIdx == 0 || !CanSellAtOpen(bars[exitIdx-1], bars[exitIdx], options.LimitPct) {
				outcome.DelayDays++
				continue
			}
			exitPrice := bars[exitIdx].TradeOpen()
			exitOrderValue := options.OrderValueCNY
			if entryPrice > 0 && exitOrderValue > 0 {
				exitOrderValue *= exitPrice / entryPrice
			}
			exitAverageAmount := AverageAmountCNY(bars, lastKnownBarIdx, options.Liquidity.AmountLookback)
			exitParticipation := ParticipationPct(exitOrderValue, exitAverageAmount)
			exitImpact := EstimateImpactRate(exitOrderValue, exitAverageAmount, options.Liquidity)
			returns, ok := RoundTripReturnWithImpact(entryPrice, exitPrice, options.Costs, entryImpact, exitImpact)
			if !ok {
				outcome.DelayDays++
				continue
			}
			outcome.Completed = true
			outcome.ExitDate = exitDate
			outcome.ExitPrice = exitPrice
			outcome.ExitParticipationPct = exitParticipation
			outcome.ExitImpactRate = exitImpact
			outcome.ExitExecutionPrice = AdjustedExitPrice(exitPrice, options.Costs, exitImpact)
			outcome.Returns = returns
			outcome.TailLoss = returns.NetReturnPct <= state.Policy.TailLossPct
			return outcome
		}
		return outcome
	}
	return outcome
}

func normalizedMarketDates(source []string) []string {
	dates := append([]string(nil), source...)
	sort.Strings(dates)
	unique := dates[:0]
	for _, date := range dates {
		if date == "" || (len(unique) > 0 && unique[len(unique)-1] == date) {
			continue
		}
		unique = append(unique, date)
	}
	return unique
}
