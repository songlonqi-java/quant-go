package dataset

import (
	"path/filepath"
	"testing"

	"quant/internal/data"
)

func TestLoadFiltersLatestAndST(t *testing.T) {
	rawDir := filepath.Join(t.TempDir(), "raw")
	if err := data.WriteParquetFile(filepath.Join(rawDir, "daily", "2026.parquet"), []data.DailyBar{
		testBar("000001.SZ", "20260101", 10),
		testBar("000001.SZ", "20260102", 11),
		testBar("000002.SZ", "20260101", 20),
		testBar("000003.SZ", "20260102", 30),
	}); err != nil {
		t.Fatalf("WriteParquetFile() error = %v", err)
	}
	if err := data.WriteStocksParquet(filepath.Join(rawDir, "stocks.parquet"), []data.StockInfo{
		{TsCode: "000001.SZ", Name: "平安银行"},
		{TsCode: "000002.SZ", Name: "旧数据"},
		{TsCode: "000003.SZ", Name: "ST测试"},
	}); err != nil {
		t.Fatalf("WriteStocksParquet() error = %v", err)
	}

	ds, err := Load(LoadOptions{RawDir: rawDir, LatestOnly: true, FilterST: true})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if ds.LatestDate != "20260102" {
		t.Fatalf("LatestDate = %s, want 20260102", ds.LatestDate)
	}
	if ds.SkippedStale != 1 {
		t.Fatalf("SkippedStale = %d, want 1", ds.SkippedStale)
	}
	if ds.FilteredST != 1 {
		t.Fatalf("FilteredST = %d, want 1", ds.FilteredST)
	}
	if len(ds.CodeMap) != 1 {
		t.Fatalf("len(CodeMap) = %d, want 1", len(ds.CodeMap))
	}
	if _, ok := ds.CodeMap["000001.SZ"]; !ok {
		t.Fatalf("remaining codes = %v, want 000001.SZ", ds.CodeMap)
	}
}

func testBar(code, date string, close float64) data.DailyBar {
	return data.DailyBar{
		TsCode:    code,
		TradeDate: date,
		Open:      close,
		High:      close,
		Low:       close,
		Close:     close,
		Vol:       1000,
		RawOpen:   close,
		RawHigh:   close,
		RawLow:    close,
		RawClose:  close,
		AdjFactor: 1,
	}
}
