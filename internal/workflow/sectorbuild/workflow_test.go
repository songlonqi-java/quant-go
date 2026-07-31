package sectorbuild

import (
	"path/filepath"
	"testing"

	"quant/internal/data"
	"quant/internal/sector"
)

func TestBuildDatesPersistsNewestSectorReport(t *testing.T) {
	rawDir := filepath.Join(t.TempDir(), "raw")
	bars := []data.DailyBar{
		{TsCode: "000001.SZ", TradeDate: "20260102", Close: 10, RawClose: 10, Amount: 100},
		{TsCode: "000001.SZ", TradeDate: "20260105", Close: 11, RawClose: 11, Amount: 120},
		{TsCode: "000002.SZ", TradeDate: "20260102", Close: 20, RawClose: 20, Amount: 200},
		{TsCode: "000002.SZ", TradeDate: "20260105", Close: 19, RawClose: 19, Amount: 180},
	}
	if err := data.WriteParquetFile(filepath.Join(rawDir, "daily", "2026.parquet"), bars); err != nil {
		t.Fatal(err)
	}
	if err := data.WriteStocksParquet(filepath.Join(rawDir, "stocks.parquet"), []data.StockInfo{
		{TsCode: "000001.SZ", Name: "甲", Industry: "电子", ListDate: "20200101"},
		{TsCode: "000002.SZ", Name: "乙", Industry: "电子", ListDate: "20200101"},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := BuildDates(rawDir, []string{"20260102", "20260105"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TradeDate != "20260105" || result.Rows != 2 {
		t.Fatalf("result = %+v, want latest 20260105 and 2 rows", result)
	}
	if result.Report == nil || result.Report.TradeDate != "20260105" || len(result.Report.Sectors) != 1 {
		t.Fatalf("report = %+v, want newest one-sector report", result.Report)
	}
	restored, err := sector.LoadReport(rawDir, "20260105")
	if err != nil {
		t.Fatal(err)
	}
	if restored == nil || len(restored.Sectors) != 1 || restored.Sectors[0].MemberCount != 2 {
		t.Fatalf("stored report = %+v, want two members", restored)
	}
}
