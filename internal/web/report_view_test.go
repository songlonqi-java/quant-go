package web

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"quant/internal/data"
	"quant/internal/news"
	"quant/internal/signal"
	"quant/internal/strategy"
)

func TestDailyReportViewUsesFriendlyCodesHorizonsAndLimitedSectors(t *testing.T) {
	sectors := make([]data.SectorDaily, 0, 24)
	for i := 0; i < 12; i++ {
		sectors = append(sectors,
			data.SectorDaily{SectorName: "涨" + strconv.Itoa(i), Chg1: float64(i + 1)},
			data.SectorDaily{SectorName: "跌" + strconv.Itoa(i), Chg1: -float64(i + 1)},
		)
	}
	response := httptest.NewRecorder()
	renderPage(response, taskTemplate, taskPageData{
		CanRecommend: true,
		Task: &Task{Report: &DailyReport{
			TradeDate: "20260801",
			Position:  signal.PositionDecision{Action: signal.PositionActionActive},
			Recommendations: []signal.SignalResult{{
				Horizon:            strategy.HorizonMid,
				Code:               "600000.SH",
				Name:               "浦发银行",
				EffectiveBuyVotes:  2.25,
				BuyGroupCount:      2,
				VoteMetricsApplied: true,
				TotalScore:         2,
				Confidence:         80,
				LiquidityApplied:   true,
				LiquidityEligible:  true,
				AverageAmountCNY:   50_000_000,
				HasTurnover:        true,
				TurnoverRatePct:    1.2,
				ParticipationPct:   0.4,
				EstimatedImpactPct: 0.03,
				RiskLabels:         []string{"20日涨幅过高"},
				HistoricalEvidence: &signal.HistoricalEvidence{
					Available: true, Eligible: true, StrategySpecific: true,
					Basis: "同策略组合 + 同市场状态", Samples: 40, Trades: 80,
					ExpectedReturnPct: 1.25, Status: "历史验证通过",
				},
			}},
			News:    &news.NewsSummary{TotalNews: 5, RecentNews: 2, RecentHotTopics: []news.HotTopic{{Keyword: "人工智能", Count: 2}}},
			Sectors: sectors,
		}},
	})
	body := response.Body.String()
	if response.Code != 200 || !strings.Contains(body, "中线") || strings.Contains(body, "600000.SH") || !strings.Contains(body, "600000") {
		t.Fatalf("friendly labels missing: status=%d body=%s", response.Code, body)
	}
	if !strings.Contains(body, "近 2 日热点") || !strings.Contains(body, "人工智能") {
		t.Fatalf("recent news missing: %s", body)
	}
	if !strings.Contains(body, "有效票/组") || !strings.Contains(body, "2.25 / 2组") {
		t.Fatalf("effective vote metrics missing: %s", body)
	}
	if !strings.Contains(body, "同策略组合") || !strings.Contains(body, "同市场状态") || !strings.Contains(body, "40日 / 80交易") || !strings.Contains(body, "历史验证通过") {
		t.Fatalf("historical evidence missing: %s", body)
	}
	if !strings.Contains(body, "风险执行") || !strings.Contains(body, "20日涨幅过高[扣6分/仓位×75%]") {
		t.Fatalf("risk execution semantics missing: %s", body)
	}
	if !strings.Contains(body, "流动性") || !strings.Contains(body, "额5000万/换1.20%/占0.40%/冲0.030%") {
		t.Fatalf("liquidity audit missing: %s", body)
	}
	if !strings.Contains(body, "涨幅前 10") || !strings.Contains(body, "涨11") || strings.Contains(body, "涨0<") || !strings.Contains(body, "跌11") || strings.Contains(body, "跌0<") {
		t.Fatalf("sector limit/sort missing: %s", body)
	}
	if len(risingSectors(sectors)) != 10 || len(fallingSectors(sectors)) != 10 {
		t.Fatalf("unexpected sector limits: rising=%d falling=%d", len(risingSectors(sectors)), len(fallingSectors(sectors)))
	}
}

func TestDailyReportViewMarksLegacyVoteMetrics(t *testing.T) {
	response := httptest.NewRecorder()
	renderPage(response, taskTemplate, taskPageData{
		CanRecommend: true,
		Task: &Task{Report: &DailyReport{
			TradeDate: "20260801",
			Position:  signal.PositionDecision{Action: signal.PositionActionActive},
			Recommendations: []signal.SignalResult{{
				Horizon: strategy.HorizonMid, Code: "600000.SH", Name: "浦发银行",
			}},
		}},
	})
	if body := response.Body.String(); response.Code != 200 || !strings.Contains(body, "旧报告未记录") {
		t.Fatalf("legacy vote metrics not identified: status=%d body=%s", response.Code, body)
	}
}

func TestDisplayStockCodeKeepsUnknownValuesReadable(t *testing.T) {
	for input, want := range map[string]string{"000001.SZ": "000001", "600000.SH": "600000", "430047.BJ": "430047", "custom": "custom"} {
		if got := displayStockCode(input); got != want {
			t.Errorf("displayStockCode(%q)=%q want=%q", input, got, want)
		}
	}
}
