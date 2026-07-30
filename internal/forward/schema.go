package forward

import "quant/internal/strategy"

var headers = []string{
	"signal_date", "target_date", "horizon", "rank", "code", "name", "close",
	"buy_signals", "sell_signals", "total_score", "confidence", "position_pct",
	"key_strategies", "market_status", "position_advice", "benchmark", "entry_plan", "invalid_condition",
	"next_open", "next_close", "next_return_pct",
	"day3_close", "day3_return_pct", "day5_close", "day5_return_pct",
	"day10_close", "day10_return_pct", "day20_close", "day20_return_pct", "day40_close", "day40_return_pct",
	"day60_close", "day60_return_pct", "day120_close", "day120_return_pct", "day250_close", "day250_return_pct",
	"status", "notes",
}

var previousHeaders = []string{
	"signal_date", "target_date", "rank", "code", "name", "close",
	"buy_signals", "sell_signals", "total_score", "confidence", "position_pct",
	"key_strategies", "market_status", "position_advice", "entry_plan", "invalid_condition",
	"next_open", "next_close", "next_return_pct",
	"day3_close", "day3_return_pct", "day5_close", "day5_return_pct",
	"status", "notes",
}

var legacyHeaders = []string{
	"signal_date", "target_date", "rank", "code", "name", "close",
	"buy_signals", "sell_signals", "total_score", "key_strategies",
	"market_status", "position_advice", "entry_plan", "invalid_condition",
	"next_open", "next_close", "next_return_pct",
	"day3_close", "day3_return_pct", "day5_close", "day5_return_pct",
	"status", "notes",
}

type validationTarget struct {
	label       string
	offset      int
	closeField  string
	returnField string
}

func validationTargets(horizon string) []validationTarget {
	switch horizon {
	case string(strategy.HorizonMid):
		return []validationTarget{
			{label: "10d", offset: 9, closeField: "day10_close", returnField: "day10_return_pct"},
			{label: "20d", offset: 19, closeField: "day20_close", returnField: "day20_return_pct"},
			{label: "40d", offset: 39, closeField: "day40_close", returnField: "day40_return_pct"},
		}
	case string(strategy.HorizonLong):
		return []validationTarget{
			{label: "60d", offset: 59, closeField: "day60_close", returnField: "day60_return_pct"},
			{label: "120d", offset: 119, closeField: "day120_close", returnField: "day120_return_pct"},
			{label: "250d", offset: 249, closeField: "day250_close", returnField: "day250_return_pct"},
		}
	default:
		return []validationTarget{
			{label: "3d", offset: 2, closeField: "day3_close", returnField: "day3_return_pct"},
			{label: "5d", offset: 4, closeField: "day5_close", returnField: "day5_return_pct"},
		}
	}
}
