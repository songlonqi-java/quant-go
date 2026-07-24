package signal

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"quant/internal/strategy"
)

type Format int

const (
	FormatTable Format = iota
	FormatCSV
	FormatJSON
)

type Reporter struct {
	Format Format
}

func NewReporter(format string) *Reporter {
	switch strings.ToLower(format) {
	case "json":
		return &Reporter{Format: FormatJSON}
	case "csv":
		return &Reporter{Format: FormatCSV}
	default:
		return &Reporter{Format: FormatTable}
	}
}

func (r *Reporter) Print(results []SignalResult) error {
	switch r.Format {
	case FormatJSON:
		return r.printJSON(results)
	case FormatCSV:
		return r.printCSV(results)
	default:
		r.printTable(results)
		return nil
	}
}

func (r *Reporter) printTable(results []SignalResult) {
	if len(results) == 0 {
		fmt.Println("无信号产生")
		return
	}

	totalBuy := 0
	totalSell := 0
	for _, horizon := range strategy.HorizonOrder() {
		horizonResults := filterByHorizon(results, horizon)
		if len(horizonResults) == 0 {
			continue
		}
		buyResults := filterByRecommendation(horizonResults, "买入")
		sellResults := filterByRecommendation(horizonResults, "卖出")
		totalBuy += len(buyResults)
		totalSell += len(sellResults)

		fmt.Printf("\n========== %s信号：%s ==========\n", strategy.HorizonLabel(horizon), strategy.HorizonDescription(horizon))
		printRecommendationTable("买入候选", buyResults, false)
		printRecommendationTable("卖出/回避", sellResults, true)
	}

	fmt.Printf("\n总计: %d 条信号 (买入 %d, 卖出 %d)\n", len(results), totalBuy, totalSell)
}

func (r *Reporter) printCSV(results []SignalResult) error {
	w := csv.NewWriter(os.Stdout)
	w.Write([]string{"排名", "周期", "代码", "名称", "日期", "收盘价", "实时价", "盘中涨跌%", "实时更新时间", "盘中标签", "买入信号", "卖出信号", "综合评分", "置信度", "建议仓位", "资金净额(万)", "大单净额(万)", "风险标签", "是否过滤", "过滤原因", "建议", "触发策略"})
	for i, res := range results {
		w.Write([]string{
			fmt.Sprintf("%d", i+1),
			string(res.Horizon),
			res.Code,
			res.Name,
			res.Date,
			fmt.Sprintf("%.2f", res.Close),
			formatCSVAmount(res.HasRealtime, res.RealtimePrice),
			formatCSVAmount(res.HasRealtime, res.RealtimeChangePct),
			res.RealtimeUpdateAt,
			strings.Join(res.IntradayLabels, ";"),
			fmt.Sprintf("%d", res.BuyCount),
			fmt.Sprintf("%d", res.SellCount),
			fmt.Sprintf("%.2f", res.TotalScore),
			fmt.Sprintf("%.0f", res.Confidence),
			fmt.Sprintf("%.1f%%", res.PositionPct),
			formatCSVAmount(res.HasMoneyflow, res.MoneyflowNetAmount),
			formatCSVAmount(res.HasMoneyflow, res.LargeMoneyflowNetAmount),
			strings.Join(res.RiskLabels, ";"),
			fmt.Sprintf("%t", res.Suppressed),
			res.SuppressionReason,
			res.Recommendation(),
			formatActiveStrategies(res.Strategies),
		})
	}
	w.Flush()
	return nil
}

