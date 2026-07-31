package sector

import (
	"testing"

	"quant/internal/data"
)

func TestAnalyzeIndustrySectorDaily(t *testing.T) {
	memberships := NewIndustryMemberships([]data.StockInfo{
		{TsCode: "000001.SZ", Industry: "科技", ListDate: "20250101"},
		{TsCode: "000002.SZ", Industry: "科技", ListDate: "20250101"},
		{TsCode: "000003.SZ", Industry: "银行", ListDate: "20250101"},
	})
	codeMap := map[string][]data.DailyBar{
		"000001.SZ": sectorBars("000001.SZ", 10, 11, 300),
		"000002.SZ": sectorBars("000002.SZ", 10, 10.5, 100),
		"000003.SZ": sectorBars("000003.SZ", 10, 9.8, 100),
	}
	moneyflows := data.NewMoneyflowStore([]data.Moneyflow{
		{TsCode: "000001.SZ", TradeDate: "20260121", NetMfAmount: 100, BuyLgAmount: 60, BuyElgAmount: 20},
		{TsCode: "000002.SZ", TradeDate: "20260121", NetMfAmount: 50, BuyLgAmount: 30, BuyElgAmount: 10},
	})
	fundamentals := data.NewFundamentalStore()
	fundamentals.LoadDailyBasics([]data.DailyBasic{
		{TsCode: "000001.SZ", TradeDate: "20260121", Pe: 10, PeTTM: 8, Pb: 2, TotalMv: 800},
		{TsCode: "000002.SZ", TradeDate: "20260121", Pe: 20, PeTTM: 16, Pb: 4, TotalMv: 1600},
		{TsCode: "000003.SZ", TradeDate: "20260121", Pe: -5, PeTTM: -2, Pb: 1, TotalMv: 100},
	})

	rows := Analyze(codeMap, memberships, moneyflows, AnalyzeOptions{
		Dates:        []string{"20260121"},
		UpdatedAt:    "test",
		Fundamentals: fundamentals,
	})
	report := NewReport(rows)
	tech, ok := report.Find(TypeIndustry, "科技")
	if !ok {
		t.Fatal("科技 sector not found")
	}

	if tech.MemberCount != 2 {
		t.Fatalf("MemberCount = %d, want 2", tech.MemberCount)
	}
	if tech.RisingCount != 2 || tech.FallingCount != 0 {
		t.Fatalf("涨跌家数 = %d/%d, want 2/0", tech.RisingCount, tech.FallingCount)
	}
	if tech.Breadth != 100 {
		t.Fatalf("Breadth = %.0f, want 100", tech.Breadth)
	}
	if tech.AmountRatio20 <= 1.5 {
		t.Fatalf("AmountRatio20 = %.2f, want > 1.5", tech.AmountRatio20)
	}
	for _, tag := range []string{"板块放量", "赚钱效应扩散", "资金确认"} {
		if !HasTag(tech, tag) {
			t.Fatalf("Tags = %q, want %s", tech.Tags, tag)
		}
	}
	if tech.PECount != 2 || tech.PETTMCount != 2 || tech.PBCount != 2 {
		t.Fatalf("估值样本数 PE/PE_TTM/PB = %d/%d/%d, want 2/2/2", tech.PECount, tech.PETTMCount, tech.PBCount)
	}
	if tech.PEAvg != 15 || tech.PETTMAvg != 12 || tech.PBAvg != 3 {
		t.Fatalf("估值均值 PE/PE_TTM/PB = %.2f/%.2f/%.2f, want 15/12/3", tech.PEAvg, tech.PETTMAvg, tech.PBAvg)
	}
	if tech.PETTMAggregate != 12 || tech.PBAggregate != 3 {
		t.Fatalf("聚合估值 PE_TTM/PB = %.2f/%.2f, want 12/3", tech.PETTMAggregate, tech.PBAggregate)
	}
}

func TestWriteAndLoadSectorDailyMergesByDateAndSector(t *testing.T) {
	rawDir := t.TempDir()
	first := data.SectorDaily{
		TradeDate:  "20260121",
		SectorType: TypeIndustry,
		SectorCode: "科技",
		SectorName: "科技",
		Chg1:       1,
	}
	replacement := first
	replacement.Chg1 = 2

	if err := WriteSectorDaily(rawDir, []data.SectorDaily{first}); err != nil {
		t.Fatal(err)
	}
	if err := WriteSectorDaily(rawDir, []data.SectorDaily{replacement}); err != nil {
		t.Fatal(err)
	}

	rows, err := LoadSectorDaily(rawDir, "20260121", "20260121")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].Chg1 != 2 {
		t.Fatalf("Chg1 = %.1f, want replacement value 2.0", rows[0].Chg1)
	}
}

func sectorBars(code string, prevClose, closePrice, lastAmount float64) []data.DailyBar {
	bars := make([]data.DailyBar, 0, 21)
	for day := 1; day <= 20; day++ {
		date := "202601" + twoDigitSector(day)
		bars = append(bars, data.DailyBar{
			TsCode:    code,
			TradeDate: date,
			Open:      prevClose,
			High:      prevClose,
			Low:       prevClose,
			Close:     prevClose,
			Amount:    100,
			Vol:       100,
		})
	}
	bars = append(bars, data.DailyBar{
		TsCode:    code,
		TradeDate: "20260121",
		Open:      closePrice,
		High:      closePrice,
		Low:       closePrice,
		Close:     closePrice,
		Amount:    lastAmount,
		Vol:       100,
	})
	return bars
}

func twoDigitSector(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
