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

type jsonOutput struct {
	Rank               int                 `json:"rank"`
	Horizon            strategy.Horizon    `json:"horizon"`
	HorizonLabel       string              `json:"horizon_label"`
	Code               string              `json:"code"`
	Name               string              `json:"name"`
	Date               string              `json:"date"`
	Close              float64             `json:"close"`
	BuySignals         int                 `json:"buy_signals"`
	SellSignals        int                 `json:"sell_signals"`
	BuyGroups          int                 `json:"buy_groups"`
	SellGroups         int                 `json:"sell_groups"`
	EffectiveBuyVotes  float64             `json:"effective_buy_votes"`
	EffectiveSellVotes float64             `json:"effective_sell_votes"`
	VoteMetricsApplied bool                `json:"vote_metrics_applied"`
	TotalScore         float64             `json:"total_score"`
	RawScore           float64             `json:"raw_score"`
	Confidence         float64             `json:"confidence"`
	PositionPct        float64             `json:"position_pct"`
	HasMoneyflow       bool                `json:"has_moneyflow"`
	MoneyflowNet       float64             `json:"moneyflow_net_amount"`
	LargeNet           float64             `json:"large_moneyflow_net_amount"`
	SectorName         string              `json:"sector_name,omitempty"`
	SectorTags         []string            `json:"sector_tags,omitempty"`
	SectorChg1         float64             `json:"sector_chg1,omitempty"`
	HasRealtime        bool                `json:"has_realtime"`
	RealtimePrice      float64             `json:"realtime_price"`
	RealtimePct        float64             `json:"realtime_change_pct"`
	RealtimeAt         string              `json:"realtime_update_at"`
	IntradayLabels     []string            `json:"intraday_labels"`
	Suppressed         bool                `json:"suppressed"`
	SuppressReason     string              `json:"suppression_reason"`
	RiskLabels         []string            `json:"risk_labels"`
	RiskEffects        []RiskEffect        `json:"risk_effects"`
	Reasons            []string            `json:"reasons"`
	WatchReason        string              `json:"watch_reason,omitempty"`
	Recommendation     string              `json:"recommendation"`
	Strategies         map[string]string   `json:"strategies"`
	GroupScores        map[string]float64  `json:"group_scores"`
	HistoricalEvidence *HistoricalEvidence `json:"historical_evidence,omitempty"`
	LiquidityModel     string              `json:"liquidity_model,omitempty"`
	LiquidityApplied   bool                `json:"liquidity_applied"`
	LiquidityEligible  bool                `json:"liquidity_eligible"`
	ListingDays        int                 `json:"listing_days,omitempty"`
	AverageAmountCNY   float64             `json:"average_amount_cny,omitempty"`
	HasTurnover        bool                `json:"has_turnover"`
	TurnoverRatePct    float64             `json:"turnover_rate_pct,omitempty"`
	OrderValueCNY      float64             `json:"estimated_order_value_cny,omitempty"`
	ParticipationPct   float64             `json:"participation_pct,omitempty"`
	ImpactPct          float64             `json:"estimated_impact_pct,omitempty"`
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

func (r *Reporter) PrintWithWatch(results []SignalResult, watchlist []SignalResult) error {
	switch r.Format {
	case FormatJSON:
		return r.printJSONWithWatch(results, watchlist)
	case FormatCSV:
		return r.printCSVWithWatch(results, watchlist)
	default:
		r.printTableWithWatch(results, watchlist)
		return nil
	}
}

func (r *Reporter) printTable(results []SignalResult) {
	r.printTableWithWatch(results, nil)
}

func (r *Reporter) printTableWithWatch(results []SignalResult, watchlist []SignalResult) {
	if len(results) == 0 {
		fmt.Println("无正式交易信号")
	} else {
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

		fmt.Printf("\n总计: %d 条正式信号 (买入 %d, 卖出 %d)\n", len(results), totalBuy, totalSell)
	}

	if len(watchlist) > 0 {
		printWatchTable("观察机会（不进入正式买入，需人工确认）", watchlist)
		fmt.Printf("\n观察机会: %d 条\n", len(watchlist))
	}
}

func (r *Reporter) printCSV(results []SignalResult) error {
	w := csv.NewWriter(os.Stdout)
	w.Write([]string{"排名", "周期", "代码", "名称", "板块", "板块涨跌%", "板块标签", "日期", "收盘价", "实时价", "盘中涨跌%", "实时更新时间", "盘中标签", "买入信号", "卖出信号", "买入有效票", "卖出有效票", "买入策略组", "卖出策略组", "综合评分", "置信度", "建议仓位", "资金净额(万)", "大单净额(万)", "流动性模型", "上市天数", "日均成交额(元)", "换手率%", "预计成交占比%", "预计冲击%", "历史日期样本", "历史交易数", "历史资格依据", "历史收缩先验", "历史胜率", "历史期望收益", "建议权重", "风险标签", "风险执行", "是否过滤", "过滤原因", "建议", "触发策略"})
	for i, res := range results {
		w.Write([]string{
			fmt.Sprintf("%d", i+1),
			string(res.Horizon),
			res.Code,
			res.Name,
			res.SectorName,
			formatOptionalPct(res.SectorName != "", res.SectorChg1),
			strings.Join(res.SectorTags, ";"),
			res.Date,
			fmt.Sprintf("%.2f", res.Close),
			formatCSVAmount(res.HasRealtime, res.RealtimePrice),
			formatCSVAmount(res.HasRealtime, res.RealtimeChangePct),
			res.RealtimeUpdateAt,
			strings.Join(res.IntradayLabels, ";"),
			fmt.Sprintf("%d", res.BuyCount),
			fmt.Sprintf("%d", res.SellCount),
			fmt.Sprintf("%.2f", res.EffectiveBuyVotes),
			fmt.Sprintf("%.2f", res.EffectiveSellVotes),
			fmt.Sprintf("%d", res.BuyGroupCount),
			fmt.Sprintf("%d", res.SellGroupCount),
			fmt.Sprintf("%.2f", res.TotalScore),
			fmt.Sprintf("%.0f", res.Confidence),
			fmt.Sprintf("%.1f%%", res.PositionPct),
			formatCSVAmount(res.HasMoneyflow, res.MoneyflowNetAmount),
			formatCSVAmount(res.HasMoneyflow, res.LargeMoneyflowNetAmount),
			res.LiquidityModel,
			formatLiquidityInt(res.LiquidityApplied, res.ListingDays),
			formatCSVAmount(res.LiquidityApplied, res.AverageAmountCNY),
			formatOptionalPct(res.HasTurnover, res.TurnoverRatePct),
			formatOptionalPct(res.LiquidityApplied, res.ParticipationPct),
			formatOptionalPct(res.LiquidityApplied, res.EstimatedImpactPct),
			formatEvidenceSamples(res),
			formatEvidenceTrades(res),
			formatEvidenceBasis(res),
			formatEvidencePriorForResult(res),
			formatEvidenceWinRate(res),
			formatEvidenceExpectedReturn(res),
			formatEvidenceWeight(res),
			strings.Join(res.RiskLabels, ";"),
			RiskPolicySummary(res),
			fmt.Sprintf("%t", res.Suppressed),
			res.SuppressionReason,
			res.Recommendation(),
			formatActiveStrategies(res.Strategies),
		})
	}
	w.Flush()
	return nil
}

func (r *Reporter) printCSVWithWatch(results []SignalResult, watchlist []SignalResult) error {
	w := csv.NewWriter(os.Stdout)
	w.Write([]string{"类别", "排名", "周期", "代码", "名称", "板块", "板块涨跌%", "板块标签", "日期", "收盘价", "实时价", "盘中涨跌%", "实时更新时间", "盘中标签", "买入信号", "卖出信号", "买入有效票", "卖出有效票", "买入策略组", "卖出策略组", "综合评分", "置信度", "建议仓位", "资金净额(万)", "大单净额(万)", "流动性模型", "上市天数", "日均成交额(元)", "换手率%", "预计成交占比%", "预计冲击%", "历史日期样本", "历史交易数", "历史资格依据", "历史收缩先验", "历史胜率", "历史期望收益", "建议权重", "风险标签", "风险执行", "是否过滤", "过滤原因", "观察原因", "建议", "触发策略"})
	writeCSVRows(w, "正式信号", results, false)
	writeCSVRows(w, "观察机会", watchlist, true)
	w.Flush()
	return nil
}

func (r *Reporter) printJSON(results []SignalResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(toJSONOutput(results, false))
}

func (r *Reporter) printJSONWithWatch(results []SignalResult, watchlist []SignalResult) error {
	type jsonSet struct {
		Signals   []jsonOutput `json:"signals"`
		Watchlist []jsonOutput `json:"watchlist"`
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonSet{
		Signals:   toJSONOutput(results, false),
		Watchlist: toJSONOutput(watchlist, true),
	})
}

func writeCSVRows(w *csv.Writer, category string, results []SignalResult, includeWatchReason bool) {
	for i, res := range results {
		watchReason := ""
		if includeWatchReason {
			watchReason = formatWatchReason(res)
		}
		w.Write([]string{
			category,
			fmt.Sprintf("%d", i+1),
			string(res.Horizon),
			res.Code,
			res.Name,
			res.SectorName,
			formatOptionalPct(res.SectorName != "", res.SectorChg1),
			strings.Join(res.SectorTags, ";"),
			res.Date,
			fmt.Sprintf("%.2f", res.Close),
			formatCSVAmount(res.HasRealtime, res.RealtimePrice),
			formatCSVAmount(res.HasRealtime, res.RealtimeChangePct),
			res.RealtimeUpdateAt,
			strings.Join(res.IntradayLabels, ";"),
			fmt.Sprintf("%d", res.BuyCount),
			fmt.Sprintf("%d", res.SellCount),
			fmt.Sprintf("%.2f", res.EffectiveBuyVotes),
			fmt.Sprintf("%.2f", res.EffectiveSellVotes),
			fmt.Sprintf("%d", res.BuyGroupCount),
			fmt.Sprintf("%d", res.SellGroupCount),
			fmt.Sprintf("%.2f", res.TotalScore),
			fmt.Sprintf("%.0f", res.Confidence),
			fmt.Sprintf("%.1f%%", res.PositionPct),
			formatCSVAmount(res.HasMoneyflow, res.MoneyflowNetAmount),
			formatCSVAmount(res.HasMoneyflow, res.LargeMoneyflowNetAmount),
			res.LiquidityModel,
			formatLiquidityInt(res.LiquidityApplied, res.ListingDays),
			formatCSVAmount(res.LiquidityApplied, res.AverageAmountCNY),
			formatOptionalPct(res.HasTurnover, res.TurnoverRatePct),
			formatOptionalPct(res.LiquidityApplied, res.ParticipationPct),
			formatOptionalPct(res.LiquidityApplied, res.EstimatedImpactPct),
			formatEvidenceSamples(res),
			formatEvidenceTrades(res),
			formatEvidenceBasis(res),
			formatEvidencePriorForResult(res),
			formatEvidenceWinRate(res),
			formatEvidenceExpectedReturn(res),
			formatEvidenceWeight(res),
			strings.Join(res.RiskLabels, ";"),
			RiskPolicySummary(res),
			fmt.Sprintf("%t", res.Suppressed),
			res.SuppressionReason,
			watchReason,
			res.Recommendation(),
			formatActiveStrategies(res.Strategies),
		})
	}
}

func toJSONOutput(results []SignalResult, includeWatchReason bool) []jsonOutput {
	output := make([]jsonOutput, 0, len(results))
	for i, r := range results {
		strats := make(map[string]string)
		for name, detail := range r.Strategies {
			strats[name] = detail.Signal.String()
		}
		item := jsonOutput{
			Rank:               i + 1,
			Horizon:            r.Horizon,
			HorizonLabel:       strategy.HorizonLabel(r.Horizon),
			Code:               r.Code,
			Name:               r.Name,
			Date:               r.Date,
			Close:              r.Close,
			BuySignals:         r.BuyCount,
			SellSignals:        r.SellCount,
			BuyGroups:          r.BuyGroupCount,
			SellGroups:         r.SellGroupCount,
			EffectiveBuyVotes:  r.EffectiveBuyVotes,
			EffectiveSellVotes: r.EffectiveSellVotes,
			VoteMetricsApplied: r.VoteMetricsApplied,
			TotalScore:         r.TotalScore,
			RawScore:           r.RawScore,
			Confidence:         r.Confidence,
			PositionPct:        r.PositionPct,
			HasMoneyflow:       r.HasMoneyflow,
			MoneyflowNet:       r.MoneyflowNetAmount,
			LargeNet:           r.LargeMoneyflowNetAmount,
			SectorName:         r.SectorName,
			SectorTags:         r.SectorTags,
			SectorChg1:         r.SectorChg1,
			HasRealtime:        r.HasRealtime,
			RealtimePrice:      r.RealtimePrice,
			RealtimePct:        r.RealtimeChangePct,
			RealtimeAt:         r.RealtimeUpdateAt,
			IntradayLabels:     r.IntradayLabels,
			Suppressed:         r.Suppressed,
			SuppressReason:     r.SuppressionReason,
			RiskLabels:         r.RiskLabels,
			RiskEffects:        AssessRiskPolicy(r),
			Reasons:            r.Reasons,
			Recommendation:     r.Recommendation(),
			Strategies:         strats,
			GroupScores:        r.GroupScores,
			HistoricalEvidence: r.HistoricalEvidence,
			LiquidityModel:     r.LiquidityModel,
			LiquidityApplied:   r.LiquidityApplied,
			LiquidityEligible:  r.LiquidityEligible,
			ListingDays:        r.ListingDays,
			AverageAmountCNY:   r.AverageAmountCNY,
			HasTurnover:        r.HasTurnover,
			TurnoverRatePct:    r.TurnoverRatePct,
			OrderValueCNY:      r.EstimatedOrderValueCNY,
			ParticipationPct:   r.ParticipationPct,
			ImpactPct:          r.EstimatedImpactPct,
		}
		if includeWatchReason {
			item.WatchReason = formatWatchReason(r)
		}
		output = append(output, item)
	}
	return output
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
	fmt.Fprintln(w, "排名\t代码\t名称\t板块\t日期\t收盘价\t盘中\t原始买/卖\t有效买/卖(组)\t评分\t置信度\t仓位\t资金\t流动性\t风险执行\t触发策略")
	for i, r := range results {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%.2f\t%s\t%d/%d\t%s\t%.2f\t%.0f\t%.1f%%\t%s\t%s\t%s\t%s\n",
			i+1, r.Code, r.Name, formatSector(r), r.Date, r.Close, formatRealtime(r), r.BuyCount, r.SellCount,
			formatEffectiveVotes(r), r.TotalScore, r.Confidence, r.PositionPct, formatMoneyflow(r), LiquiditySummary(r), RiskPolicySummary(r),
			formatActiveStrategies(r.Strategies))
	}
	w.Flush()
}

