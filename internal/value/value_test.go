package value

import (
	"os"
	"path/filepath"
	"testing"

	"quant/internal/data"
	"quant/internal/sector"

	"github.com/parquet-go/parquet-go"
)

func TestMonthlyPersistsQualifiedCandidateAndQuarterlyRemovesDeterioration(t *testing.T) {
	rawDir := t.TempDir()
	const tradeDate = "20260731"
	if err := data.WriteParquetFile(filepath.Join(rawDir, "daily", "2026.parquet"), []data.DailyBar{
		{TsCode: "000001.SZ", TradeDate: tradeDate, Close: 10, RawClose: 10},
		{TsCode: "000002.SZ", TradeDate: tradeDate, Close: 20, RawClose: 20},
	}); err != nil {
		t.Fatal(err)
	}
	if err := data.WriteStocksParquet(filepath.Join(rawDir, "stocks.parquet"), []data.StockInfo{
		{TsCode: "000001.SZ", Name: "价值甲", Industry: "科技"},
		{TsCode: "000002.SZ", Name: "行业乙", Industry: "科技"},
	}); err != nil {
		t.Fatal(err)
	}
	writeValueParquet(t, filepath.Join(rawDir, "daily_basic", "2026.parquet"), []data.DailyBasic{
		{TsCode: "000001.SZ", TradeDate: tradeDate, Pe: 12, PeTTM: 10, Pb: 1.5, TotalMv: 100000},
		{TsCode: "000002.SZ", TradeDate: tradeDate, Pe: 25, PeTTM: 20, Pb: 2, TotalMv: 100000},
	})
	writeValueParquet(t, filepath.Join(rawDir, "fina", "fina_indicator.parquet"), []data.FinaIndicator{
		{TsCode: "000001.SZ", AnnDate: tradeDate, Roe: 12, NIncomeYoY: 10, RevenueYoY: 8},
		{TsCode: "000002.SZ", AnnDate: tradeDate, Roe: 12, NIncomeYoY: 10, RevenueYoY: 8},
	})
	if err := sector.WriteSectorDaily(rawDir, []data.SectorDaily{{
		TradeDate: tradeDate, SectorType: sector.TypeIndustry, SectorCode: "科技", SectorName: "科技",
		PETTMAggregate: 20, PBAggregate: 2, PETTMCount: 2, PBCount: 2,
	}}); err != nil {
		t.Fatal(err)
	}

	monthly, err := Monthly(MonthlyOptions{RawDir: rawDir, Date: tradeDate, TopN: 20})
	if err != nil {
		t.Fatal(err)
	}
	if monthly.Qualified != 1 || len(monthly.Candidates) != 1 {
		t.Fatalf("qualified/candidates = %d/%d, want 1/1", monthly.Qualified, len(monthly.Candidates))
	}
	if got := monthly.Candidates[0]; got.Code != "000001.SZ" || got.DiscountPct != 50 || got.ValuationBasis != "PE_TTM" {
		t.Fatalf("candidate = %+v, want 000001.SZ with 50%% PE_TTM discount", got)
	}
	if _, err := os.Stat(monthly.SnapshotPath); err != nil {
		t.Fatalf("monthly snapshot not persisted: %v", err)
	}

	writeValueParquet(t, filepath.Join(rawDir, "fina", "fina_indicator.parquet"), []data.FinaIndicator{
		{TsCode: "000001.SZ", AnnDate: tradeDate, Roe: 5, NIncomeYoY: -20, RevenueYoY: -15},
		{TsCode: "000002.SZ", AnnDate: tradeDate, Roe: 12, NIncomeYoY: 10, RevenueYoY: 8},
	})
	quarterly, err := Quarterly(QuarterlyOptions{RawDir: rawDir, Date: tradeDate})
	if err != nil {
		t.Fatal(err)
	}
	if len(quarterly.Items) != 1 || quarterly.Items[0].Decision != DecisionExit {
		t.Fatalf("quarterly items = %+v, want one deterioration exit", quarterly.Items)
	}
	if _, err := os.Stat(quarterly.ReviewPath); err != nil {
		t.Fatalf("quarterly review not persisted: %v", err)
	}
}

func writeValueParquet[T any](t *testing.T, path string, rows []T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := parquet.NewWriter(f)
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
