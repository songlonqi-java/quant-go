package signal

import (
	"fmt"
	"os"
	"text/tabwriter"

	"quant/internal/strategy"
)

// HistoricalEvidence is the out-of-sample evidence attached to a candidate by
// the validation module. Returns are percentages measured from a feasible next
// open entry to the configured horizon close, net of configured trading costs.
// SuggestedWeightPct is a share of the deployable portfolio budget, not a
// guarantee that the portfolio should be fully invested.
type HistoricalEvidence struct {
	Available         bool
	Eligible          bool
	Enforced          bool
	Basis             string
	StrategySpecific  bool
	PriorBasis        string
	PriorSamples      int
	PriorWeight       float64
	Trades            int
	Samples           int
	Wins              int
	WinRatePct        float64
	ExpectedReturnPct float64
	// ProxyExpectedReturnPct is the equal-weight market proxy return over the
	// same holding windows; AlphaExpectedReturnPct is the excess over it and is
	// what formal qualification is measured against.
	ProxyExpectedReturnPct float64
	AlphaExpectedReturnPct float64
	AverageWinPct          float64
	AverageLossPct         float64
	VolatilityPct          float64
	MaxDrawdownPct         float64
	PositiveFolds          int
	PositiveAlphaFolds     int
	FoldCount              int
	SuggestedWeightPct     float64
	Status                 string
}

// PrintHistoricalEvidence prints the evidence used to qualify and size the
// formal candidates and watchlist. It is intentionally separate from the main
// signal table so callers can see whether a candidate was rejected by history,
// market risk, or intraday execution constraints.
func PrintHistoricalEvidence(results, watchlist []SignalResult) {
	rows := append(append([]SignalResult{}, results...), watchlist...)
	count := 0
	for _, r := range rows {
		if r.HistoricalEvidence != nil {
			count++
		}
	}
	if count == 0 {
		return
	}
	fmt.Println("\n========== 历史样本外证据 ==========")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "周期\t代码\t资格依据\t收缩先验\t日期样本/交易\t胜率\t期望收益(超基准)\t最大回撤\t正收益/超基准折\t权重\t状态")
	for _, r := range rows {
		e := r.HistoricalEvidence
		if e == nil {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d/%d\t%.1f%%\t%+.2f%%(%+.2f%%)\t%.2f%%\t%d/%d\t%.1f%%\t%s\n",
			strategy.HorizonLabel(r.Horizon), r.Code, formatEvidenceBasisValue(*e), formatEvidencePrior(*e), e.Samples, e.Trades, e.WinRatePct,
			e.ExpectedReturnPct, e.AlphaExpectedReturnPct, e.MaxDrawdownPct, e.PositiveAlphaFolds, e.FoldCount,
			e.SuggestedWeightPct, e.Status)
	}
	w.Flush()
}

func formatEvidencePrior(e HistoricalEvidence) string {
	if !e.Available {
		return "-"
	}
	if e.PriorBasis == "" {
		return fmt.Sprintf("中性基线(权重%.0f)", e.PriorWeight)
	}
	return fmt.Sprintf("%s(%d日,权重%.0f)", e.PriorBasis, e.PriorSamples, e.PriorWeight)
}

func formatEvidenceBasisValue(e HistoricalEvidence) string {
	if !e.Available || e.Basis == "" {
		return "-"
	}
	return e.Basis
}

func formatEvidenceSamples(r SignalResult) string {
	if r.HistoricalEvidence == nil || !r.HistoricalEvidence.Available {
		return "-"
	}
	return fmt.Sprintf("%d", r.HistoricalEvidence.Samples)
}

func formatEvidenceTrades(r SignalResult) string {
	if r.HistoricalEvidence == nil || !r.HistoricalEvidence.Available {
		return "-"
	}
	return fmt.Sprintf("%d", r.HistoricalEvidence.Trades)
}

func formatEvidenceBasis(r SignalResult) string {
	if r.HistoricalEvidence == nil || !r.HistoricalEvidence.Available {
		return "-"
	}
	return formatEvidenceBasisValue(*r.HistoricalEvidence)
}

func formatEvidencePriorForResult(r SignalResult) string {
	if r.HistoricalEvidence == nil || !r.HistoricalEvidence.Available {
		return "-"
	}
	return formatEvidencePrior(*r.HistoricalEvidence)
}

func formatEvidenceWinRate(r SignalResult) string {
	if r.HistoricalEvidence == nil || !r.HistoricalEvidence.Available {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", r.HistoricalEvidence.WinRatePct)
}

func formatEvidenceExpectedReturn(r SignalResult) string {
	if r.HistoricalEvidence == nil || !r.HistoricalEvidence.Available {
		return "-"
	}
	return fmt.Sprintf("%+.2f%%", r.HistoricalEvidence.ExpectedReturnPct)
}

func formatEvidenceWeight(r SignalResult) string {
	if r.HistoricalEvidence == nil || r.HistoricalEvidence.SuggestedWeightPct <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", r.HistoricalEvidence.SuggestedWeightPct)
}