func (r *Reporter) printJSON(results []SignalResult) error {
	type jsonOutput struct {
		Rank           int                `json:"rank"`
		Horizon        strategy.Horizon   `json:"horizon"`
		HorizonLabel   string             `json:"horizon_label"`
		Code           string             `json:"code"`
		Name           string             `json:"name"`
		Date           string             `json:"date"`
		Close          float64            `json:"close"`
		BuySignals     int                `json:"buy_signals"`
		SellSignals    int                `json:"sell_signals"`
		TotalScore     float64            `json:"total_score"`
		RawScore       float64            `json:"raw_score"`
		Confidence     float64            `json:"confidence"`
		PositionPct    float64            `json:"position_pct"`
		HasMoneyflow   bool               `json:"has_moneyflow"`
		MoneyflowNet   float64            `json:"moneyflow_net_amount"`
		LargeNet       float64            `json:"large_moneyflow_net_amount"`
		HasRealtime    bool               `json:"has_realtime"`
		RealtimePrice  float64            `json:"realtime_price"`
		RealtimePct    float64            `json:"realtime_change_pct"`
		RealtimeAt     string             `json:"realtime_update_at"`
		IntradayLabels []string           `json:"intraday_labels"`
		Suppressed     bool               `json:"suppressed"`
		SuppressReason string             `json:"suppression_reason"`
		RiskLabels     []string           `json:"risk_labels"`
		Reasons        []string           `json:"reasons"`
		Recommendation string             `json:"recommendation"`
		Strategies     map[string]string  `json:"strategies"`
		GroupScores    map[string]float64 `json:"group_scores"`
	}

	var output []jsonOutput
	for i, r := range results {
		strats := make(map[string]string)
		for name, detail := range r.Strategies {
			strats[name] = detail.Signal.String()
		}
		output = append(output, jsonOutput{
			Rank:           i + 1,
			Horizon:        r.Horizon,
			HorizonLabel:   strategy.HorizonLabel(r.Horizon),
			Code:           r.Code,
			Name:           r.Name,
			Date:           r.Date,
			Close:          r.Close,
			BuySignals:     r.BuyCount,
			SellSignals:    r.SellCount,
			TotalScore:     r.TotalScore,
			RawScore:       r.RawScore,
			Confidence:     r.Confidence,
			PositionPct:    r.PositionPct,
			HasMoneyflow:   r.HasMoneyflow,
			MoneyflowNet:   r.MoneyflowNetAmount,
			LargeNet:       r.LargeMoneyflowNetAmount,
			HasRealtime:    r.HasRealtime,
			RealtimePrice:  r.RealtimePrice,
			RealtimePct:    r.RealtimeChangePct,
			RealtimeAt:     r.RealtimeUpdateAt,
			IntradayLabels: r.IntradayLabels,
			Suppressed:     r.Suppressed,
			SuppressReason: r.SuppressionReason,
			RiskLabels:     r.RiskLabels,
			Reasons:        r.Reasons,
			Recommendation: r.Recommendation(),
			Strategies:     strats,
			GroupScores:    r.GroupScores,
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func printRecommendationTable(title string, results []SignalResult, ascending bool) {
	if len(results) == 0 {
		fmt.Printf("\n%s: 无\n", title)
		return
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].TotalScore == results[j].TotalScore {
			return results[i].Code < results[j].Code
		}
		if ascending {
			return results[i].TotalScore < results[j].TotalScore
		}
		return results[i].TotalScore > results[j].TotalScore
	})

	fmt.Printf("\n%s\n", title)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "排名\t代码\t名称\t日期\t收盘价\t盘中\t买/卖\t评分\t置信度\t仓位\t资金\t风险\t触发策略")
	for i, r := range results {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%.2f\t%s\t%d/%d\t%.2f\t%.0f\t%.1f%%\t%s\t%s\t%s\n",
			i+1, r.Code, r.Name, r.Date, r.Close, formatRealtime(r), r.BuyCount, r.SellCount,
			r.TotalScore, r.Confidence, r.PositionPct, formatMoneyflow(r), strings.Join(r.RiskLabels, ","),
			formatActiveStrategies(r.Strategies))
	}
	w.Flush()
}

func formatRealtime(r SignalResult) string {
	if !r.HasRealtime {
		return "-"
	}
	value := fmt.Sprintf("%.2f(%+.2f%%)", r.RealtimePrice, r.RealtimeChangePct)
	if len(r.IntradayLabels) == 0 {
		return value
	}
	return value + "/" + strings.Join(r.IntradayLabels, ",")
}

func formatMoneyflow(r SignalResult) string {
	if !r.HasMoneyflow {
		return "-"
	}
	return fmt.Sprintf("净%+.0f/大%+.0f", r.MoneyflowNetAmount, r.LargeMoneyflowNetAmount)
}

func formatCSVAmount(ok bool, amount float64) string {
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.0f", amount)
}

func formatStrategies(s map[string]SignalDetail) string {
	var parts []string
	for name, detail := range s {
		parts = append(parts, fmt.Sprintf("%s:%s", name, detail.Signal.String()))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func formatActiveStrategies(s map[string]SignalDetail) string {
	var parts []string
	for name, detail := range s {
		if detail.Signal == strategy.Hold {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%s", name, detail.Signal.String()))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func filterByRecommendation(results []SignalResult, rec string) []SignalResult {
	var filtered []SignalResult
	for _, r := range results {
		if r.Recommendation() == rec {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func filterByHorizon(results []SignalResult, horizon strategy.Horizon) []SignalResult {
	var filtered []SignalResult
	for _, r := range results {
		if r.Horizon == horizon {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
