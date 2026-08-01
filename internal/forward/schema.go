package forward

import "quant/internal/strategy"

var preExitHeaders = []string{
	"signal_date", "target_date", "horizon", "rank", "code", "name", "close",
	"buy_signals", "sell_signals", "buy_effective_votes", "sell_effective_votes", "buy_groups", "sell_groups",
	"total_score", "confidence", "position_pct",
	"key_strategies", "market_status", "position_advice", "benchmark", "entry_plan", "invalid_condition",
	"cost_model", "commission_rate", "slippage_rate",
	"next_open", "next_close", "next_return_pct", "next_cost_pct", "next_net_return_pct",
	"day3_close", "day3_return_pct", "day3_cost_pct", "day3_net_return_pct",
	"day5_close", "day5_return_pct", "day5_cost_pct", "day5_net_return_pct",
	"day10_close", "day10_return_pct", "day10_cost_pct", "day10_net_return_pct",
	"day20_close", "day20_return_pct", "day20_cost_pct", "day20_net_return_pct",
	"day40_close", "day40_return_pct", "day40_cost_pct", "day40_net_return_pct",
	"day60_close", "day60_return_pct", "day60_cost_pct", "day60_net_return_pct",
	"day120_close", "day120_return_pct", "day120_cost_pct", "day120_net_return_pct",
	"day250_close", "day250_return_pct", "day250_cost_pct", "day250_net_return_pct",
	"status", "notes",
}

// preLiquidityHeaders is the managed-exit schema before capacity and impact
// audit fields were added.
var preLiquidityHeaders = []string{
	"signal_date", "target_date", "horizon", "rank", "code", "name", "close",
	"buy_signals", "sell_signals", "buy_effective_votes", "sell_effective_votes", "buy_groups", "sell_groups",
	"total_score", "confidence", "position_pct",
	"key_strategies", "market_status", "position_advice", "benchmark", "entry_plan", "invalid_condition",
	"cost_model", "commission_rate", "slippage_rate",
	"next_open", "next_close", "next_return_pct", "next_cost_pct", "next_net_return_pct",
	"day3_close", "day3_return_pct", "day3_cost_pct", "day3_net_return_pct",
	"day5_close", "day5_return_pct", "day5_cost_pct", "day5_net_return_pct",
	"day10_close", "day10_return_pct", "day10_cost_pct", "day10_net_return_pct",
	"day20_close", "day20_return_pct", "day20_cost_pct", "day20_net_return_pct",
	"day40_close", "day40_return_pct", "day40_cost_pct", "day40_net_return_pct",
	"day60_close", "day60_return_pct", "day60_cost_pct", "day60_net_return_pct",
	"day120_close", "day120_return_pct", "day120_cost_pct", "day120_net_return_pct",
	"day250_close", "day250_return_pct", "day250_cost_pct", "day250_net_return_pct",
	"exit_model", "managed_exit_status", "exit_trigger_date", "exit_reason", "exit_stop_price", "exit_holding_days",
	"exit_date", "exit_open", "exit_delay_days", "exit_return_pct", "exit_cost_pct", "exit_net_return_pct", "exit_tail_loss",
	"status", "notes",
}

