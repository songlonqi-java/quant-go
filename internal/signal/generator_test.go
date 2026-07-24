package signal

import (
	"testing"

	"quant/internal/data"
	"quant/internal/market"
	"quant/internal/strategy"
)

func TestGenerateWithContextLimitsPerHorizon(t *testing.T) {
	barsMap := map[string][]data.DailyBar{
		"000001.SZ": signalBars("000001.SZ"),
		"000002.SZ": signalBars("000002.SZ"),
	}
	strategies := []strategy.Strategy{
		fixedStrategy{name: "limit_up", score: 10},
		fixedStrategy{name: "ma_crossover", score: 10},
		fixedStrategy{name: "dividend_deviation", score: 10},
	}

	results := GenerateWithContext(barsMap, strategies, 1, nil, nil)

	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want one result per horizon", len(results))
	}
	counts := make(map[strategy.Horizon]int)
	for _, r := range results {
		counts[r.Horizon]++
	}
	for _, horizon := range strategy.HorizonOrder() {
		if counts[horizon] != 1 {
			t.Fatalf("horizon %s count = %d, want 1", horizon, counts[horizon])
		}
	}
}

func TestGenerateWithContextAndMoneyflowAddsConfirmation(t *testing.T) {
	barsMap := map[string][]data.DailyBar{
		"000001.SZ": signalBars("000001.SZ"),
	}
	strategies := []strategy.Strategy{
		fixedStrategy{name: "limit_up", score: 10},
	}
	store := data.NewMoneyflowStore([]data.Moneyflow{
		{
			TsCode:        "000001.SZ",
			TradeDate:     "20260102",
			NetMfAmount:   1000,
			BuyLgAmount:   300,
			SellLgAmount:  100,
			BuyElgAmount:  500,
			SellElgAmount: 50,
		},
	})

	results := GenerateWithContextAndMoneyflow(barsMap, strategies, 0, nil, nil, store)

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if !results[0].HasMoneyflow {
		t.Fatal("HasMoneyflow = false, want true")
	}
	if results[0].MoneyflowNetAmount != 1000 {
		t.Fatalf("MoneyflowNetAmount = %.0f, want 1000", results[0].MoneyflowNetAmount)
	}
	if results[0].LargeMoneyflowNetAmount != 650 {
		t.Fatalf("LargeMoneyflowNetAmount = %.0f, want 650", results[0].LargeMoneyflowNetAmount)
	}
	if !containsString(results[0].RiskLabels, "资金确认") {
		t.Fatalf("RiskLabels = %v, want 资金确认", results[0].RiskLabels)
	}
}

func TestApplyPositionPolicySuppressesBuysWhenBearishWithoutMoneyflow(t *testing.T) {
	results := []SignalResult{
		{
			Horizon:     strategy.HorizonShort,
			Code:        "000001.SZ",
			Name:        "平安银行",
			Date:        "20260102",
			BuyCount:    3,
			TotalScore:  2.5,
			Confidence:  80,
			PositionPct: 5,
		},
	}

	decision := ApplyPositionPolicy(results, &market.MarketStatus{Sentiment: "偏空"})

	if decision.Action != PositionActionCash {
		t.Fatalf("Action = %s, want %s", decision.Action, PositionActionCash)
	}
	if results[0].Recommendation() != "观望" {
		t.Fatalf("Recommendation = %s, want 观望", results[0].Recommendation())
	}
	if !results[0].Suppressed {
		t.Fatal("Suppressed = false, want true")
	}
	if results[0].PositionPct != 0 {
		t.Fatalf("PositionPct = %.1f, want 0", results[0].PositionPct)
	}
	if !containsString(results[0].RiskLabels, "空仓过滤") {
		t.Fatalf("RiskLabels = %v, want 空仓过滤", results[0].RiskLabels)
	}
}

func TestApplyPositionPolicyAllowsBearishProbeWithMoneyflowConfirmation(t *testing.T) {
	results := []SignalResult{
		{
			Horizon:                 strategy.HorizonShort,
			Code:                    "000001.SZ",
			Name:                    "平安银行",
			Date:                    "20260102",
			BuyCount:                3,
			TotalScore:              2.5,
			Confidence:              80,
			PositionPct:             5,
			HasMoneyflow:            true,
			MoneyflowNetAmount:      1000,
			LargeMoneyflowNetAmount: 500,
		},
	}

	decision := ApplyPositionPolicy(results, &market.MarketStatus{Sentiment: "偏空"})

	if decision.Action != PositionActionProbe {
		t.Fatalf("Action = %s, want %s", decision.Action, PositionActionProbe)
	}
	if results[0].Recommendation() != "买入" {
		t.Fatalf("Recommendation = %s, want 买入", results[0].Recommendation())
	}
	if results[0].PositionPct != 3 {
		t.Fatalf("PositionPct = %.1f, want 3", results[0].PositionPct)
	}
	if !containsString(results[0].RiskLabels, "轻仓试错") {
		t.Fatalf("RiskLabels = %v, want 轻仓试错", results[0].RiskLabels)
	}
}

type fixedStrategy struct {
	name  string
	score float64
}

func (f fixedStrategy) Name() string { return f.name }
func (f fixedStrategy) Warmup() int  { return 0 }
func (f fixedStrategy) Signal(_ []data.DailyBar, _ int) strategy.SignalType {
	return strategy.Buy
}
func (f fixedStrategy) Score(_ []data.DailyBar, _ int) float64 { return f.score }

func signalBars(code string) []data.DailyBar {
	return []data.DailyBar{
		{TsCode: code, TradeDate: "20260101", Open: 10, High: 10, Low: 10, Close: 10, RawClose: 10, AdjFactor: 1},
		{TsCode: code, TradeDate: "20260102", Open: 11, High: 11, Low: 11, Close: 11, RawClose: 11, AdjFactor: 1},
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
