package signal

import (
	"math"

	"quant/internal/strategy"
)

const (
	sameGroupAdditionalVote = 0.25
	maxVotesPerGroup        = 1.50
)

type directionalGroupVotes struct {
	buy  int
	sell int
}

// applyEffectiveVotes discounts correlated signals in the same strategy
// family. The first directional signal contributes one vote; additional
// signals in that family contribute only 0.25 each, capped at 1.5. Conflicting
// signals cancel within their family before contributing to cross-family
// consensus.
func applyEffectiveVotes(result *SignalResult) {
	if result == nil {
		return
	}
	groups := make(map[strategy.Group]directionalGroupVotes)
	for name, detail := range result.Strategies {
		group := strategy.GroupForStrategy(name)
		votes := groups[group]
		switch detail.Signal {
		case strategy.Buy:
			votes.buy++
		case strategy.Sell:
			votes.sell++
		}
		groups[group] = votes
	}

	result.BuyGroupCount = 0
	result.SellGroupCount = 0
	result.EffectiveBuyVotes = 0
	result.EffectiveSellVotes = 0
	result.VoteMetricsApplied = true
	for _, votes := range groups {
		net := discountedGroupVotes(votes.buy) - discountedGroupVotes(votes.sell)
		switch {
		case net > 1e-9:
			result.BuyGroupCount++
			result.EffectiveBuyVotes += net
		case net < -1e-9:
			result.SellGroupCount++
			result.EffectiveSellVotes += -net
		}
	}
}

func discountedGroupVotes(count int) float64 {
	if count <= 0 {
		return 0
	}
	return math.Min(maxVotesPerGroup, 1+float64(count-1)*sameGroupAdditionalVote)
}

func effectiveVoteTotals(result SignalResult) (float64, float64) {
	if result.VoteMetricsApplied {
		return result.EffectiveBuyVotes, result.EffectiveSellVotes
	}
	// Compatibility path for manually constructed SignalResult values used by
	// library callers and older persisted reports.
	return float64(result.BuyCount), float64(result.SellCount)
}

func requiredIndependentConsensus(horizon strategy.Horizon) (groups int, votes float64) {
	switch horizon {
	case strategy.HorizonShort:
		return 2, 2.25
	case strategy.HorizonMid:
		return 2, 2.00
	case strategy.HorizonLong:
		return 1, 1.00
	default:
		return 2, 2.00
	}
}

func hasRequiredIndependentConsensus(result SignalResult) bool {
	if !result.VoteMetricsApplied {
		return result.BuyCount >= minBuySignals(result.Horizon)
	}
	requiredGroups, requiredVotes := requiredIndependentConsensus(result.Horizon)
	return result.BuyGroupCount >= requiredGroups && result.EffectiveBuyVotes+1e-9 >= requiredVotes
}