var headers = []string{
	"signal_date", "target_date", "horizon", "rank", "code", "name", "close",
	"buy_signals", "sell_signals", "buy_effective_votes", "sell_effective_votes", "buy_groups", "sell_groups",
	"total_score", "confidence", "position_pct",
	"key_strategies", "market_status", "position_advice", "benchmark", "entry_plan", "invalid_condition",
	"liquidity_model", "liquidity_policy", "estimated_order_value_cny", "average_amount_cny", "turnover_rate_pct", "listing_days",
	"entry_participation_pct", "entry_impact_pct", "exit_participation_pct", "exit_impact_pct",
	"cost_model", "commission_rate", "slippage_rate",
	"next_open", "next_close", "next_return_pct", "next_cost_pct", "next_net_return_pct",
	"day3_close", "day3_return_pct", "day3_cost_pct", "day3_net_return_pct",
	"day5_close", "day5_return_pct", "day5_cost_pct", "day5_net_return_pct",
	"day10_close", "day10_return_pct", "day10_cost_pct", "day10_net_return_pct",
	"day20_close", "day20_return_pct", "day20_cost_pct", "day20_net_return_pct",
	"day40_close", "day40_return_pct", "day40_cost_pct", "day40_net_return_pct",
	"day60_close", "day60_return_pct", "day60_cost_pct", "day60_net_return_pct",
	"day120_close", "day120_return_pct", "day120_cost_pct", "day120_net_return_pct",
	"day250_close", "day250_return_pct", "day250_cost_pct", "day250_net_return_pct",
	"exit_model", "managed_exit_status", "exit_trigger_date", "exit_reason", "exit_stop_price", "exit_holding_days",
	"exit_date", "exit_open", "exit_delay_days", "exit_return_pct", "exit_cost_pct", "exit_net_return_pct", "exit_tail_loss",
	"status", "notes",
}

var preCostHeaders = []string{
	"signal_date", "target_date", "horizon", "rank", "code", "name", "close",
	"buy_signals", "sell_signals", "buy_effective_votes", "sell_effective_votes", "buy_groups", "sell_groups",
	"total_score", "confidence", "position_pct",
	"key_strategies", "market_status", "position_advice", "benchmark", "entry_plan", "invalid_condition",
	"next_open", "next_close", "next_return_pct",
	"day3_close", "day3_return_pct", "day5_close", "day5_return_pct",
	"day10_close", "day10_return_pct", "day20_close", "day20_return_pct", "day40_close", "day40_return_pct",
	"day60_close", "day60_return_pct", "day120_close", "day120_return_pct", "day250_close", "day250_return_pct",
	"status", "notes",
}

// preVoteHeaders was the first multi-horizon schema. Keeping it explicit
// protects locally accumulated records while newer audit fields are added.
var preVoteHeaders = []string{
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
	label          string
	offset         int
	closeField     string
	returnField    string
	costField      string
	netReturnField string
}

func validationTargets(horizon string) []validationTarget {
	switch horizon {
	case string(strategy.HorizonMid):
		return []validationTarget{
			{label: "10d", offset: 9, closeField: "day10_close", returnField: "day10_return_pct", costField: "day10_cost_pct", netReturnField: "day10_net_return_pct"},
			{label: "20d", offset: 19, closeField: "day20_close", returnField: "day20_return_pct", costField: "day20_cost_pct", netReturnField: "day20_net_return_pct"},
			{label: "40d", offset: 39, closeField: "day40_close", returnField: "day40_return_pct", costField: "day40_cost_pct", netReturnField: "day40_net_return_pct"},
		}
	case string(strategy.HorizonLong):
		return []validationTarget{
			{label: "60d", offset: 59, closeField: "day60_close", returnField: "day60_return_pct", costField: "day60_cost_pct", netReturnField: "day60_net_return_pct"},
			{label: "120d", offset: 119, closeField: "day120_close", returnField: "day120_return_pct", costField: "day120_cost_pct", netReturnField: "day120_net_return_pct"},
			{label: "250d", offset: 249, closeField: "day250_close", returnField: "day250_return_pct", costField: "day250_cost_pct", netReturnField: "day250_net_return_pct"},
		}
	default:
		return []validationTarget{
			{label: "3d", offset: 2, closeField: "day3_close", returnField: "day3_return_pct", costField: "day3_cost_pct", netReturnField: "day3_net_return_pct"},
			{label: "5d", offset: 4, closeField: "day5_close", returnField: "day5_return_pct", costField: "day5_cost_pct", netReturnField: "day5_net_return_pct"},
		}
	}
}
