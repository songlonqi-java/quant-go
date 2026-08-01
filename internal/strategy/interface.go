package strategy

import "quant/internal/data"

type SignalType int

const (
	Hold SignalType = iota
	Buy
	Sell
)

func (s SignalType) String() string {
	switch s {
	case Buy:
		return "BUY"
	case Sell:
		return "SELL"
	default:
		return "HOLD"
	}
}

type Strategy interface {
	Name() string
	Warmup() int
	Signal(bars []data.DailyBar, idx int) SignalType
}

type ScoreStrategy interface {
	Strategy
	// Score describes setup strength. Aggregation uses its magnitude after the
	// strategy has emitted BUY or SELL; Signal supplies the direction.
	Score(bars []data.DailyBar, idx int) float64
}

type StrategyOutput struct {
	Code     string
	Name     string
	Signal   SignalType
	Score    float64
	Close    float64
	Date     string
	Strategy string
}

type FundStoreUser interface {
	SetFundStore(fs interface{})
}

type UniverseUser interface {
	SetUniverse(barsMap map[string][]data.DailyBar)
}

type HistoricalUniverseUser interface {
	SetHistoricalUniverse(barsMap map[string][]data.DailyBar)
}
