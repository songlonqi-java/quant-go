package signal

import (
	"math"
	"testing"

	"quant/internal/data"
	"quant/internal/market"
	"quant/internal/sector"
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

func TestLimitByRecommendationDropsWatchOnlyResults(t *testing.T) {
	results := []SignalResult{
		{
			Horizon:    strategy.HorizonShort,
			Code:       "000001.SZ",
			BuyCount:   2,
			TotalScore: 2.0,
		},
		{
			Horizon:    strategy.HorizonShort,
			Code:       "000002.SZ",
			BuyCount:   1,
			SellCount:  1,
			TotalScore: 0.8,
		},
		{
			Horizon:    strategy.HorizonMid,
			Code:       "000003.SZ",
			BuyCount:   2,
			TotalScore: 1.5,
		},
	}

	formal := LimitByRecommendation(results, 1)

	if len(formal) != 2 {
		t.Fatalf("len(formal) = %d, want 2", len(formal))
	}
	if formal[0].Code != "000001.SZ" {
		t.Fatalf("formal[0].Code = %s, want 000001.SZ", formal[0].Code)
	}
	if formal[1].Code != "000003.SZ" {
		t.Fatalf("formal[1].Code = %s, want 000003.SZ", formal[1].Code)
	}
}

func TestCandidatePoolBackfillsAnIntradayRejectedLeader(t *testing.T) {
	results := []SignalResult{
		{Horizon: strategy.HorizonShort, Code: "000001.SZ", BuyCount: 3, TotalScore: 3.0, Confidence: 80, PositionPct: 5, IntradayLabels: []string{"高开>3%"}},
		{Horizon: strategy.HorizonShort, Code: "000002.SZ", BuyCount: 3, TotalScore: 2.0, Confidence: 80, PositionPct: 5},
		{Horizon: strategy.HorizonShort, Code: "000003.SZ", BuyCount: 3, TotalScore: 1.0, Confidence: 80, PositionPct: 5},
		{Horizon: strategy.HorizonShort, Code: "000004.SZ", BuyCount: 3, TotalScore: 0.5, Confidence: 80, PositionPct: 5},
	}

	pool := SelectCandidatePool(results, 1)
	if len(pool) != 3 {
		t.Fatalf("len(pool) = %d, want 3", len(pool))
	}
	decision := ApplyPositionPolicy(pool, &market.MarketStatus{Sentiment: "偏多"})
	if decision.QualifiedBuys != 2 {
		t.Fatalf("QualifiedBuys = %d, want 2", decision.QualifiedBuys)
	}
	formal := LimitByRecommendation(pool, 1)
	if len(formal) != 1 || formal[0].Code != "000002.SZ" {
		t.Fatalf("formal = %+v, want 000002.SZ to backfill rejected leader", formal)
	}
}

func TestSelectWatchlistKeepsNonFormalBuyCandidates(t *testing.T) {
	formal := []SignalResult{
		{
			Horizon:    strategy.HorizonShort,
			Code:       "000001.SZ",
			BuyCount:   3,
			TotalScore: 3.0,
			Confidence: 80,
		},
	}
	results := []SignalResult{
		formal[0],
		{
			Horizon:    strategy.HorizonShort,
			Code:       "000002.SZ",
			BuyCount:   1,
			TotalScore: 1.2,
			Confidence: 55,
		},
		{
			Horizon:    strategy.HorizonShort,
			Code:       "000003.SZ",
			BuyCount:   1,
			SellCount:  1,
			TotalScore: 0.8,
			Confidence: 45,
		},
		{
			Horizon:    strategy.HorizonShort,
			Code:       "000004.SZ",
			SellCount:  1,
			TotalScore: -1.0,
			Confidence: 30,
		},
	}

	watchlist := SelectWatchlist(results, formal, 10)

	if len(watchlist) != 2 {
		t.Fatalf("len(watchlist) = %d, want 2", len(watchlist))
	}
	if watchlist[0].Code != "000002.SZ" {
		t.Fatalf("watchlist[0].Code = %s, want 000002.SZ", watchlist[0].Code)
	}
	if watchlist[1].Code != "000003.SZ" {
		t.Fatalf("watchlist[1].Code = %s, want 000003.SZ", watchlist[1].Code)
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

func TestApplyPositionPolicyRejectsCandidateWithoutRequiredHistoricalEvidence(t *testing.T) {
	results := []SignalResult{{
		Horizon: strategy.HorizonShort, BuyCount: 3, TotalScore: 2, Confidence: 80, PositionPct: 5,
		HistoricalEvidence: &HistoricalEvidence{Available: true, Enforced: true, Eligible: false, Status: "历史验证不足"},
	}}
	decision := ApplyPositionPolicy(results, &market.MarketStatus{Sentiment: "中性震荡"})
	if decision.QualifiedBuys != 0 || decision.Action != PositionActionCash {
		t.Fatalf("decision = %#v, want no qualified buy and cash", decision)
	}
	if !results[0].Suppressed || results[0].PositionPct != 0 {
		t.Fatalf("result = %#v, want suppressed candidate", results[0])
	}
}

func TestBoundedContributionTreatsNonFiniteScoreAsNeutral(t *testing.T) {
	for _, score := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got := boundedContribution(score); got != 1 {
			t.Fatalf("boundedContribution(%v) = %.2f, want 1.00", score, got)
		}
	}
}

func TestSignalStrengthAndConfidenceAreDirectionSymmetric(t *testing.T) {
	if boundedContribution(-20) <= boundedContribution(-5) {
		t.Fatalf("stronger negative sell score should have greater magnitude: %.2f <= %.2f", boundedContribution(-20), boundedContribution(-5))
	}
	buy := confidenceScore(SignalResult{BuyCount: 2, TotalScore: 2})
	sell := confidenceScore(SignalResult{SellCount: 2, TotalScore: -2})
	if buy != sell {
		t.Fatalf("buy confidence %.1f != symmetric sell confidence %.1f", buy, sell)
	}
}

func TestApplySectorContextAddsSectorLabels(t *testing.T) {
	results := []SignalResult{
		{
			Horizon:    strategy.HorizonShort,
			Code:       "000001.SZ",
			Date:       "20260121",
			BuyCount:   3,
			TotalScore: 2.5,
		},
	}
	memberships := sector.NewIndustryMemberships([]data.StockInfo{
		{TsCode: "000001.SZ", Industry: "科技", ListDate: "20250101"},
	})
	report := sector.NewReport([]data.SectorDaily{
		{
			TradeDate:  "20260121",
			SectorType: sector.TypeIndustry,
			SectorCode: "科技",
			SectorName: "科技",
			Chg1:       2.3,
			Tags:       "板块放量,赚钱效应扩散,资金确认",
		},
	})

	ApplySectorContext(results, report, memberships)

	if results[0].SectorName != "科技" {
		t.Fatalf("SectorName = %q, want 科技", results[0].SectorName)
	}
	if results[0].SectorChg1 != 2.3 {
		t.Fatalf("SectorChg1 = %.1f, want 2.3", results[0].SectorChg1)
	}
	for _, label := range []string{"板块共振", "板块资金确认"} {
		if !containsString(results[0].RiskLabels, label) {
			t.Fatalf("RiskLabels = %v, want %s", results[0].RiskLabels, label)
		}
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
