package execution

import (
	"math"
	"testing"

	"quant/internal/data"
)

func TestAssessLiquidityAppliesListingAmountTurnoverAndParticipation(t *testing.T) {
	bars := liquidityBars(3, 10_000) // 10 million CNY per day
	policy := DefaultLiquidityPolicy()
	assessment := AssessLiquidity(LiquidityInput{
		Bars: bars, Index: 2, ListDate: "20251220", StockName: "普通股票",
		TurnoverRatePct: 0.3, HasTurnover: true, OrderValueCNY: 1_000_000,
	}, policy)
	if assessment.Eligible {
		t.Fatalf("assessment = %+v", assessment)
	}
	for _, label := range []string{"上市时间不足", "成交额不足", "换手率不足", "成交占比过高"} {
		if !containsString(assessment.HardLabels, label) {
			t.Fatalf("missing %q in %+v", label, assessment)
		}
	}
}

func TestAssessLiquidityMakesMissingTurnoverAuditableWithoutBlockingByDefault(t *testing.T) {
	bars := liquidityBars(20, 100_000)
	assessment := AssessLiquidity(LiquidityInput{
		Bars: bars, Index: 19, ListDate: "20200101", OrderValueCNY: 1_000_000,
	}, DefaultLiquidityPolicy())
	if !assessment.Eligible || !containsString(assessment.NoticeLabels, "换手数据缺失") {
		t.Fatalf("assessment = %+v", assessment)
	}
}

func TestAssessLiquidityRequiresTurnoverWhenConfigured(t *testing.T) {
	policy := DefaultLiquidityPolicy()
	policy.RequireTurnoverData = true
	assessment := AssessLiquidity(LiquidityInput{
		Bars: liquidityBars(20, 100_000), Index: 19, ListDate: "20200101", OrderValueCNY: 1_000_000,
	}, policy)
	if assessment.Eligible || !containsString(assessment.HardLabels, "必需换手数据缺失") {
		t.Fatalf("assessment = %+v", assessment)
	}
}

func TestRoundTripReturnWithImpactMatchesAdjustedPrices(t *testing.T) {
	costs := CostModel{Commission: 0.001, Slippage: 0.002}
	result, ok := RoundTripReturnWithImpact(10, 11, costs, 0.003, 0.004)
	if !ok {
		t.Fatal("impact return not calculated")
	}
	wantEntry := 10 * 1.002 * 1.003 * 1.001
	wantExit := 11 * 0.998 * 0.996 * 0.999
	want := (wantExit/wantEntry - 1) * 100
	if math.Abs(result.NetReturnPct-want) > 1e-12 || result.CostImpactPct <= 0 {
		t.Fatalf("result = %+v, want net %.12f", result, want)
	}
}

func liquidityBars(count int, amountThousandCNY float64) []data.DailyBar {
	bars := make([]data.DailyBar, count)
	for i := range bars {
		bars[i] = data.DailyBar{
			TsCode: "000001.SZ", TradeDate: "202601" + twoDigitLiquidity(i+1),
			Close: 10, RawClose: 10, Vol: 1000, Amount: amountThousandCNY,
		}
	}
	return bars
}

func twoDigitLiquidity(value int) string {
	return string([]byte{'0' + byte(value/10), '0' + byte(value%10)})
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
