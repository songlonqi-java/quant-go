package signal

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
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

	buyResults := filterByRecommendation(results, "买入")
	sellResults := filterByRecommendation(results, "卖出")

	if len(buyResults) > 0 {
		fmt.Println("\n========== 买入建议 ==========")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "排名\t代码\t名称\t日期\t收盘价\t信号数\t综合评分\t建议")
		for i, r := range buyResults {
			strategies := formatStrategies(r.Strategies)
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%.2f\t%s\t%.2f\t%s\n",
				i+1, r.Code, r.Name, r.Date, r.Close, strategies, r.TotalScore, r.Recommendation())
		}
		w.Flush()
	}

	if len(sellResults) > 0 {
		fmt.Println("\n========== 卖出建议 ==========")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "排名\t代码\t名称\t日期\t收盘价\t信号数\t综合评分\t建议")
		sort.Slice(sellResults, func(i, j int) bool {
			return sellResults[i].TotalScore < sellResults[j].TotalScore
		})
		for i, r := range sellResults {
			strategies := formatStrategies(r.Strategies)
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%.2f\t%s\t%.2f\t%s\n",
				i+1, r.Code, r.Name, r.Date, r.Close, strategies, r.TotalScore, r.Recommendation())
		}
		w.Flush()
	}

	fmt.Printf("\n总计: %d 条信号 (买入 %d, 卖出 %d)\n", len(results), len(buyResults), len(sellResults))
}

func (r *Reporter) printCSV(results []SignalResult) error {
	w := csv.NewWriter(os.Stdout)
	w.Write([]string{"排名", "代码", "名称", "日期", "收盘价", "买入信号", "卖出信号", "综合评分", "建议", "策略详情"})
	for i, res := range results {
		w.Write([]string{
			fmt.Sprintf("%d", i+1),
			res.Code,
			res.Name,
			res.Date,
			fmt.Sprintf("%.2f", res.Close),
			fmt.Sprintf("%d", res.BuyCount),
			fmt.Sprintf("%d", res.SellCount),
			fmt.Sprintf("%.2f", res.TotalScore),
			res.Recommendation(),
			formatStrategies(res.Strategies),
		})
	}
	w.Flush()
	return nil
}

func (r *Reporter) printJSON(results []SignalResult) error {
	type jsonOutput struct {
		Rank           int                `json:"rank"`
		Code           string             `json:"code"`
		Name           string             `json:"name"`
		Date           string             `json:"date"`
		Close          float64            `json:"close"`
		BuySignals     int                `json:"buy_signals"`
		SellSignals    int                `json:"sell_signals"`
		TotalScore     float64            `json:"total_score"`
		Recommendation string             `json:"recommendation"`
		Strategies     map[string]string  `json:"strategies"`
	}

	var output []jsonOutput
	for i, r := range results {
		strats := make(map[string]string)
		for name, detail := range r.Strategies {
			strats[name] = detail.Signal.String()
		}
		output = append(output, jsonOutput{
			Rank:           i + 1,
			Code:           r.Code,
			Name:           r.Name,
			Date:           r.Date,
			Close:          r.Close,
			BuySignals:     r.BuyCount,
			SellSignals:    r.SellCount,
			TotalScore:     r.TotalScore,
			Recommendation: r.Recommendation(),
			Strategies:     strats,
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func formatStrategies(s map[string]SignalDetail) string {
	var parts []string
	for name, detail := range s {
		parts = append(parts, fmt.Sprintf("%s:%s", name, detail.Signal.String()))
	}
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
