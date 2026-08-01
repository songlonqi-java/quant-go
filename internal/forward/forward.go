package forward

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"quant/internal/data"
	"quant/internal/execution"
	"quant/internal/market"
	"quant/internal/signal"
	"quant/internal/strategy"
)

const picksFile = "picks.csv"

func Record(dir string, results []signal.SignalResult, marketStatus *market.MarketStatus, limit int, tradingDates []string) error {
	return RecordWithDecision(dir, results, marketStatus, limit, tradingDates, signal.PositionDecision{})
}

func RecordWithDecision(dir string, results []signal.SignalResult, marketStatus *market.MarketStatus, limit int, tradingDates []string, decision signal.PositionDecision) error {
	if limit <= 0 {
		limit = 5
	}
	picks := make([]signal.SignalResult, 0, limit*3)
	counts := make(map[strategy.Horizon]int)
	for _, r := range results {
		if r.Recommendation() == "买入" {
			h := r.Horizon
			if counts[h] >= limit {
				continue
			}
			picks = append(picks, r)
			counts[h]++
		}
	}
	if len(picks) == 0 {
		if shouldRecordCash(decision) {
			return recordCashDecision(dir, results, marketStatus, tradingDates, decision)
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	signalDate := picks[0].Date
	targetDate := nextTradingDate(signalDate, tradingDates)
	path := filepath.Join(dir, picksFile)
	if err := Migrate(dir); err != nil {
		return err
	}
	existing, err := readRows(path)
	if os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, row := range existing {
		seen[row["signal_date"]+"|"+row["target_date"]+"|"+row["horizon"]+"|"+row["code"]] = true
	}

	var rows []map[string]string
	for _, pick := range picks {
		key := signalDate + "|" + targetDate + "|" + string(pick.Horizon) + "|" + pick.Code
		if seen[key] {
			continue
		}
		rows = append(rows, map[string]string{
			"signal_date":               signalDate,
			"target_date":               targetDate,
			"horizon":                   string(pick.Horizon),
			"rank":                      strconv.Itoa(len(rows) + 1),
			"code":                      pick.Code,
			"name":                      pick.Name,
			"close":                     fmt.Sprintf("%.2f", pick.Close),
			"buy_signals":               strconv.Itoa(pick.BuyCount),
			"sell_signals":              strconv.Itoa(pick.SellCount),
			"buy_effective_votes":       fmt.Sprintf("%.2f", pick.EffectiveBuyVotes),
			"sell_effective_votes":      fmt.Sprintf("%.2f", pick.EffectiveSellVotes),
			"buy_groups":                strconv.Itoa(pick.BuyGroupCount),
			"sell_groups":               strconv.Itoa(pick.SellGroupCount),
			"total_score":               fmt.Sprintf("%.2f", pick.TotalScore),
			"confidence":                fmt.Sprintf("%.0f", pick.Confidence),
			"position_pct":              fmt.Sprintf("%.1f", pick.PositionPct),
			"key_strategies":            strings.Join(buyStrategies(pick), ";"),
			"market_status":             marketSentiment(marketStatus),
			"position_advice":           marketAdvice(marketStatus),
			"benchmark":                 "",
			"entry_plan":                entryPlan(pick),
			"invalid_condition":         "流动性不合格、高开>3%或目标日开盘涨停",
			"liquidity_model":           pick.LiquidityModel,
			"estimated_order_value_cny": fmt.Sprintf("%.2f", pick.EstimatedOrderValueCNY),
			"average_amount_cny":        fmt.Sprintf("%.2f", pick.AverageAmountCNY),
			"turnover_rate_pct":         formatForwardOptional(pick.TurnoverRatePct, pick.HasTurnover),
			"listing_days":              strconv.Itoa(pick.ListingDays),
			"entry_participation_pct":   fmt.Sprintf("%.4f", pick.ParticipationPct),
			"entry_impact_pct":          fmt.Sprintf("%.4f", pick.EstimatedImpactPct),
			"status":                    "pending",
			"notes":                     strings.Join(append([]string{signal.RiskPolicySummary(pick)}, pick.Reasons...), ";"),
		})
	}
	if len(rows) == 0 {
		return nil
	}
	if err := appendRows(path, rows); err != nil {
		return err
	}
	return writeDailyMarkdown(dir, signalDate, targetDate, picks, marketStatus)
}

func shouldRecordCash(decision signal.PositionDecision) bool {
	return decision.Action == signal.PositionActionCash || decision.Action == signal.PositionActionWatch
}

func recordCashDecision(dir string, results []signal.SignalResult, marketStatus *market.MarketStatus, tradingDates []string, decision signal.PositionDecision) error {
	signalDate := latestSignalDate(results)
	if signalDate == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	targetDate := nextTradingDate(signalDate, tradingDates)
	path := filepath.Join(dir, picksFile)
	if err := Migrate(dir); err != nil {
		return err
	}
	existing, err := readRows(path)
	if os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		return err
	}
	key := signalDate + "|" + targetDate + "|short|CASH"
	for _, row := range existing {
		if row["signal_date"]+"|"+row["target_date"]+"|"+row["horizon"]+"|"+row["code"] == key {
			return nil
		}
	}
	row := map[string]string{
		"signal_date":          signalDate,
		"target_date":          targetDate,
		"horizon":              "short",
		"rank":                 "0",
		"code":                 "CASH",
		"name":                 string(decision.Action),
		"close":                "0.00",
		"buy_signals":          "0",
		"sell_signals":         "0",
		"buy_effective_votes":  "0.00",
		"sell_effective_votes": "0.00",
		"buy_groups":           "0",
		"sell_groups":          "0",
		"total_score":          "0.00",
		"confidence":           "0",
		"position_pct":         "0.0",
		"key_strategies":       "cash",
		"market_status":        marketSentiment(marketStatus),
		"position_advice":      decision.Advice,
		"benchmark":            "MARKET_PROXY_EQUAL_WEIGHT",
		"entry_plan":           decision.Advice,
		"invalid_condition":    "市场转强且出现合格候选才解除空仓",
		"status":               "cash",
		"notes":                strings.Join(decision.Reasons, ";"),
	}
	if err := appendRows(path, []map[string]string{row}); err != nil {
		return err
	}
	return writeCashMarkdown(dir, signalDate, targetDate, decision, marketStatus)
}

func latestSignalDate(results []signal.SignalResult) string {
	var latest string
	for _, r := range results {
		if r.Date > latest {
			latest = r.Date
		}
	}
	return latest
}

func Migrate(dir string) error {
	path := filepath.Join(dir, picksFile)
	rows, err := readRows(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if rows == nil {
		return nil
	}
	canonical, err := hasCanonicalHeader(path)
	if err != nil {
		return err
	}
	if canonical {
		return nil
	}
	return writeRows(path, rows)
}

func Validate(dir string, barsMap map[string][]data.DailyBar, costModel execution.CostModel) (int, error) {
	return ValidateWithExecution(dir, barsMap, costModel, execution.LiquidityPolicy{}, 0)
}

func ValidateWithExecution(dir string, barsMap map[string][]data.DailyBar, costModel execution.CostModel, liquidity execution.LiquidityPolicy, referenceEquity float64) (int, error) {
	if err := costModel.Validate(); err != nil {
		return 0, fmt.Errorf("前向测试交易成本配置无效: %w", err)
	}
	if err := liquidity.Validate(); err != nil {
		return 0, fmt.Errorf("前向测试流动性配置无效: %w", err)
	}
	if liquidity.Enabled && (liquidity.MaxParticipationPct > 0 || liquidity.ImpactCoefficient > 0) &&
		(referenceEquity <= 0 || math.IsNaN(referenceEquity) || math.IsInf(referenceEquity, 0)) {
		return 0, fmt.Errorf("前向测试启用成交占比或冲击成本时，参考权益必须为正有限数")
	}
	path := filepath.Join(dir, picksFile)
	if err := Migrate(dir); err != nil {
		return 0, err
	}
	rows, err := readRows(path)
	if err != nil {
		return 0, err
	}
	updated := 0
	dirty := false
	marketDates := marketTradingDates(barsMap)
	for _, row := range rows {
		code := row["code"]
		if code == "CASH" {
			rowUpdated, rowDirty := validateCashRow(row, barsMap, marketDates, costModel)
			updated += rowUpdated
			dirty = dirty || rowDirty
			continue
		}
		orderValue := forwardOrderValue(row, referenceEquity)
		forceCostRefresh, metadataChanged := syncCostModelWithExecution(row, costModel, liquidity, referenceEquity)
		dirty = dirty || metadataChanged
		if row["status"] == "invalid_break_prev_low" {
			row["status"] = "validated_1d"
			appendNote(row, "盘中跌破前一交易日低点（历史记录已迁移，保留开盘成交及后续收益）")
			dirty = true
		}
		targetDate := row["target_date"]
		bars := append([]data.DailyBar(nil), barsMap[code]...)
		if len(bars) == 0 || targetDate == "" {
			continue
		}
		sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
		targetIdx := indexByDate(bars, targetDate)
		if targetIdx < 0 {
			if len(marketDates) > 0 && targetDate <= marketDates[len(marketDates)-1] {
				if row["status"] != "no_trade_suspended" {
					row["status"] = "no_trade_suspended"
					appendNote(row, "目标市场交易日无个股行情，按停牌或不可交易处理")
					updated++
					dirty = true
				}
				rowUpdated, rowDirty := setForwardFields(row, managedExitFields("no_trade"))
				updated += rowUpdated
				dirty = dirty || rowDirty
			}
			continue
		}
		signalIdx := indexByDate(bars, row["signal_date"])
		if signalIdx < 0 && targetIdx > 0 {
			signalIdx = targetIdx - 1
		}
		if signalIdx < 0 {
			continue
		}
		entryAverageAmount := execution.AverageAmountCNY(bars, signalIdx, liquidity.AmountLookback)
		entryParticipation := execution.ParticipationPct(orderValue, entryAverageAmount)
		entryImpact := execution.EstimateImpactRate(orderValue, entryAverageAmount, liquidity)
		if setLiquidityEntryFields(row, liquidity, referenceEquity, orderValue, entryAverageAmount, entryParticipation, entryImpact) {
			dirty = true
		}
		if reason := forwardLiquidityRejectReason(row, liquidity, entryAverageAmount, entryParticipation); reason != "" {
			fields := noTradeLiquidityFields()
			fields["status"] = "no_trade_liquidity"
			rowUpdated, rowDirty := setForwardFields(row, fields)
			updated += rowUpdated
			dirty = dirty || rowDirty
			oldNotes := row["notes"]
			appendNote(row, reason)
			dirty = dirty || row["notes"] != oldNotes
			rowUpdated, rowDirty = validateManagedExitWithExecution(row, bars, marketDates, costModel, liquidity, orderValue, true)
			updated += rowUpdated
			dirty = dirty || rowDirty
			continue
		}

		if row["next_open"] == "" {
			open := bars[targetIdx].TradeOpen()
			close := bars[targetIdx].TradeClose()
			prevClose := bars[signalIdx].TradeClose()
			prevLow := bars[signalIdx].TradeLow()
			row["next_open"] = fmt.Sprintf("%.2f", open)
			row["next_close"] = fmt.Sprintf("%.2f", close)
			if bars[targetIdx].Vol <= 0 || open <= 0 {
				row["status"] = "no_trade_suspended"
				appendNote(row, "目标日停牌或开盘价无效，无法按计划买入")
			} else if !execution.CanBuyAtOpen(bars[signalIdx], bars[targetIdx], 0) {
				row["status"] = "no_trade_limit_up"
				appendNote(row, "目标日开盘涨停，无法按计划买入")
			} else if prevClose > 0 && open > prevClose*1.03 {
				row["status"] = "no_trade_gap"
				appendNote(row, "高开超过3%，按计划放弃")
			} else {
				exitImpact := impactBeforeIndex(bars, targetIdx, scaledOrderValue(orderValue, open, close), liquidity)
				if !writeReturnBreakdownWithImpact(row, "next_return_pct", "next_cost_pct", "next_net_return_pct", open, close, costModel, entryImpact, exitImpact) {
					row["status"] = "no_trade_invalid_price"
					appendNote(row, "目标日开盘价或收盘价无效，无法验证收益")
				} else {
					row["status"] = "validated_1d"
					if prevLow > 0 && bars[targetIdx].TradeLow() < prevLow {
						appendNote(row, "盘中跌破前一交易日低点（已按开盘成交，保留收益）")
					}
				}
			}
			updated++
			dirty = true
		} else if !isNoTradeStatus(row["status"]) && (forceCostRefresh || row["next_net_return_pct"] == "" || row["next_cost_pct"] == "") {
			open := parseFloat(row["next_open"])
			close := parseFloat(row["next_close"])
			exitImpact := impactBeforeIndex(bars, targetIdx, scaledOrderValue(orderValue, open, close), liquidity)
			if writeReturnBreakdownWithImpact(row, "next_return_pct", "next_cost_pct", "next_net_return_pct", open, close, costModel, entryImpact, exitImpact) {
				if row["status"] == "" || row["status"] == "pending" {
					row["status"] = "validated_1d"
				}
				updated++
				dirty = true
			}
		}
		if isNoTradeStatus(row["status"]) {
			rowUpdated, rowDirty := validateManagedExitWithExecution(row, bars, marketDates, costModel, liquidity, orderValue, forceCostRefresh)
			updated += rowUpdated
			dirty = dirty || rowDirty
			continue
		}
		rowUpdated, rowDirty := validateHorizonReturns(row, bars, targetIdx, costModel, liquidity, orderValue, entryImpact, forceCostRefresh)
		updated += rowUpdated
		dirty = dirty || rowDirty
		rowUpdated, rowDirty = validateManagedExitWithExecution(row, bars, marketDates, costModel, liquidity, orderValue, forceCostRefresh)
		updated += rowUpdated
		dirty = dirty || rowDirty
	}
	if !dirty {
		return 0, nil
	}
	return updated, writeRows(path, rows)
}

func validateCashRow(row map[string]string, barsMap map[string][]data.DailyBar, marketDates []string, costModel execution.CostModel) (int, bool) {
	if len(marketDates) == 0 || row["target_date"] == "" {
		return 0, false
	}
	targetIdx := firstDateOnOrAfter(marketDates, row["target_date"])
	if targetIdx < 0 {
		return 0, false
	}

	updated := 0
	dirty := false
	forceCostRefresh, metadataChanged := syncCostModel(row, costModel)
	dirty = dirty || metadataChanged
	entryDate := marketDates[targetIdx]
	if row["next_return_pct"] == "" || row["next_cost_pct"] == "" || row["next_net_return_pct"] == "" || forceCostRefresh {
		if result, ok := equalWeightMarketReturn(barsMap, entryDate, entryDate, costModel); ok {
			writeBreakdownValues(row, "next_return_pct", "next_cost_pct", "next_net_return_pct", result)
			row["status"] = "cash_validated_1d"
			appendNote(row, "空仓对照：等权市场代理当日收益")
			updated++
			dirty = true
		}
	}

	for _, target := range validationTargets(row["horizon"]) {
		if targetIdx+target.offset >= len(marketDates) || (row[target.returnField] != "" && row[target.costField] != "" && row[target.netReturnField] != "" && !forceCostRefresh) {
			continue
		}
		exitDate := marketDates[targetIdx+target.offset]
		if result, ok := equalWeightMarketReturn(barsMap, entryDate, exitDate, costModel); ok {
			writeBreakdownValues(row, target.returnField, target.costField, target.netReturnField, result)
			row["status"] = "cash_validated_" + target.label
			updated++
			dirty = true
		}
	}
	return updated, dirty
}

func marketTradingDates(barsMap map[string][]data.DailyBar) []string {
	seen := make(map[string]bool)
	for _, bars := range barsMap {
		for _, bar := range bars {
			if bar.TradeDate != "" {
				seen[bar.TradeDate] = true
			}
		}
	}
	dates := make([]string, 0, len(seen))
	for date := range seen {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	return dates
}

func equalWeightMarketReturn(barsMap map[string][]data.DailyBar, entryDate, exitDate string, costModel execution.CostModel) (execution.ReturnBreakdown, bool) {
	var total execution.ReturnBreakdown
	count := 0
	for _, bars := range barsMap {
		entryIdx := indexByDate(bars, entryDate)
		exitIdx := indexByDate(bars, exitDate)
		if entryIdx < 0 || exitIdx < 0 {
			continue
		}
		entry := bars[entryIdx].TradeOpen()
		exit := bars[exitIdx].TradeClose()
		if entry <= 0 || exit <= 0 {
			continue
		}
		result, ok := execution.RoundTripReturn(entry, exit, costModel)
		if !ok {
			continue
		}
		total.GrossReturnPct += result.GrossReturnPct
		total.CostImpactPct += result.CostImpactPct
		total.NetReturnPct += result.NetReturnPct
		count++
	}
	if count == 0 {
		return execution.ReturnBreakdown{}, false
	}
	total.GrossReturnPct /= float64(count)
	total.CostImpactPct /= float64(count)
	total.NetReturnPct /= float64(count)
	return total, true
}

func validateHorizonReturns(row map[string]string, bars []data.DailyBar, targetIdx int, costModel execution.CostModel, liquidity execution.LiquidityPolicy, orderValue, entryImpact float64, forceCostRefresh bool) (int, bool) {
	open := parseFloat(row["next_open"])
	if open <= 0 {
		return 0, false
	}
	updated := 0
	dirty := false
	for _, target := range validationTargets(row["horizon"]) {
		if targetIdx+target.offset >= len(bars) {
			continue
		}
		close := parseFloat(row[target.closeField])
		if close <= 0 {
			close = bars[targetIdx+target.offset].TradeClose()
			row[target.closeField] = fmt.Sprintf("%.2f", close)
		}
		if row[target.returnField] != "" && row[target.costField] != "" && row[target.netReturnField] != "" && !forceCostRefresh {
			continue
		}
		exitImpact := impactBeforeIndex(bars, targetIdx+target.offset, scaledOrderValue(orderValue, open, close), liquidity)
		if !writeReturnBreakdownWithImpact(row, target.returnField, target.costField, target.netReturnField, open, close, costModel, entryImpact, exitImpact) {
			continue
		}
		row["status"] = "validated_" + target.label
		updated++
		dirty = true
	}
	return updated, dirty
}

func validateManagedExit(row map[string]string, bars []data.DailyBar, marketDates []string, costModel execution.CostModel, forceCostRefresh bool) (int, bool) {
	return validateManagedExitWithExecution(row, bars, marketDates, costModel, execution.LiquidityPolicy{}, 0, forceCostRefresh)
}

func validateManagedExitWithExecution(row map[string]string, bars []data.DailyBar, marketDates []string, costModel execution.CostModel, liquidity execution.LiquidityPolicy, orderValue float64, forceCostRefresh bool) (int, bool) {
	forceRefresh := forceCostRefresh || row["exit_model"] != execution.ExitModelVersion
	if row["managed_exit_status"] == "completed" && !forceRefresh {
		return 0, false
	}
	horizon := strategy.Horizon(row["horizon"])
	outcome := execution.SimulateManagedExit(bars, marketDates, row["signal_date"], horizon, execution.SimulationOptions{
		Costs: costModel, MaxEntryGapPct: 3, Liquidity: liquidity, OrderValueCNY: orderValue,
	})
	fields := managedExitFields("pending")
	if !outcome.EntryFeasible {
		fields["managed_exit_status"] = "no_trade"
		return setForwardFields(row, fields)
	}
	if outcome.Triggered {
		fields["managed_exit_status"] = "pending_liquidity"
		fields["exit_trigger_date"] = outcome.TriggerDate
		fields["exit_reason"] = string(outcome.Reason)
		fields["exit_stop_price"] = formatOptionalFloat(outcome.StopPrice)
		fields["exit_holding_days"] = strconv.Itoa(outcome.HoldingDays)
		fields["exit_delay_days"] = strconv.Itoa(outcome.DelayDays)
	}
	if outcome.Completed {
		fields["managed_exit_status"] = "completed"
		fields["exit_date"] = outcome.ExitDate
		fields["exit_open"] = fmt.Sprintf("%.2f", outcome.ExitPrice)
		fields["exit_return_pct"] = fmt.Sprintf("%.2f", outcome.Returns.GrossReturnPct)
		fields["exit_cost_pct"] = fmt.Sprintf("%.2f", outcome.Returns.CostImpactPct)
		fields["exit_net_return_pct"] = fmt.Sprintf("%.2f", outcome.Returns.NetReturnPct)
		fields["exit_tail_loss"] = strconv.FormatBool(outcome.TailLoss)
		fields["entry_participation_pct"] = fmt.Sprintf("%.4f", outcome.EntryParticipationPct)
		fields["entry_impact_pct"] = fmt.Sprintf("%.4f", outcome.EntryImpactRate*100)
		fields["exit_participation_pct"] = fmt.Sprintf("%.4f", outcome.ExitParticipationPct)
		fields["exit_impact_pct"] = fmt.Sprintf("%.4f", outcome.ExitImpactRate*100)
	}
	return setForwardFields(row, fields)
}

func managedExitFields(status string) map[string]string {
	return map[string]string{
		"exit_model":             execution.ExitModelVersion,
		"managed_exit_status":    status,
		"exit_trigger_date":      "",
		"exit_reason":            "",
		"exit_stop_price":        "",
		"exit_holding_days":      "",
		"exit_date":              "",
		"exit_open":              "",
		"exit_delay_days":        "",
		"exit_return_pct":        "",
		"exit_cost_pct":          "",
		"exit_net_return_pct":    "",
		"exit_tail_loss":         "",
		"exit_participation_pct": "",
		"exit_impact_pct":        "",
	}
}

func setForwardFields(row map[string]string, fields map[string]string) (int, bool) {
	changed := false
	for field, value := range fields {
		if row[field] != value {
			row[field] = value
			changed = true
		}
	}
	if changed {
		return 1, true
	}
	return 0, false
}

func formatOptionalFloat(value float64) string {
	if value <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2f", value)
}

func syncCostModel(row map[string]string, costModel execution.CostModel) (forceRefresh bool, changed bool) {
	return syncCostModelWithExecution(row, costModel, execution.LiquidityPolicy{}, 0)
}

func syncCostModelWithExecution(row map[string]string, costModel execution.CostModel, liquidity execution.LiquidityPolicy, referenceEquity float64) (forceRefresh bool, changed bool) {
	commission := fmt.Sprintf("%.8f", costModel.Commission)
	slippage := fmt.Sprintf("%.8f", costModel.Slippage)
	model := execution.CostModelVersion
	liquidityModel := ""
	liquidityPolicy := ""
	if liquidity.Enabled {
		model = execution.ImpactCostModelVersion
		liquidityModel = execution.LiquidityModelVersion
		liquidityPolicy = liquidityPolicyKey(liquidity, referenceEquity)
	}
	if row["cost_model"] == model && row["commission_rate"] == commission && row["slippage_rate"] == slippage &&
		row["liquidity_model"] == liquidityModel && row["liquidity_policy"] == liquidityPolicy {
		return false, false
	}
	hadCostModel := row["cost_model"] != "" || row["commission_rate"] != "" || row["slippage_rate"] != "" || row["liquidity_policy"] != ""
	row["cost_model"] = model
	row["commission_rate"] = commission
	row["slippage_rate"] = slippage
	row["liquidity_model"] = liquidityModel
	row["liquidity_policy"] = liquidityPolicy
	if hadCostModel {
		appendNote(row, fmt.Sprintf("交易成本口径已重算：手续费%.4f%%/边，滑点%.4f%%/边，含流动性冲击=%t", costModel.Commission*100, costModel.Slippage*100, liquidity.Enabled))
	}
	return true, true
}

func writeReturnBreakdown(row map[string]string, grossField, costField, netField string, entry, exit float64, costModel execution.CostModel) bool {
	return writeReturnBreakdownWithImpact(row, grossField, costField, netField, entry, exit, costModel, 0, 0)
}

func writeReturnBreakdownWithImpact(row map[string]string, grossField, costField, netField string, entry, exit float64, costModel execution.CostModel, entryImpact, exitImpact float64) bool {
	result, ok := execution.RoundTripReturnWithImpact(entry, exit, costModel, entryImpact, exitImpact)
	if !ok {
		return false
	}
	writeBreakdownValues(row, grossField, costField, netField, result)
	return true
}

func writeBreakdownValues(row map[string]string, grossField, costField, netField string, result execution.ReturnBreakdown) {
	row[grossField] = fmt.Sprintf("%.2f", result.GrossReturnPct)
	row[costField] = fmt.Sprintf("%.2f", result.CostImpactPct)
	row[netField] = fmt.Sprintf("%.2f", result.NetReturnPct)
}

func forwardOrderValue(row map[string]string, referenceEquity float64) float64 {
	positionPct := parseFloat(row["position_pct"])
	if referenceEquity > 0 && positionPct > 0 {
		return referenceEquity * positionPct / 100
	}
	return parseFloat(row["estimated_order_value_cny"])
}

func setLiquidityEntryFields(row map[string]string, liquidity execution.LiquidityPolicy, referenceEquity, orderValue, averageAmount, participation, impact float64) bool {
	if !liquidity.Enabled {
		return false
	}
	fields := map[string]string{
		"liquidity_model":           execution.LiquidityModelVersion,
		"liquidity_policy":          liquidityPolicyKey(liquidity, referenceEquity),
		"estimated_order_value_cny": fmt.Sprintf("%.2f", orderValue),
		"average_amount_cny":        fmt.Sprintf("%.2f", averageAmount),
		"entry_participation_pct":   fmt.Sprintf("%.4f", participation),
		"entry_impact_pct":          fmt.Sprintf("%.4f", impact*100),
	}
	changed := false
	for key, value := range fields {
		if row[key] != value {
			row[key] = value
			changed = true
		}
	}
	return changed
}

func liquidityPolicyKey(policy execution.LiquidityPolicy, referenceEquity float64) string {
	return fmt.Sprintf("%t|%d|%d|%.8g|%.8g|%t|%.8g|%.8g|%.8g|%.8g",
		policy.Enabled, policy.MinListingDays, policy.AmountLookback, policy.MinAverageAmountCNY,
		policy.MinTurnoverRatePct, policy.RequireTurnoverData, policy.MaxParticipationPct,
		policy.ImpactCoefficient, policy.MaxImpactRate, referenceEquity)
}

func impactBeforeIndex(bars []data.DailyBar, exitIdx int, orderValue float64, liquidity execution.LiquidityPolicy) float64 {
	if !liquidity.Enabled || exitIdx <= 0 || exitIdx > len(bars) {
		return 0
	}
	averageAmount := execution.AverageAmountCNY(bars, exitIdx-1, liquidity.AmountLookback)
	return execution.EstimateImpactRate(orderValue, averageAmount, liquidity)
}

func scaledOrderValue(orderValue, entryPrice, exitPrice float64) float64 {
	if orderValue <= 0 || entryPrice <= 0 || exitPrice <= 0 {
		return orderValue
	}
	return orderValue * exitPrice / entryPrice
}

func formatForwardOptional(value float64, present bool) string {
	if !present {
		return ""
	}
	return fmt.Sprintf("%.4f", value)
}

func forwardLiquidityRejectReason(row map[string]string, policy execution.LiquidityPolicy, averageAmount, participation float64) string {
	if !policy.Enabled {
		return ""
	}
	if execution.IsSTName(row["name"]) {
		return "信号记录为 ST 股票，按流动性规则放弃"
	}
	listingDays := parseFloat(row["listing_days"])
	if policy.MinListingDays > 0 && row["listing_days"] != "" && listingDays >= 0 && listingDays < float64(policy.MinListingDays) {
		return fmt.Sprintf("上市时间不足%d天，按流动性规则放弃", policy.MinListingDays)
	}
	if policy.MinAverageAmountCNY > 0 && averageAmount <= 0 {
		return "信号日前缺少有效成交额，按流动性规则放弃"
	}
	if policy.MinAverageAmountCNY > 0 && averageAmount < policy.MinAverageAmountCNY {
		return fmt.Sprintf("日均成交额%.0f万元低于下限%.0f万元，按流动性规则放弃", averageAmount/10_000, policy.MinAverageAmountCNY/10_000)
	}
	turnover := parseFloat(row["turnover_rate_pct"])
	if policy.MinTurnoverRatePct > 0 && row["turnover_rate_pct"] != "" && turnover < policy.MinTurnoverRatePct {
		return fmt.Sprintf("换手率%.2f%%低于下限%.2f%%，按流动性规则放弃", turnover, policy.MinTurnoverRatePct)
	}
	if policy.MinTurnoverRatePct > 0 && policy.RequireTurnoverData && row["turnover_rate_pct"] == "" {
		return "信号记录缺少换手率，按流动性规则放弃"
	}
	if policy.MaxParticipationPct > 0 && participation > policy.MaxParticipationPct {
		return fmt.Sprintf("预计成交占比%.2f%%超过上限%.2f%%，按流动性规则放弃", participation, policy.MaxParticipationPct)
	}
	return ""
}

func noTradeLiquidityFields() map[string]string {
	fields := map[string]string{
		"next_return_pct": "", "next_cost_pct": "", "next_net_return_pct": "",
	}
	for _, target := range validationTargets("") {
		fields[target.closeField] = ""
		fields[target.returnField] = ""
		fields[target.costField] = ""
		fields[target.netReturnField] = ""
	}
	for _, horizon := range []string{string(strategy.HorizonMid), string(strategy.HorizonLong)} {
		for _, target := range validationTargets(horizon) {
			fields[target.closeField] = ""
			fields[target.returnField] = ""
			fields[target.costField] = ""
			fields[target.netReturnField] = ""
		}
	}
	return fields
}

func readRows(path string) ([]map[string]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	var rows []map[string]string
	for _, rec := range records[1:] {
		row := make(map[string]string)
		for _, h := range headers {
			row[h] = ""
		}
		rowHeaders := records[0]
		if (sameHeader(records[0], legacyHeaders) || sameHeader(records[0], previousHeaders) || sameHeader(records[0], preVoteHeaders) || sameHeader(records[0], preCostHeaders) || sameHeader(records[0], preExitHeaders) || sameHeader(records[0], preLiquidityHeaders)) && len(rec) == len(headers) {
			rowHeaders = headers
		}
		for i, v := range rec {
			if i < len(rowHeaders) && rowHeaders[i] != "" {
				row[rowHeaders[i]] = v
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func appendRows(path string, rows []map[string]string) error {
	if err := migratePath(path); err != nil {
		return err
	}
	exists := false
	if _, err := os.Stat(path); err == nil {
		exists = true
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	defer writer.Flush()
	if !exists {
		if err := writer.Write(headers); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if err := writer.Write(rowValues(row)); err != nil {
			return err
		}
	}
	return writer.Error()
}

func hasCanonicalHeader(path string) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	reader := csv.NewReader(f)
	header, err := reader.Read()
	if err == io.EOF {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return sameHeader(header, headers), nil
}

func migratePath(path string) error {
	dir := filepath.Dir(path)
	rows, err := readRows(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	canonical, err := hasCanonicalHeader(path)
	if err != nil {
		return err
	}
	if canonical {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return writeRows(path, rows)
}

func sameHeader(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeRows(path string, rows []map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	defer writer.Flush()
	if err := writer.Write(headers); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writer.Write(rowValues(row)); err != nil {
			return err
		}
	}
	return writer.Error()
}

func rowValues(row map[string]string) []string {
	values := make([]string, len(headers))
	for i, h := range headers {
		values[i] = row[h]
	}
	return values
}

func writeDailyMarkdown(dir, signalDate, targetDate string, picks []signal.SignalResult, marketStatus *market.MarketStatus) error {
	path := filepath.Join(dir, fmt.Sprintf("%s_for_%s.md", signalDate, targetDate))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "# %s Signal For %s\n\n", signalDate, targetDate)
	fmt.Fprintln(f, "## Run Context")
	fmt.Fprintln(f)
	fmt.Fprintf(f, "- 数据最新交易日：%s\n", signalDate)
	fmt.Fprintf(f, "- 建议验证日：%s\n", targetDate)
	fmt.Fprintf(f, "- 市场情绪：%s\n", marketSentiment(marketStatus))
	fmt.Fprintf(f, "- 仓位建议：%s\n\n", marketAdvice(marketStatus))
	fmt.Fprintln(f, "## Buy Watchlist")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "| Rank | Code | Name | Close | Raw Signals | Effective Votes | Score | Confidence | Position | Risk | Key Strategies |")
	fmt.Fprintln(f, "|---:|---|---|---:|---:|---:|---:|---:|---:|---|---|")
	for i, pick := range picks {
		fmt.Fprintf(f, "| %d | %s | %s | %.2f | %d买/%d卖 | %.2f买/%d组 | %.2f | %.0f | %.1f%% | %s | %s |\n",
			i+1, pick.Code, pick.Name, pick.Close, pick.BuyCount, pick.SellCount,
			pick.EffectiveBuyVotes, pick.BuyGroupCount, pick.TotalScore, pick.Confidence, pick.PositionPct,
			signal.RiskPolicySummary(pick), strings.Join(buyStrategies(pick), ", "))
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "## Validation Plan")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "- 回填目标日开盘价，并按信号周期记录各观察期收盘价。")
	fmt.Fprintln(f, "- 每个观察期同时记录毛收益、双边手续费/滑点/流动性冲击造成的成本拖累和净收益；参数读取运行时配置。")
	fmt.Fprintln(f, "- 另行跟踪初始止损、ATR 移动止损和时间止损；收盘确认、下一可交易开盘退出，并记录停牌/跌停延迟。")
	fmt.Fprintln(f, "- 信号日流动性不合格或预计成交占比超限时视为未成交；退出不会仅因成交占比过高而永久阻断。")
	fmt.Fprintln(f, "- 高开超过 3% 时视为未成交；开盘成交后若盘中跌破前一交易日低点，只记录风险并保留后续收益。")
	return nil
}

func writeCashMarkdown(dir, signalDate, targetDate string, decision signal.PositionDecision, marketStatus *market.MarketStatus) error {
	path := filepath.Join(dir, fmt.Sprintf("%s_for_%s.md", signalDate, targetDate))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "# %s Signal For %s\n\n", signalDate, targetDate)
	fmt.Fprintln(f, "## Run Context")
	fmt.Fprintln(f)
	fmt.Fprintf(f, "- 数据最新交易日：%s\n", signalDate)
	fmt.Fprintf(f, "- 建议验证日：%s\n", targetDate)
	fmt.Fprintf(f, "- 市场情绪：%s\n", marketSentiment(marketStatus))
	fmt.Fprintf(f, "- 仓位建议：%s\n\n", marketAdvice(marketStatus))
	fmt.Fprintln(f, "## Position Decision")
	fmt.Fprintln(f)
	fmt.Fprintf(f, "- 策略状态：%s\n", decision.Action)
	fmt.Fprintf(f, "- 买入候选：%d，合格候选：%d，过滤：%d\n", decision.CandidateBuys, decision.QualifiedBuys, decision.SuppressedBuys)
	if len(decision.Reasons) > 0 {
		fmt.Fprintf(f, "- 触发原因：%s\n", strings.Join(decision.Reasons, "；"))
	}
	fmt.Fprintf(f, "- 执行建议：%s\n\n", decision.Advice)
	fmt.Fprintln(f, "## Validation Plan")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "- `picks.csv` 记录 `CASH`，表示该日不新增买入。")
	fmt.Fprintln(f, "- 后续验证以等权市场代理作为反事实，并同时记录其毛收益、交易成本拖累和净收益，判断空仓是否规避了亏损。")
	return nil
}

func buyStrategies(r signal.SignalResult) []string {
	var names []string
	for name, detail := range r.Strategies {
		if detail.Signal.String() == "BUY" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func entryPlan(r signal.SignalResult) string {
	for _, label := range r.RiskLabels {
		if label == "5日涨幅过高" || label == "20日涨幅过高" {
			return "只接受低吸"
		}
	}
	return "平开或低吸，避免追高"
}

func marketSentiment(ms *market.MarketStatus) string {
	if ms == nil || ms.Sentiment == "" {
		return "未知"
	}
	return ms.Sentiment
}

func marketAdvice(ms *market.MarketStatus) string {
	if ms == nil || ms.Advice == "" {
		return "未知"
	}
	return ms.Advice
}

func nextTradingDate(date string, tradingDates []string) string {
	for _, d := range tradingDates {
		if d > date {
			return d
		}
	}
	t, err := time.Parse("20060102", date)
	if err != nil {
		return date
	}
	for {
		t = t.AddDate(0, 0, 1)
		if t.Weekday() != time.Saturday && t.Weekday() != time.Sunday {
			return t.Format("20060102")
		}
	}
}

func firstDateOnOrAfter(dates []string, date string) int {
	for i, candidate := range dates {
		if candidate >= date {
			return i
		}
	}
	return -1
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func indexByDate(bars []data.DailyBar, date string) int {
	for i, b := range bars {
		if b.TradeDate == date {
			return i
		}
	}
	return -1
}

func isNoTradeStatus(status string) bool {
	return strings.HasPrefix(status, "no_trade")
}

func appendNote(row map[string]string, note string) {
	for _, existing := range strings.Split(row["notes"], ";") {
		if existing == note {
			return
		}
	}
	if row["notes"] == "" {
		row["notes"] = note
		return
	}
	if !strings.Contains(row["notes"], note) {
		row["notes"] += ";" + note
	}
}