func printWatchTable(title string, results []SignalResult) {
	sort.Slice(results, func(i, j int) bool {
		left := watchScore(results[i])
		right := watchScore(results[j])
		if left == right {
			if results[i].TotalScore == results[j].TotalScore {
				return results[i].Code < results[j].Code
			}
			return results[i].TotalScore > results[j].TotalScore
		}
		return left > right
	})

	fmt.Printf("\n%s\n", title)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "排名\t周期\t代码\t名称\t板块\t日期\t收盘价\t盘中\t原始买/卖\t有效买/卖(组)\t评分\t置信度\t资金\t流动性\t风险执行\t观察原因\t触发策略")
	for i, r := range results {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%.2f\t%s\t%d/%d\t%s\t%.2f\t%.0f\t%s\t%s\t%s\t%s\t%s\n",
			i+1, strategy.HorizonLabel(r.Horizon), r.Code, r.Name, formatSector(r), r.Date, r.Close, formatRealtime(r),
			r.BuyCount, r.SellCount, formatEffectiveVotes(r), r.TotalScore, r.Confidence, formatMoneyflow(r),
			LiquiditySummary(r), RiskPolicySummary(r), formatWatchReason(r), formatActiveStrategies(r.Strategies))
	}
	w.Flush()
}

func formatEffectiveVotes(r SignalResult) string {
	if !r.VoteMetricsApplied {
		return "-"
	}
	return fmt.Sprintf("%.2f/%.2f(%d/%d)", r.EffectiveBuyVotes, r.EffectiveSellVotes, r.BuyGroupCount, r.SellGroupCount)
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

func formatSector(r SignalResult) string {
	if r.SectorName == "" {
		return "-"
	}
	return fmt.Sprintf("%s(%+.1f%%)", r.SectorName, r.SectorChg1)
}

func formatCSVAmount(ok bool, amount float64) string {
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.0f", amount)
}

func formatLiquidityInt(ok bool, value int) string {
	if !ok || value < 0 {
		return ""
	}
	return fmt.Sprintf("%d", value)
}

func LiquiditySummary(r SignalResult) string {
	if !r.LiquidityApplied {
		return "-"
	}
	turnover := "换手?"
	if r.HasTurnover {
		turnover = fmt.Sprintf("换%.2f%%", r.TurnoverRatePct)
	}
	return fmt.Sprintf("额%.0f万/%s/占%.2f%%/冲%.3f%%",
		r.AverageAmountCNY/10_000, turnover, r.ParticipationPct, r.EstimatedImpactPct)
}

func formatOptionalPct(ok bool, amount float64) string {
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.2f", amount)
}

func formatWatchReason(r SignalResult) string {
	var reasons []string
	if r.SuppressionReason != "" {
		addUnique(&reasons, r.SuppressionReason)
	}
	for _, issue := range qualifyingBuyIssues(r) {
		addUnique(&reasons, issue)
	}
	switch {
	case r.Recommendation() == "买入":
		addUnique(&reasons, "正式买入榜外候选")
	case rawRecommendation(r) == "买入" && r.Suppressed:
		addUnique(&reasons, "仓位/风控过滤")
	case r.SellCount > 0:
		addUnique(&reasons, "多空冲突")
	default:
		addUnique(&reasons, "共振不足")
	}
	if len(reasons) == 0 {
		return "-"
	}
	return strings.Join(reasons, ";")
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
