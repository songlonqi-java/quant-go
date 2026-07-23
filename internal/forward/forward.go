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
)

const picksFile = "picks.csv"

var headers = []string{
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

func Record(dir string, results []signal.SignalResult, marketStatus *market.MarketStatus, limit int, tradingDates []string) error {
	if limit <= 0 {
		limit = 5
	}
	var picks []signal.SignalResult
	for _, r := range results {
		if r.Recommendation() == "买入" {
			picks = append(picks, r)
		}
		if len(picks) >= limit {
			break
		}
	}
	if len(picks) == 0 {
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
		seen[row["signal_date"]+"|"+row["target_date"]+"|"+row["code"]] = true
	}

	var rows []map[string]string
	for _, pick := range picks {
		key := signalDate + "|" + targetDate + "|" + pick.Code
		if seen[key] {
			continue
		}
		rows = append(rows, map[string]string{
			"signal_date":       signalDate,
			"target_date":       targetDate,
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
	for _, row := range rows {
		code := row["code"]
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
			if prevClose > 0 && open > prevClose*1.03 {
				row["status"] = "no_trade_gap"
				appendNote(row, "高开超过3%，按计划放弃")
			} else {
				row["next_return_pct"] = fmt.Sprintf("%.2f", returnPct(open, close))
				row["status"] = "validated_1d"
				if prevLow > 0 && bars[targetIdx].TradeLow() < prevLow {
					row["status"] = "invalid_break_prev_low"
					appendNote(row, "盘中跌破前一交易日低点")
				}
			}
			updated++
		}
		if isNoTradeStatus(row["status"]) {
			continue
		}
		if targetIdx+2 < len(bars) && row["day3_close"] == "" {
			open := parseFloat(row["next_open"])
			close := bars[targetIdx+2].TradeClose()
			row["day3_close"] = fmt.Sprintf("%.2f", close)
			row["day3_return_pct"] = fmt.Sprintf("%.2f", returnPct(open, close))
			row["status"] = "validated_3d"
			updated++
		}
		if targetIdx+4 < len(bars) && row["day5_close"] == "" {
			open := parseFloat(row["next_open"])
			close := bars[targetIdx+4].TradeClose()
			row["day5_close"] = fmt.Sprintf("%.2f", close)
			row["day5_return_pct"] = fmt.Sprintf("%.2f", returnPct(open, close))
			row["status"] = "validated_5d"
			updated++
		}
	}
	if updated == 0 {
		return 0, nil
	}
	return updated, writeRows(path, rows)
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
		if sameHeader(records[0], legacyHeaders) && len(rec) == len(headers) {
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
	fmt.Fprintln(f, "- 高开超过 3% 或跌破前一交易日低点时标记为未触发/失效。")
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
