package forward

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"quant/internal/data"
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
			"signal_date":       signalDate,
			"target_date":       targetDate,
			"horizon":           string(pick.Horizon),
			"rank":              strconv.Itoa(len(rows) + 1),
			"code":              pick.Code,
			"name":              pick.Name,
			"close":             fmt.Sprintf("%.2f", pick.Close),
			"buy_signals":       strconv.Itoa(pick.BuyCount),
			"sell_signals":      strconv.Itoa(pick.SellCount),
			"total_score":       fmt.Sprintf("%.2f", pick.TotalScore),
			"confidence":        fmt.Sprintf("%.0f", pick.Confidence),
			"position_pct":      fmt.Sprintf("%.1f", pick.PositionPct),
			"key_strategies":    strings.Join(buyStrategies(pick), ";"),
			"market_status":     marketSentiment(marketStatus),
			"position_advice":   marketAdvice(marketStatus),
			"benchmark":         "",
			"entry_plan":        entryPlan(pick),
			"invalid_condition": "高开>3%或跌破前日低点",
			"status":            "pending",
			"notes":             strings.Join(append(pick.RiskLabels, pick.Reasons...), ";"),
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
		"signal_date":       signalDate,
		"target_date":       targetDate,
		"horizon":           "short",
		"rank":              "0",
		"code":              "CASH",
		"name":              string(decision.Action),
		"close":             "0.00",
		"buy_signals":       "0",
		"sell_signals":      "0",
		"total_score":       "0.00",
		"confidence":        "0",
		"position_pct":      "0.0",
		"key_strategies":    "cash",
		"market_status":     marketSentiment(marketStatus),
		"position_advice":   decision.Advice,
		"benchmark":         "MARKET_PROXY_EQUAL_WEIGHT",
		"entry_plan":        decision.Advice,
		"invalid_condition": "市场转强且出现合格候选才解除空仓",
		"status":            "cash",
		"notes":             strings.Join(decision.Reasons, ";"),
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

func Validate(dir string, barsMap map[string][]data.DailyBar) (int, error) {
	path := filepath.Join(dir, picksFile)
	if err := Migrate(dir); err != nil {
		return 0, err
	}
	rows, err := readRows(path)
	if err != nil {
		return 0, err
	}
	updated := 0
	marketDates := marketTradingDates(barsMap)
	for _, row := range rows {
		code := row["code"]
		if code == "CASH" {
			updated += validateCashRow(row, barsMap, marketDates)
			continue
		}
		targetDate := row["target_date"]
		bars := append([]data.DailyBar(nil), barsMap[code]...)
		if len(bars) == 0 || targetDate == "" {
			continue
		}
		sort.Slice(bars, func(i, j int) bool { return bars[i].TradeDate < bars[j].TradeDate })
		targetIdx := firstIndexOnOrAfter(bars, targetDate)
		if targetIdx < 0 {
			continue
		}
		signalIdx := indexByDate(bars, row["signal_date"])
		if signalIdx < 0 && targetIdx > 0 {
			signalIdx = targetIdx - 1
		}
		if signalIdx < 0 {
			continue
		}

		if row["next_open"] == "" {
			open := bars[targetIdx].TradeOpen()
			close := bars[targetIdx].TradeClose()
			prevClose := bars[signalIdx].TradeClose()
			prevLow := bars[signalIdx].TradeLow()
			row["next_open"] = fmt.Sprintf("%.2f", open)
			row["next_close"] = fmt.Sprintf("%.2f", close)
			if bars[targetIdx].IsLimitUpOpenWithFallback(prevClose) {
				row["status"] = "no_trade_limit_up"
				appendNote(row, "目标日开盘涨停，无法按计划买入")
			} else if prevClose > 0 && open > prevClose*1.03 {
				row["status"] = "no_trade_gap"
				appendNote(row, "高开超过3%，按计划放弃")
			} else {
				row["next_return_pct"] = fmt.Sprintf("%.2f", returnPct(open, close))
				row["status"] = "validated_1d"
				if prevLow > 0 && bars[targetIdx].TradeLow() < prevLow {
					appendNote(row, "盘中跌破前一交易日低点（已按开盘成交，保留收益）")
				}
			}
			updated++
		}
		if isNoTradeStatus(row["status"]) {
			continue
		}
		updated += validateHorizonReturns(row, bars, targetIdx)
	}
	if updated == 0 {
		return 0, nil
	}
	return updated, writeRows(path, rows)
}

