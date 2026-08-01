package execution

import (
	"math"

	"quant/internal/data"
	"quant/internal/strategy"
)

type ExitReason string

const ExitModelVersion = "managed_exit_v1"

const (
	ExitReasonInitialStop ExitReason = "initial_stop"
	ExitReasonATRTrailing ExitReason = "atr_trailing_stop"
	ExitReasonTimeStop    ExitReason = "time_stop"
	ExitReasonStrategy    ExitReason = "strategy_sell"
)

type ExitPolicy struct {
	Horizon             strategy.Horizon
	InitialStopPct      float64
	ATRPeriod           int
	ATRTrailingMultiple float64
	MaxHoldingDays      int
	TailLossPct         float64
}

func DefaultExitPolicy(horizon strategy.Horizon) ExitPolicy {
	switch horizon {
	case strategy.HorizonMid:
		return ExitPolicy{Horizon: horizon, InitialStopPct: 8, ATRPeriod: 14, ATRTrailingMultiple: 2.5, MaxHoldingDays: 20, TailLossPct: -10}
	case strategy.HorizonLong:
		return ExitPolicy{Horizon: horizon, InitialStopPct: 12, ATRPeriod: 14, ATRTrailingMultiple: 3, MaxHoldingDays: 120, TailLossPct: -15}
	default:
		return ExitPolicy{Horizon: strategy.HorizonShort, InitialStopPct: 5, ATRPeriod: 14, ATRTrailingMultiple: 2, MaxHoldingDays: 5, TailLossPct: -8}
	}
}

type ExitTrigger struct {
	Date        string
	Reason      ExitReason
	StopPrice   float64
	Close       float64
	HoldingDays int
}

// ExitState observes only information available at each close. A trigger is
// therefore executable no earlier than the next market open.
type ExitState struct {
	Policy          ExitPolicy
	EntryDate       string
	EntryPrice      float64
	InitialStop     float64
	TrailingStop    float64
	HighestClose    float64
	HeldMarketDays  int
	Triggered       bool
	TriggeredReason ExitReason
}

func NewExitState(horizon strategy.Horizon, entryDate string, entryPrice float64) *ExitState {
	policy := DefaultExitPolicy(horizon)
	return &ExitState{
		Policy:       policy,
		EntryDate:    entryDate,
		EntryPrice:   entryPrice,
		InitialStop:  entryPrice * (1 - policy.InitialStopPct/100),
		HighestClose: entryPrice,
	}
}

// MergeEntry updates a position after an additional buy. The oldest holding
// clock is retained and every stop may only become tighter, never looser.
func (s *ExitState) MergeEntry(horizon strategy.Horizon, averageEntryPrice float64) {
	if s == nil || averageEntryPrice <= 0 {
		return
	}
	mergedHorizon := ShorterHorizon(s.Policy.Horizon, horizon)
	policy := DefaultExitPolicy(mergedHorizon)
	s.Policy = policy
	s.EntryPrice = averageEntryPrice
	candidate := averageEntryPrice * (1 - policy.InitialStopPct/100)
	if candidate > s.InitialStop {
		s.InitialStop = candidate
	}
	if averageEntryPrice > s.HighestClose {
		s.HighestClose = averageEntryPrice
	}
}

// ObserveSession advances one market session. hasPrice=false represents a
// suspension: the time stop still ages, while price-based stops cannot update.
func (s *ExitState) ObserveSession(date string, close, atr float64, hasPrice bool) (ExitTrigger, bool) {
	if s == nil || s.Triggered || s.EntryPrice <= 0 {
		return ExitTrigger{}, false
	}
	s.HeldMarketDays++
	if hasPrice && close > 0 {
		if close > s.HighestClose {
			s.HighestClose = close
		}
		if atr > 0 && s.Policy.ATRTrailingMultiple > 0 {
			candidate := s.HighestClose - atr*s.Policy.ATRTrailingMultiple
			if candidate > s.TrailingStop {
				s.TrailingStop = candidate
			}
		}
		if close <= s.InitialStop {
			return s.trigger(date, ExitReasonInitialStop, s.InitialStop, close), true
		}
		if s.TrailingStop > s.InitialStop && close <= s.TrailingStop {
			return s.trigger(date, ExitReasonATRTrailing, s.TrailingStop, close), true
		}
	}
	if s.Policy.MaxHoldingDays > 0 && s.HeldMarketDays >= s.Policy.MaxHoldingDays {
		return s.trigger(date, ExitReasonTimeStop, 0, close), true
	}
	return ExitTrigger{}, false
}

func (s *ExitState) trigger(date string, reason ExitReason, stopPrice, close float64) ExitTrigger {
	s.Triggered = true
	s.TriggeredReason = reason
	return ExitTrigger{Date: date, Reason: reason, StopPrice: stopPrice, Close: close, HoldingDays: s.HeldMarketDays}
}

// ATR uses executable/raw OHLC when available and adjusted OHLC only as the
// legacy fallback, matching the rest of the execution layer.
func ATR(bars []data.DailyBar, idx, period int) float64 {
	if period <= 0 || idx < period || idx >= len(bars) {
		return 0
	}
	var sum float64
	for i := idx - period + 1; i <= idx; i++ {
		high := bars[i].TradeHigh()
		low := bars[i].TradeLow()
		prevClose := bars[i-1].TradeClose()
		highLow := high - low
		highPrevClose := math.Abs(high - prevClose)
		lowPrevClose := math.Abs(low - prevClose)
		sum += math.Max(highLow, math.Max(highPrevClose, lowPrevClose))
	}
	return sum / float64(period)
}

func ShorterHorizon(left, right strategy.Horizon) strategy.Horizon {
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	rank := map[strategy.Horizon]int{strategy.HorizonShort: 0, strategy.HorizonMid: 1, strategy.HorizonLong: 2}
	if rank[right] < rank[left] {
		return right
	}
	return left
}