func validateCashRow(row map[string]string, barsMap map[string][]data.DailyBar, marketDates []string) int {
	if len(marketDates) == 0 || row["target_date"] == "" {
		return 0
	}
	targetIdx := firstDateOnOrAfter(marketDates, row["target_date"])
	if targetIdx < 0 {
		return 0
	}

	updated := 0
	entryDate := marketDates[targetIdx]
	if row["next_return_pct"] == "" {
		if ret, ok := equalWeightMarketReturn(barsMap, entryDate, entryDate); ok {
			row["next_return_pct"] = fmt.Sprintf("%.2f", ret)
			row["status"] = "cash_validated_1d"
			appendNote(row, "空仓对照：等权市场代理当日收益")
			updated++
		}
	}

	for _, target := range validationTargets(row["horizon"]) {
		if targetIdx+target.offset >= len(marketDates) || row[target.returnField] != "" {
			continue
		}
		exitDate := marketDates[targetIdx+target.offset]
		if ret, ok := equalWeightMarketReturn(barsMap, entryDate, exitDate); ok {
			row[target.returnField] = fmt.Sprintf("%.2f", ret)
			row["status"] = "cash_validated_" + target.label
			updated++
		}
	}
	return updated
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

func equalWeightMarketReturn(barsMap map[string][]data.DailyBar, entryDate, exitDate string) (float64, bool) {
	var total float64
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
		total += returnPct(entry, exit)
		count++
	}
	if count == 0 {
		return 0, false
	}
	return total / float64(count), true
}

func validateHorizonReturns(row map[string]string, bars []data.DailyBar, targetIdx int) int {
	open := parseFloat(row["next_open"])
	if open <= 0 {
		return 0
	}
	updated := 0
	for _, target := range validationTargets(row["horizon"]) {
		if targetIdx+target.offset >= len(bars) || row[target.closeField] != "" {
			continue
		}
		close := bars[targetIdx+target.offset].TradeClose()
		row[target.closeField] = fmt.Sprintf("%.2f", close)
		row[target.returnField] = fmt.Sprintf("%.2f", returnPct(open, close))
		row["status"] = "validated_" + target.label
		updated++
	}
	return updated
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
		if (sameHeader(records[0], legacyHeaders) || sameHeader(records[0], previousHeaders)) && len(rec) == len(headers) {
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
	fmt.Fprintln(f, "| Rank | Code | Name | Close | Signals | Score | Confidence | Position | Risk | Key Strategies |")
	fmt.Fprintln(f, "|---:|---|---|---:|---:|---:|---:|---:|---|---|")
	for i, pick := range picks {
		fmt.Fprintf(f, "| %d | %s | %s | %.2f | %d买/%d卖 | %.2f | %.0f | %.1f%% | %s | %s |\n",
			i+1, pick.Code, pick.Name, pick.Close, pick.BuyCount, pick.SellCount,
			pick.TotalScore, pick.Confidence, pick.PositionPct,
			strings.Join(pick.RiskLabels, ","), strings.Join(buyStrategies(pick), ", "))
	}
	fmt.Fprintln(f)
	fmt.Fprintln(f, "## Validation Plan")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "- 回填次日开盘价、收盘价和次日收益。")
	fmt.Fprintln(f, "- 回填 3 日和 5 日收盘收益。")
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
	fmt.Fprintln(f, "- 后续验证时重点比较目标日市场和原始候选表现，判断空仓是否规避了亏损。")
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

func firstIndexOnOrAfter(bars []data.DailyBar, date string) int {
	for i, b := range bars {
		if b.TradeDate >= date {
			return i
		}
	}
	return -1
}

func firstDateOnOrAfter(dates []string, date string) int {
	for i, candidate := range dates {
		if candidate >= date {
			return i
		}
	}
	return -1
}

func returnPct(from, to float64) float64 {
	if from <= 0 {
		return 0
	}
	return (to/from - 1) * 100
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
	return strings.HasPrefix(status, "no_trade") || strings.HasPrefix(status, "invalid_")
}

func appendNote(row map[string]string, note string) {
	if row["notes"] == "" {
		row["notes"] = note
		return
	}
	if !strings.Contains(row["notes"], note) {
		row["notes"] += ";" + note
	}
}
